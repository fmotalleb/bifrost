package proxy

import (
	"context"
	"errors"
	"net"
	"testing"

	"github.com/fmotalleb/bifrost/config"
)

func testRouteSelector(t *testing.T) *Selector {
	t.Helper()
	selector, err := NewSelector(map[string]config.Iface{
		"eth0": {Weight: 1},
		"eth1": {Weight: 1},
	})
	if err != nil {
		t.Fatalf("NewSelector failed: %v", err)
	}
	return selector
}

func testRouteBindingsAndCache() (map[string]ifaceBinding, *IPCache) {
	return map[string]ifaceBinding{
			"eth0": {name: "eth0", index: 1},
			"eth1": {name: "eth1", index: 2},
		}, &IPCache{
			prefetch: true,
			entries: map[int]ipCacheEntry{
				1: {ip: net.ParseIP("10.0.0.1")},
				2: {ip: net.ParseIP("10.0.0.2")},
			},
		}
}

func TestDialWithFailoverSuccessOnSecondAttempt(t *testing.T) {
	selector := testRouteSelector(t)
	bindings, cache := testRouteBindingsAndCache()

	failCount := 0
	dialCount := 0
	route, conn, err := dialWithFailover(
		context.Background(),
		selector,
		bindings,
		cache,
		func(_ ifaceBinding) bool { return true },
		2,
		func(_ context.Context, _ selectedRoute) (net.Conn, error) {
			dialCount++
			if dialCount == 1 {
				return nil, errors.New("first attempt failed")
			}
			c1, c2 := net.Pipe()
			_ = c2.Close()
			return c1, nil
		},
		func(route selectedRoute, _ error) {
			if route.ifaceName != "" {
				failCount++
			}
		},
	)
	if err != nil {
		t.Fatalf("dialWithFailover returned error: %v", err)
	}
	defer conn.Close()

	if route.ifaceName == "" {
		t.Fatal("expected selected route")
	}
	if dialCount != 2 {
		t.Fatalf("dial count = %d, want 2", dialCount)
	}
	if failCount != 1 {
		t.Fatalf("fail count = %d, want 1", failCount)
	}
}

func TestDialWithFailoverExhausted(t *testing.T) {
	selector := testRouteSelector(t)
	bindings, cache := testRouteBindingsAndCache()

	failCount := 0
	_, conn, err := dialWithFailover(
		context.Background(),
		selector,
		bindings,
		cache,
		func(_ ifaceBinding) bool { return true },
		2,
		func(_ context.Context, _ selectedRoute) (net.Conn, error) {
			return nil, errors.New("dial failed")
		},
		func(route selectedRoute, _ error) {
			if route.ifaceName != "" {
				failCount++
			}
		},
	)
	if err == nil {
		t.Fatal("expected error when all attempts fail")
	}
	if conn != nil {
		t.Fatal("expected nil connection on failure")
	}
	if failCount != 2 {
		t.Fatalf("fail count = %d, want 2", failCount)
	}
}

func TestFailoverAttempts(t *testing.T) {
	if got := failoverAttempts(0, 0); got != 1 {
		t.Fatalf("failoverAttempts(0, 0) = %d, want 1", got)
	}
	if got := failoverAttempts(0, 1); got != 1 {
		t.Fatalf("failoverAttempts(0, 1) = %d, want 1", got)
	}
	if got := failoverAttempts(0, 2); got != 2 {
		t.Fatalf("failoverAttempts(0, 2) = %d, want 2", got)
	}
	if got := failoverAttempts(0, 10); got != 2 {
		t.Fatalf("failoverAttempts(0, 10) = %d, want 2", got)
	}
	if got := failoverAttempts(1, 10); got != 1 {
		t.Fatalf("failoverAttempts(1, 10) = %d, want 1", got)
	}
	if got := failoverAttempts(3, 2); got != 2 {
		t.Fatalf("failoverAttempts(3, 2) = %d, want 2", got)
	}
	if got := failoverAttempts(3, 10); got != 3 {
		t.Fatalf("failoverAttempts(3, 10) = %d, want 3", got)
	}
}
