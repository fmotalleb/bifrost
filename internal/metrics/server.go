package metrics

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/fmotalleb/bifrost/internal/proxy"
)

const unassignedIface = "unassigned"

type ifaceCounters struct {
	success uint64
	failed  uint64
	txBytes uint64
	rxBytes uint64
}

// IfaceSnapshot is the dashboard/API view for one interface.
type IfaceSnapshot struct {
	Name    string `json:"name"`
	Success uint64 `json:"success"`
	Failed  uint64 `json:"failed"`
	TXBytes uint64 `json:"tx_bytes"`
	RXBytes uint64 `json:"rx_bytes"`
}

// Snapshot is a point-in-time metrics summary used by the dashboard.
type Snapshot struct {
	GeneratedAt  time.Time       `json:"generated_at"`
	TotalSuccess uint64          `json:"total_success"`
	TotalFailed  uint64          `json:"total_failed"`
	TotalTXBytes uint64          `json:"total_tx_bytes"`
	TotalRXBytes uint64          `json:"total_rx_bytes"`
	Ifaces       []IfaceSnapshot `json:"ifaces"`
}

// Recorder records proxy telemetry and exports Prometheus metrics.
type Recorder struct {
	failedConnections    *prometheus.CounterVec
	successConnections   *prometheus.CounterVec
	transferBytesTotal   *prometheus.CounterVec
	txBytesPerConnection *prometheus.HistogramVec
	rxBytesPerConnection *prometheus.HistogramVec
	totalSuccess         atomic.Uint64
	totalFailed          atomic.Uint64
	totalTXBytes         atomic.Uint64
	totalRXBytes         atomic.Uint64
	mu                   sync.RWMutex
	ifaceCountersByName  map[string]*ifaceCounters
}

func newRecorder(ifaces []string, registerer prometheus.Registerer) *Recorder {
	recorder := &Recorder{
		failedConnections: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: "bifrost",
				Name:      "failed_connections_total",
				Help:      "Total failed connection attempts grouped by selected interface.",
			},
			[]string{"iface"},
		),
		successConnections: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: "bifrost",
				Name:      "successful_connections_total",
				Help:      "Total successful proxied connections grouped by selected interface.",
			},
			[]string{"iface"},
		),
		transferBytesTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: "bifrost",
				Name:      "transfer_bytes_total",
				Help:      "Total transferred bytes observed on proxy streams by interface and direction.",
			},
			[]string{"iface", "direction"},
		),
		txBytesPerConnection: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: "bifrost",
				Name:      "connection_tx_bytes",
				Help:      "Client-to-upstream transferred bytes per successful connection.",
				Buckets:   prometheus.ExponentialBuckets(1024, 2, 20),
			},
			[]string{"iface"},
		),
		rxBytesPerConnection: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: "bifrost",
				Name:      "connection_rx_bytes",
				Help:      "Upstream-to-client transferred bytes per successful connection.",
				Buckets:   prometheus.ExponentialBuckets(1024, 2, 20),
			},
			[]string{"iface"},
		),
		ifaceCountersByName: make(map[string]*ifaceCounters, len(ifaces)+1),
	}

	registerer.MustRegister(
		recorder.failedConnections,
		recorder.successConnections,
		recorder.transferBytesTotal,
		recorder.txBytesPerConnection,
		recorder.rxBytesPerConnection,
	)

	allIfaces := append([]string{}, ifaces...)
	allIfaces = append(allIfaces, unassignedIface)
	for _, iface := range allIfaces {
		name := normalizeIface(iface)
		recorder.ifaceCountersByName[name] = &ifaceCounters{}
		recorder.failedConnections.WithLabelValues(name).Add(0)
		recorder.successConnections.WithLabelValues(name).Add(0)
		recorder.transferBytesTotal.WithLabelValues(name, proxy.DirectionTX).Add(0)
		recorder.transferBytesTotal.WithLabelValues(name, proxy.DirectionRX).Add(0)
	}

	return recorder
}

func normalizeIface(iface string) string {
	name := strings.TrimSpace(iface)
	if name == "" {
		return unassignedIface
	}
	return strings.ToLower(name)
}

func (r *Recorder) counterFor(iface string) *ifaceCounters {
	name := normalizeIface(iface)
	r.mu.RLock()
	counter, ok := r.ifaceCountersByName[name]
	r.mu.RUnlock()
	if ok {
		return counter
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	counter, ok = r.ifaceCountersByName[name]
	if ok {
		return counter
	}

	counter = &ifaceCounters{}
	r.ifaceCountersByName[name] = counter
	r.failedConnections.WithLabelValues(name).Add(0)
	r.successConnections.WithLabelValues(name).Add(0)
	r.transferBytesTotal.WithLabelValues(name, proxy.DirectionTX).Add(0)
	r.transferBytesTotal.WithLabelValues(name, proxy.DirectionRX).Add(0)
	return counter
}

// AddTransfer records transfer bytes by interface and direction.
func (r *Recorder) AddTransfer(iface string, direction string, bytes int64) {
	if bytes <= 0 {
		return
	}
	name := normalizeIface(iface)

	switch direction {
	case proxy.DirectionTX:
		r.totalTXBytes.Add(uint64(bytes))
		counter := r.counterFor(name)
		atomic.AddUint64(&counter.txBytes, uint64(bytes))
	case proxy.DirectionRX:
		r.totalRXBytes.Add(uint64(bytes))
		counter := r.counterFor(name)
		atomic.AddUint64(&counter.rxBytes, uint64(bytes))
	default:
		return
	}

	r.transferBytesTotal.WithLabelValues(name, direction).Add(float64(bytes))
}

// ObserveConnection records per-connection success/failure and byte histograms.
func (r *Recorder) ObserveConnection(iface string, success bool, txBytes, rxBytes int64) {
	name := normalizeIface(iface)
	counter := r.counterFor(name)

	if success {
		r.totalSuccess.Add(1)
		atomic.AddUint64(&counter.success, 1)
		r.successConnections.WithLabelValues(name).Inc()

		if txBytes >= 0 {
			r.txBytesPerConnection.WithLabelValues(name).Observe(float64(txBytes))
		}
		if rxBytes >= 0 {
			r.rxBytesPerConnection.WithLabelValues(name).Observe(float64(rxBytes))
		}
		return
	}

	r.totalFailed.Add(1)
	atomic.AddUint64(&counter.failed, 1)
	r.failedConnections.WithLabelValues(name).Inc()
}

// Snapshot returns a point-in-time copy of in-memory counters.
func (r *Recorder) Snapshot() Snapshot {
	snapshot := Snapshot{
		GeneratedAt:  time.Now(),
		TotalSuccess: r.totalSuccess.Load(),
		TotalFailed:  r.totalFailed.Load(),
		TotalTXBytes: r.totalTXBytes.Load(),
		TotalRXBytes: r.totalRXBytes.Load(),
	}

	r.mu.RLock()
	ifaceNames := make([]string, 0, len(r.ifaceCountersByName))
	for iface := range r.ifaceCountersByName {
		ifaceNames = append(ifaceNames, iface)
	}
	sort.Strings(ifaceNames)
	for _, name := range ifaceNames {
		counter := r.ifaceCountersByName[name]
		snapshot.Ifaces = append(snapshot.Ifaces, IfaceSnapshot{
			Name:    name,
			Success: atomic.LoadUint64(&counter.success),
			Failed:  atomic.LoadUint64(&counter.failed),
			TXBytes: atomic.LoadUint64(&counter.txBytes),
			RXBytes: atomic.LoadUint64(&counter.rxBytes),
		})
	}
	r.mu.RUnlock()

	return snapshot
}

// Server exposes Prometheus metrics and a built-in dashboard.
type Server struct {
	addr     netip.AddrPort
	httpSrv  *http.Server
	recorder *Recorder
}

// NewServer creates a metrics web server with /metrics and dashboard routes.
func NewServer(addr netip.AddrPort, ifaces []string) (*Server, error) {
	if !addr.IsValid() {
		return nil, fmt.Errorf("metrics address must be valid")
	}

	registry := prometheus.NewRegistry()
	recorder := newRecorder(ifaces, registry)

	server := &Server{
		addr:     addr,
		recorder: recorder,
	}

	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(registry, promhttp.HandlerOpts{}))
	mux.HandleFunc("/api/snapshot", server.handleSnapshot)
	mux.HandleFunc("/", server.handleDashboard)

	server.httpSrv = &http.Server{
		Addr:              addr.String(),
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	return server, nil
}

// Telemetry returns the proxy telemetry recorder implementation.
func (s *Server) Telemetry() proxy.Telemetry {
	return s.recorder
}

// Serve starts the metrics server and stops it when context is canceled.
func (s *Server) Serve(ctx context.Context) error {
	listener, err := net.Listen("tcp", s.addr.String())
	if err != nil {
		return fmt.Errorf("listen metrics on %s: %w", s.addr.String(), err)
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.httpSrv.Shutdown(shutdownCtx)
	}()

	err = s.httpSrv.Serve(listener)
	if err == nil || err == http.ErrServerClosed {
		return nil
	}
	return fmt.Errorf("serve metrics http: %w", err)
}

func (s *Server) handleSnapshot(writer http.ResponseWriter, _ *http.Request) {
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err := json.NewEncoder(writer).Encode(s.recorder.Snapshot()); err != nil {
		http.Error(writer, err.Error(), http.StatusInternalServerError)
	}
}

func (s *Server) handleDashboard(writer http.ResponseWriter, _ *http.Request) {
	writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = writer.Write([]byte(dashboardHTML))
}

const dashboardHTML = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Bifrost Metrics</title>
  <style>
    :root {
      --bg: #0a111f;
      --panel: #111c33;
      --panel-soft: #182746;
      --text: #f3f7ff;
      --muted: #9eb4da;
      --good: #3ed598;
      --bad: #ff6f7d;
      --accent: #49a2ff;
      --line: #274170;
    }
    * { box-sizing: border-box; }
    body {
      margin: 0;
      font-family: "Segoe UI", "Trebuchet MS", sans-serif;
      color: var(--text);
      background:
        radial-gradient(60rem 60rem at 10% -10%, #2e5da455 0%, transparent 60%),
        radial-gradient(50rem 50rem at 95% 10%, #2b8a6e33 0%, transparent 55%),
        linear-gradient(180deg, #060b15 0%, #0a111f 100%);
      min-height: 100vh;
      padding: 24px;
    }
    .wrap {
      max-width: 1100px;
      margin: 0 auto;
      display: grid;
      gap: 14px;
    }
    .head {
      display: flex;
      justify-content: space-between;
      align-items: flex-end;
      gap: 10px;
    }
    .title {
      font-size: 1.6rem;
      margin: 0;
      letter-spacing: 0.02em;
    }
    .sub {
      color: var(--muted);
      margin-top: 4px;
      font-size: 0.95rem;
    }
    .cards {
      display: grid;
      grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
      gap: 12px;
    }
    .card {
      background: linear-gradient(160deg, var(--panel) 0%, var(--panel-soft) 100%);
      border: 1px solid var(--line);
      border-radius: 14px;
      padding: 14px 16px;
      box-shadow: 0 10px 30px #00000033;
    }
    .k {
      color: var(--muted);
      font-size: 0.85rem;
      text-transform: uppercase;
      letter-spacing: 0.08em;
    }
    .v {
      margin-top: 6px;
      font-size: 1.3rem;
      font-weight: 700;
    }
    .good { color: var(--good); }
    .bad { color: var(--bad); }
    .table-wrap {
      background: linear-gradient(160deg, var(--panel) 0%, var(--panel-soft) 100%);
      border: 1px solid var(--line);
      border-radius: 14px;
      overflow: hidden;
      box-shadow: 0 10px 30px #00000033;
    }
    table {
      width: 100%;
      border-collapse: collapse;
    }
    th, td {
      padding: 11px 12px;
      border-bottom: 1px solid #20365e;
      text-align: left;
      font-size: 0.92rem;
    }
    th {
      color: #c9d9f7;
      background: #0d1830;
      font-weight: 600;
    }
    tr:last-child td {
      border-bottom: 0;
    }
    .mono {
      font-variant-numeric: tabular-nums;
      font-family: "Consolas", "Courier New", monospace;
    }
    .foot {
      color: var(--muted);
      font-size: 0.85rem;
      display: flex;
      gap: 10px;
      align-items: center;
      justify-content: space-between;
    }
    a { color: #8dc3ff; text-decoration: none; }
    a:hover { text-decoration: underline; }
  </style>
</head>
<body>
  <div class="wrap">
    <div class="head">
      <div>
        <h1 class="title">Bifrost Metrics Dashboard</h1>
        <div class="sub">Live connection health and transfer throughput by interface</div>
      </div>
      <div class="sub mono" id="updatedAt">Last update: -</div>
    </div>

    <div class="cards">
      <div class="card"><div class="k">Success Rate</div><div class="v" id="successRate">-</div></div>
      <div class="card"><div class="k">Successful Connections</div><div class="v good mono" id="successCount">0</div></div>
      <div class="card"><div class="k">Failed Connections</div><div class="v bad mono" id="failedCount">0</div></div>
      <div class="card"><div class="k">Current TX Rate</div><div class="v mono" id="txRate">0 B/s</div></div>
      <div class="card"><div class="k">Current RX Rate</div><div class="v mono" id="rxRate">0 B/s</div></div>
      <div class="card"><div class="k">Total Transfer</div><div class="v mono" id="totalTransfer">0 B</div></div>
    </div>

    <div class="table-wrap">
      <table>
        <thead>
          <tr>
            <th>Interface</th>
            <th>Success</th>
            <th>Failed</th>
            <th>Success Rate</th>
            <th>TX Bytes</th>
            <th>RX Bytes</th>
            <th>TX Rate</th>
            <th>RX Rate</th>
          </tr>
        </thead>
        <tbody id="ifaceRows"></tbody>
      </table>
    </div>

    <div class="foot">
      <span>Prometheus endpoint: <a href="/metrics" target="_blank" rel="noopener noreferrer">/metrics</a></span>
      <span>JSON snapshot endpoint: <a href="/api/snapshot" target="_blank" rel="noopener noreferrer">/api/snapshot</a></span>
    </div>
  </div>

  <script>
    let previous = null;

    function formatBytes(value) {
      if (value < 1024) return value.toFixed(0) + " B";
      const units = ["KB", "MB", "GB", "TB"];
      let v = value / 1024;
      let idx = 0;
      while (v >= 1024 && idx < units.length - 1) {
        v /= 1024;
        idx++;
      }
      return v.toFixed(v < 10 ? 2 : 1) + " " + units[idx];
    }

    function formatRate(value) {
      return formatBytes(Math.max(0, value)) + "/s";
    }

    function pct(success, failed) {
      const total = success + failed;
      if (total === 0) return "n/a";
      return ((success * 100) / total).toFixed(2) + "%";
    }

    function mapByName(ifaces) {
      const out = {};
      for (const item of ifaces) out[item.name] = item;
      return out;
    }

    function updateDashboard(snapshot) {
      const now = Date.now();
      let txRate = 0;
      let rxRate = 0;
      let previousByIface = {};
      let elapsed = 0;

      if (previous) {
        elapsed = (now - previous.ts) / 1000;
        if (elapsed > 0) {
          txRate = (snapshot.total_tx_bytes - previous.snap.total_tx_bytes) / elapsed;
          rxRate = (snapshot.total_rx_bytes - previous.snap.total_rx_bytes) / elapsed;
        }
        previousByIface = mapByName(previous.snap.ifaces);
      }

      document.getElementById("updatedAt").textContent = "Last update: " + new Date(snapshot.generated_at).toLocaleTimeString();
      document.getElementById("successCount").textContent = snapshot.total_success.toLocaleString();
      document.getElementById("failedCount").textContent = snapshot.total_failed.toLocaleString();
      document.getElementById("successRate").textContent = pct(snapshot.total_success, snapshot.total_failed);
      document.getElementById("txRate").textContent = formatRate(txRate);
      document.getElementById("rxRate").textContent = formatRate(rxRate);
      document.getElementById("totalTransfer").textContent = formatBytes(snapshot.total_tx_bytes + snapshot.total_rx_bytes);

      const rows = [];
      for (const iface of snapshot.ifaces) {
        let ifaceTxRate = 0;
        let ifaceRxRate = 0;
        if (elapsed > 0 && previousByIface[iface.name]) {
          ifaceTxRate = (iface.tx_bytes - previousByIface[iface.name].tx_bytes) / elapsed;
          ifaceRxRate = (iface.rx_bytes - previousByIface[iface.name].rx_bytes) / elapsed;
        }
        rows.push(
          "<tr>" +
          "<td class='mono'>" + iface.name + "</td>" +
          "<td class='mono'>" + iface.success.toLocaleString() + "</td>" +
          "<td class='mono'>" + iface.failed.toLocaleString() + "</td>" +
          "<td class='mono'>" + pct(iface.success, iface.failed) + "</td>" +
          "<td class='mono'>" + formatBytes(iface.tx_bytes) + "</td>" +
          "<td class='mono'>" + formatBytes(iface.rx_bytes) + "</td>" +
          "<td class='mono'>" + formatRate(ifaceTxRate) + "</td>" +
          "<td class='mono'>" + formatRate(ifaceRxRate) + "</td>" +
          "</tr>"
        );
      }
      document.getElementById("ifaceRows").innerHTML = rows.join("");

      previous = { ts: now, snap: snapshot };
    }

    async function tick() {
      try {
        const response = await fetch("/api/snapshot", { cache: "no-store" });
        if (!response.ok) throw new Error("snapshot request failed: " + response.status);
        const snapshot = await response.json();
        updateDashboard(snapshot);
      } catch (error) {
        console.error(error);
      }
    }

    tick();
    setInterval(tick, 1000);
  </script>
</body>
</html>`
