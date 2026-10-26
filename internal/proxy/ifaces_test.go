package proxy

import (
	"net"
	"testing"
)

type unsupportedAddr struct{}

func (u unsupportedAddr) Network() string { return "" }
func (u unsupportedAddr) String() string  { return "" }

func ipAddr(raw string) *net.IPAddr {
	return &net.IPAddr{IP: net.ParseIP(raw)}
}

func TestPickAddress(t *testing.T) {
	addrs := []net.Addr{
		ipAddr("127.0.0.1"),
		ipAddr("2001:db8::1"),
		ipAddr("10.10.10.2"),
	}

	ipv4 := pickAddress(addrs, true)
	if got, want := ipv4.String(), "10.10.10.2"; got != want {
		t.Fatalf("pickAddress(preferIPv4=true) = %q, want %q", got, want)
	}

	ipv6 := pickAddress(addrs, false)
	if got, want := ipv6.String(), "2001:db8::1"; got != want {
		t.Fatalf("pickAddress(preferIPv4=false) = %q, want %q", got, want)
	}
}

func TestPickAddressDoesNotUseOtherFamily(t *testing.T) {
	addrs := []net.Addr{ipAddr("2001:db8::2")}
	if got := pickAddress(addrs, true); got != nil {
		t.Fatalf("pickAddress(preferIPv4=true) = %q, want nil", got)
	}
}

func TestInterfaceHasIP(t *testing.T) {
	addrs := []net.Addr{
		&net.IPNet{IP: net.ParseIP("10.10.10.3")},
		ipAddr("2001:db8::2"),
	}
	if !interfaceHasIP(addrs, net.ParseIP("10.10.10.3")) {
		t.Fatal("interfaceHasIP did not find configured address")
	}
	if interfaceHasIP(addrs, net.ParseIP("10.10.10.4")) {
		t.Fatal("interfaceHasIP matched an address that is not configured")
	}
}

func TestIPFromAddr(t *testing.T) {
	ipNet := &net.IPNet{IP: net.ParseIP("10.0.0.1")}
	if got := ipFromAddr(ipNet); got == nil || got.String() != "10.0.0.1" {
		t.Fatalf("ipFromAddr(*net.IPNet) = %v, want 10.0.0.1", got)
	}

	ipAddr := &net.IPAddr{IP: net.ParseIP("2001:db8::5")}
	if got := ipFromAddr(ipAddr); got == nil || got.String() != "2001:db8::5" {
		t.Fatalf("ipFromAddr(*net.IPAddr) = %v, want 2001:db8::5", got)
	}

	if got := ipFromAddr(unsupportedAddr{}); got != nil {
		t.Fatalf("ipFromAddr(unsupported) = %v, want nil", got)
	}
}

func TestNormalizeIfaceName(t *testing.T) {
	got := normalizeIfaceName("  Ethernet\t 0   ")
	want := "Ethernet 0"
	if got != want {
		t.Fatalf("normalizeIfaceName() = %q, want %q", got, want)
	}
}
