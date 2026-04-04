package proxy

import (
	"context"
	"fmt"
	"net"
)

// dialContextOnRoute dials the remote target from a selected route.
//
// It always binds the local source IP and may apply OS-specific interface pinning.
func dialContextOnRoute(ctx context.Context, network, addr string, route selectedRoute) (net.Conn, error) {
	dialer := net.Dialer{LocalAddr: &net.TCPAddr{IP: route.bindIP}}

	if err := configureDialerInterfacePinning(&dialer, route.binding.index, route.bindIP); err != nil {
		return nil, fmt.Errorf("configure interface pinning for %q: %w", route.ifaceName, err)
	}

	return dialer.DialContext(ctx, network, addr)
}
