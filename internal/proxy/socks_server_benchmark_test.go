package proxy

import (
	"net"
	"testing"

	"github.com/fmotalleb/bifrost/config"
)

func benchmarkSOCKSServer() *SOCKSServer {
	ifaces := map[string]config.Iface{
		"eth0": {Weight: 1},
		"eth1": {Weight: 1},
		"eth2": {Weight: 1},
		"eth3": {Weight: 1},
	}
	selector, _ := NewSelector(ifaces)

	bindings := map[string]ifaceBinding{
		"eth0": {name: "eth0", index: 1},
		"eth1": {name: "eth1", index: 2},
		"eth2": {name: "eth2", index: 3},
		"eth3": {name: "eth3", index: 4},
	}

	cache := &IPCache{
		prefetch: true,
		entries: map[int]ipCacheEntry{
			1: {ip: net.ParseIP("10.0.0.1")},
			2: {ip: net.ParseIP("10.0.0.2")},
			3: {ip: net.ParseIP("10.0.0.3")},
			4: {ip: net.ParseIP("10.0.0.4")},
		},
	}

	return &SOCKSServer{
		selector:      selector,
		ifaceBindings: bindings,
		ipCache:       cache,
		telemetry:     noopTelemetry{},
	}
}

func BenchmarkSOCKSSelectDialRouteIPv4(b *testing.B) {
	server := benchmarkSOCKSServer()
	addr := "93.184.216.34:443"

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		route, err := server.selectDialRoute(addr)
		if err != nil {
			b.Fatalf("selectDialRoute failed: %v", err)
		}
		server.selector.Release(route.ifaceName)
	}
}

func BenchmarkSOCKSSelectDialRouteHostname(b *testing.B) {
	server := benchmarkSOCKSServer()
	addr := "example.com:443"

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		route, err := server.selectDialRoute(addr)
		if err != nil {
			b.Fatalf("selectDialRoute failed: %v", err)
		}
		server.selector.Release(route.ifaceName)
	}
}
