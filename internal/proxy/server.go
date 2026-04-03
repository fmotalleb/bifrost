package proxy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync/atomic"

	"github.com/fmotalleb/go-tools/log"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"

	"github.com/fmotalleb/bifrost/config"
)

var errStreamAborted = errors.New("proxy stream aborted")

const (
	streamDirections = 2
)

// Server is a TCP reverse proxy that binds each upstream connection to a selected interface.
type Server struct {
	cfg           config.Config
	selector      *Selector
	ifaceBindings map[string]ifaceBinding
	ipCache       *IPCache
	telemetry     Telemetry
	connID        atomic.Uint64
}

// NewServer constructs a proxy server from config.
func NewServer(cfg config.Config, telemetry Telemetry) (*Server, error) {
	preferIPv4 := cfg.Server.Addr().Is4()
	runtime, err := prepareRuntimeDependencies(cfg, preferIPv4, telemetry)
	if err != nil {
		return nil, err
	}

	return &Server{
		cfg:           runtime.cfg,
		selector:      runtime.selector,
		ifaceBindings: runtime.bindings,
		ipCache:       runtime.cache,
		telemetry:     runtime.telemetry,
	}, nil
}

func normalizeConfiguredIFaces(
	ifaces map[string]config.Iface,
	preferIPv4 bool,
) (map[string]config.Iface, map[string]ifaceBinding, error) {
	normalized := make(map[string]config.Iface, len(ifaces))
	bindings := make(map[string]ifaceBinding, len(ifaces))
	for configuredName, ifaceCfg := range ifaces {
		resolvedIface, err := ResolveInterface(configuredName)
		if err != nil {
			return nil, nil, err
		}
		resolvedName := resolvedIface.Name

		if _, exists := normalized[resolvedName]; exists {
			return nil, nil, fmt.Errorf(
				"configured interfaces resolve to the same OS interface %q",
				resolvedName,
			)
		}

		sourceIP := normalizeSourceIP(ifaceCfg.SourceIP, preferIPv4)
		if ifaceCfg.SourceIP != nil && sourceIP == nil {
			family := "IPv6"
			if preferIPv4 {
				family = "IPv4"
			}
			return nil, nil, fmt.Errorf(
				"interface %q source_ip %q does not match upstream address family %s",
				configuredName,
				ifaceCfg.SourceIP.String(),
				family,
			)
		}

		ifaceCfg.SourceIP = cloneIP(sourceIP)
		normalized[resolvedName] = ifaceCfg
		bindings[resolvedName] = ifaceBinding{
			name:     resolvedName,
			index:    resolvedIface.Index,
			sourceIP: cloneIP(sourceIP),
		}
	}

	return normalized, bindings, nil
}

// normalizeSourceIP keeps only the address family used by the upstream server.
func normalizeSourceIP(ip net.IP, preferIPv4 bool) net.IP {
	if ip == nil {
		return nil
	}
	if preferIPv4 {
		return ip.To4()
	}
	if ip.To4() != nil {
		return nil
	}
	return ip
}

// Serve starts listening and blocks until context cancellation or fatal listener failure.
func (s *Server) Serve(ctx context.Context) error {
	logger := log.Of(ctx)
	var listenConfig net.ListenConfig
	listener, err := listenConfig.Listen(ctx, "tcp", s.cfg.Listen.String())
	if err != nil {
		return fmt.Errorf("listen on %s: %w", s.cfg.Listen, err)
	}
	defer listener.Close()

	logger.Info("listening",
		zap.String("listen", s.cfg.Listen.String()),
		zap.String("server", s.cfg.Server.String()),
	)

	go func() {
		<-ctx.Done()
		_ = listener.Close()
	}()

	for {
		clientConn, acceptErr := listener.Accept()
		if acceptErr != nil {
			if errors.Is(acceptErr, net.ErrClosed) || ctx.Err() != nil {
				return nil
			}

			logger.Warn("accept failed", zap.Error(acceptErr))
			continue
		}

		id := s.connID.Add(1)
		go s.handleConnection(ctx, id, clientConn)
	}
}

func (s *Server) handleConnection(ctx context.Context, id uint64, clientConn net.Conn) {
	logger := log.Of(ctx)
	defer clientConn.Close()

	ifaceName, err := s.selector.Pick()
	if err != nil {
		logger.Warn("failed to select interface", zap.Uint64("connection_id", id), zap.Error(err))
		return
	}
	defer s.selector.Release(ifaceName)

	var txBytes int64
	var rxBytes int64
	var success bool
	defer func() {
		s.telemetry.ObserveConnection(ifaceName, success, txBytes, rxBytes)
	}()

	binding, ok := s.ifaceBindings[ifaceName]
	if !ok {
		logger.Warn("missing cached interface binding",
			zap.Uint64("connection_id", id),
			zap.String("iface", ifaceName),
		)
		return
	}

	bindIP, err := s.ipCache.GetBindIP(binding, s.cfg.Server.Addr().Is4())
	if err != nil {
		logger.Warn("failed to resolve interface ip",
			zap.Uint64("connection_id", id),
			zap.String("iface", ifaceName),
			zap.Int("iface_index", binding.index),
			zap.Error(err),
		)
		return
	}

	dialer := net.Dialer{LocalAddr: &net.TCPAddr{IP: bindIP}}
	upstreamConn, err := dialer.DialContext(ctx, "tcp", s.cfg.Server.String())
	if err != nil {
		logger.Warn("failed to dial upstream",
			zap.Uint64("connection_id", id),
			zap.String("iface", ifaceName),
			zap.Int("iface_index", binding.index),
			zap.String("bind_ip", bindIP.String()),
			zap.Error(err),
		)
		return
	}
	defer upstreamConn.Close()
	success = true

	logger.Info("accepted connection",
		zap.Uint64("connection_id", id),
		zap.String("client", clientConn.RemoteAddr().String()),
		zap.String("iface", ifaceName),
		zap.Int("iface_index", binding.index),
		zap.String("bind_ip", bindIP.String()),
		zap.String("upstream", s.cfg.Server.String()),
	)

	stats, proxyErr := pipeBothWays(clientConn, upstreamConn, func(direction string, bytes int64) {
		s.telemetry.AddTransfer(ifaceName, direction, bytes)
	})
	txBytes = stats.clientToUpstream
	rxBytes = stats.upstreamToClient
	if proxyErr != nil {
		fields := []zap.Field{
			zap.Uint64("connection_id", id),
			zap.String("client", clientConn.RemoteAddr().String()),
			zap.String("upstream", s.cfg.Server.String()),
			zap.Error(proxyErr),
		}
		if isHotPathConnectionError(proxyErr) {
			logger.Debug("proxy stream closed with expected network error", fields...)
			return
		}
		logger.Warn("proxy stream failed", fields...)
		return
	}
}

type transferStats struct {
	clientToUpstream int64
	upstreamToClient int64
}

func pipeBothWays(
	clientConn, upstreamConn net.Conn,
	onTransfer func(direction string, bytes int64),
) (transferStats, error) {
	group := new(errgroup.Group)
	results := make(chan streamCopyResult, streamDirections)

	group.Go(func() error {
		results <- copyAndCloseWrite(
			withMonitoredWrite(upstreamConn, DirectionTX, onTransfer),
			clientConn,
			"client_to_upstream",
		)
		return nil
	})

	group.Go(func() error {
		results <- copyAndCloseWrite(
			withMonitoredWrite(clientConn, DirectionRX, onTransfer),
			upstreamConn,
			"upstream_to_client",
		)
		return nil
	})

	_ = group.Wait()
	close(results)

	stats := transferStats{}
	var aborted bool
	unexpected := make([]error, 0, streamDirections)
	for result := range results {
		if result.aborted {
			aborted = true
		}
		switch result.direction {
		case DirectionTX:
			stats.clientToUpstream = result.bytes
		case DirectionRX:
			stats.upstreamToClient = result.bytes
		}
		if result.err != nil {
			unexpected = append(unexpected, result.err)
		}
	}

	if len(unexpected) > 0 {
		return stats, errors.Join(unexpected...)
	}
	if aborted {
		return stats, errStreamAborted
	}

	return stats, nil
}

type streamCopyResult struct {
	direction string
	bytes     int64
	aborted   bool
	err       error
}

func copyAndCloseWrite(dst, src net.Conn, direction string) streamCopyResult {
	result := streamCopyResult{direction: classifyDirection(direction)}

	copied, err := io.Copy(dst, src)
	result.bytes = copied
	if err != nil {
		if isHotPathConnectionError(err) {
			result.aborted = true
		} else {
			result.err = fmt.Errorf("%s: copy: %w", direction, err)
		}
	}

	if err := closeWrite(dst); err != nil {
		switch {
		case isHotPathConnectionError(err):
			result.aborted = true
		case result.err == nil:
			result.err = fmt.Errorf("%s: close write: %w", direction, err)
		default:
			result.err = errors.Join(result.err, fmt.Errorf("%s: close write: %w", direction, err))
		}
	}

	return result
}

// classifyDirection maps internal copy labels to exported metrics directions.
func classifyDirection(direction string) string {
	switch direction {
	case "client_to_upstream":
		return DirectionTX
	case "upstream_to_client":
		return DirectionRX
	default:
		return ""
	}
}

type monitoredConn struct {
	net.Conn
	direction  string
	onTransfer func(direction string, bytes int64)
}

func (c monitoredConn) Write(p []byte) (int, error) {
	n, err := c.Conn.Write(p)
	if n > 0 && c.onTransfer != nil {
		c.onTransfer(c.direction, int64(n))
	}
	return n, err
}

func (c monitoredConn) CloseWrite() error {
	closeWriter, ok := c.Conn.(interface{ CloseWrite() error })
	if !ok {
		return c.Close()
	}
	return closeWriter.CloseWrite()
}

func withMonitoredWrite(conn net.Conn, direction string, onTransfer func(string, int64)) net.Conn {
	if onTransfer == nil {
		return conn
	}
	return monitoredConn{
		Conn:       conn,
		direction:  direction,
		onTransfer: onTransfer,
	}
}

// closeWrite performs TCP half-close when available.
func closeWrite(conn net.Conn) error {
	closeWriter, ok := conn.(interface{ CloseWrite() error })
	if !ok {
		return conn.Close()
	}

	return closeWriter.CloseWrite()
}

// isHotPathConnectionError identifies expected close/read/write errors in proxy streams.
func isHotPathConnectionError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, errStreamAborted) {
		return true
	}
	if errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) {
		return true
	}

	var opErr *net.OpError
	if errors.As(err, &opErr) {
		switch opErr.Op {
		case "read", "write", "close":
			return true
		}
	}

	return false
}
