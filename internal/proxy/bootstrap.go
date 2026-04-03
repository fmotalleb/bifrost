package proxy

import (
	"fmt"

	"github.com/fmotalleb/bifrost/config"
)

type runtimeDependencies struct {
	cfg       config.Config
	selector  *Selector
	bindings  map[string]ifaceBinding
	cache     *IPCache
	telemetry Telemetry
}

func prepareRuntimeDependencies(
	cfg config.Config,
	preferIPv4 bool,
	telemetry Telemetry,
) (runtimeDependencies, error) {
	normalizedIFaces, bindings, err := normalizeConfiguredIFaces(cfg.IFaces, preferIPv4)
	if err != nil {
		return runtimeDependencies{}, fmt.Errorf("normalize interfaces: %w", err)
	}
	cfg.IFaces = normalizedIFaces

	cache, err := NewIPCache(cfg.Cache.TTL, cfg.Cache.Prefetch, bindings, preferIPv4)
	if err != nil {
		return runtimeDependencies{}, fmt.Errorf("create ip cache: %w", err)
	}

	selector, err := NewSelector(cfg.IFaces)
	if err != nil {
		return runtimeDependencies{}, err
	}

	if telemetry == nil {
		telemetry = NoopTelemetry
	}

	return runtimeDependencies{
		cfg:       cfg,
		selector:  selector,
		bindings:  bindings,
		cache:     cache,
		telemetry: telemetry,
	}, nil
}
