//go:build !linux && !windows

package proxy

import "net"

func configureDialerInterfacePinning(_ *net.Dialer, _ string, _ int, _ net.IP) error {
	return nil
}
