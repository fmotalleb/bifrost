package proxy

import (
	"context"
	"errors"
	"fmt"
	"net"
)

const defaultDialFailoverAttempts = 2

// selectedRoute is the selected interface route and resolved local bind IP for one outbound dial.
type selectedRoute struct {
	ifaceName string
	binding   ifaceBinding
	bindIP    net.IP
}

// selectBindRoute selects an interface and resolves its bind IP for the outbound connection path.
//
// It encapsulates the shared hot-path logic used by both SOCKS and reverse proxy request handling.
// On failure after interface selection, it releases the selected interface slot before returning.
func selectBindRoute(
	selector *Selector,
	bindings map[string]ifaceBinding,
	cache *IPCache,
	preferIPv4 func(binding ifaceBinding) bool,
) (selectedRoute, error) {
	ifaceName, err := selector.Pick()
	if err != nil {
		return selectedRoute{}, fmt.Errorf("select interface: %w", err)
	}

	binding, ok := bindings[ifaceName]
	if !ok {
		selector.Release(ifaceName)
		return selectedRoute{ifaceName: ifaceName}, fmt.Errorf("missing cached interface binding for %q", ifaceName)
	}

	bindIP, err := cache.GetBindIP(binding, preferIPv4(binding))
	if err != nil {
		selector.Release(ifaceName)
		return selectedRoute{ifaceName: ifaceName, binding: binding}, fmt.Errorf("resolve bind ip for %q: %w", ifaceName, err)
	}

	return selectedRoute{
		ifaceName: ifaceName,
		binding:   binding,
		bindIP:    bindIP,
	}, nil
}

// dialWithFailover dials using selected interface routes and retries on failure.
//
// It performs up to `attempts` attempts. On dial failure, it releases the selected interface
// before retrying. The callback is invoked for each failed attempt and can update telemetry/logging.
func dialWithFailover(
	ctx context.Context,
	selector *Selector,
	bindings map[string]ifaceBinding,
	cache *IPCache,
	preferIPv4 func(binding ifaceBinding) bool,
	attempts int,
	dial func(ctx context.Context, route selectedRoute) (net.Conn, error),
	onAttemptFailure func(route selectedRoute, err error),
) (selectedRoute, net.Conn, error) {
	if attempts < 1 {
		attempts = 1
	}

	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		route, err := selectBindRoute(selector, bindings, cache, preferIPv4)
		if err != nil {
			if onAttemptFailure != nil {
				onAttemptFailure(route, err)
			}
			return selectedRoute{}, nil, err
		}

		conn, err := dial(ctx, route)
		if err == nil {
			return route, conn, nil
		}

		selector.Release(route.ifaceName)
		if onAttemptFailure != nil {
			onAttemptFailure(route, err)
		}
		lastErr = fmt.Errorf("dial via %q: %w", route.ifaceName, err)
	}

	if lastErr == nil {
		lastErr = errors.New("dial failed without attempts")
	}
	return selectedRoute{}, nil, lastErr
}

func failoverAttempts(configuredAttempts int, ifaceCount int) int {
	attempts := configuredAttempts
	if attempts <= 0 {
		attempts = defaultDialFailoverAttempts
	}

	if ifaceCount <= 1 {
		return 1
	}
	if ifaceCount < attempts {
		return ifaceCount
	}
	return attempts
}
