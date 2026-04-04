package proxy

import (
	"net"
	"testing"

	"github.com/fmotalleb/bifrost/config"
)

func benchmarkReverseProxyServer(ip1, ip2, ip3, ip4 string) *Server {
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
			1: {ip: net.ParseIP(ip1)},
			2: {ip: net.ParseIP(ip2)},
			3: {ip: net.ParseIP(ip3)},
			4: {ip: net.ParseIP(ip4)},
		},
	}

	return &Server{
		selector:      selector,
		ifaceBindings: bindings,
		ipCache:       cache,
		telemetry:     noopTelemetry{},
	}
}

func selectReverseProxyRouteForBenchmark(s *Server, preferIPv4 bool) (string, error) {
	route, err := selectBindRoute(
		s.selector,
		s.ifaceBindings,
		s.ipCache,
		func(_ ifaceBinding) bool { return preferIPv4 },
	)
	if err != nil {
		return "", err
	}

	return route.ifaceName, nil
}

func BenchmarkReverseProxySelectRouteIPv4(b *testing.B) {
	server := benchmarkReverseProxyServer("10.0.0.1", "10.0.0.2", "10.0.0.3", "10.0.0.4")

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ifaceName, err := selectReverseProxyRouteForBenchmark(server, true)
		if err != nil {
			b.Fatalf("select route failed: %v", err)
		}
		server.selector.Release(ifaceName)
	}
}

func BenchmarkReverseProxySelectRouteIPv6(b *testing.B) {
	server := benchmarkReverseProxyServer("2001:db8::1", "2001:db8::2", "2001:db8::3", "2001:db8::4")

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ifaceName, err := selectReverseProxyRouteForBenchmark(server, false)
		if err != nil {
			b.Fatalf("select route failed: %v", err)
		}
		server.selector.Release(ifaceName)
	}
}
