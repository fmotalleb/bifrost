package proxy

import "testing"

func TestPrefersIPv4Dial(t *testing.T) {
	tests := []struct {
		name string
		addr string
		want bool
	}{
		{name: "ipv4", addr: "93.184.216.34:443", want: true},
		{name: "ipv6", addr: "[2001:db8::1]:443", want: false},
		{name: "hostname", addr: "example.com:443", want: true},
		{name: "empty", addr: "", want: true},
		{name: "invalid", addr: "invalid", want: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := prefersIPv4Dial(test.addr); got != test.want {
				t.Fatalf("prefersIPv4Dial(%q) = %v, want %v", test.addr, got, test.want)
			}
		})
	}
}
