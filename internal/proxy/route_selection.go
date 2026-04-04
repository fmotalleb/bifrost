package proxy

import (
	"fmt"
	"net"
)

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
