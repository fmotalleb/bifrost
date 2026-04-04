//go:build !windows

package proxy

import "net"

func configureDialerInterfacePinning(_ *net.Dialer, _ int, _ net.IP) error {
	return nil
}
