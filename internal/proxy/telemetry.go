package proxy

const (
	// DirectionTX is bytes sent from client to upstream.
	DirectionTX = "tx"
	// DirectionRX is bytes received from upstream to client.
	DirectionRX = "rx"
)

// Telemetry receives proxy connection and transfer observations.
type Telemetry interface {
	AddTransfer(iface string, direction string, bytes int64)
	ObserveConnection(iface string, success bool, txBytes, rxBytes int64)
}

type noopTelemetry struct{}

func (noopTelemetry) AddTransfer(_ string, _ string, _ int64) {}

func (noopTelemetry) ObserveConnection(_ string, _ bool, _, _ int64) {}

// NoopTelemetry is the default telemetry sink when metrics are disabled.
var NoopTelemetry Telemetry = noopTelemetry{}
