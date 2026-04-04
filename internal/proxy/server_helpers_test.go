package proxy

import (
	"errors"
	"io"
	"net"
	"testing"
)

func TestNormalizeSourceIP(t *testing.T) {
	ipv4 := net.ParseIP("10.0.0.2")
	ipv6 := net.ParseIP("2001:db8::2")

	if got := normalizeSourceIP(ipv4, true); got == nil || got.String() != "10.0.0.2" {
		t.Fatalf("normalizeSourceIP(ipv4, true) = %v", got)
	}
	if got := normalizeSourceIP(ipv6, true); got != nil {
		t.Fatalf("normalizeSourceIP(ipv6, true) = %v, want nil", got)
	}
	if got := normalizeSourceIP(ipv4, false); got != nil {
		t.Fatalf("normalizeSourceIP(ipv4, false) = %v, want nil", got)
	}
	if got := normalizeSourceIP(ipv6, false); got == nil || got.String() != "2001:db8::2" {
		t.Fatalf("normalizeSourceIP(ipv6, false) = %v", got)
	}
}

func TestClassifyDirection(t *testing.T) {
	if got := classifyDirection("client_to_upstream"); got != DirectionTX {
		t.Fatalf("classifyDirection(client_to_upstream) = %q", got)
	}
	if got := classifyDirection("upstream_to_client"); got != DirectionRX {
		t.Fatalf("classifyDirection(upstream_to_client) = %q", got)
	}
	if got := classifyDirection("unknown"); got != "" {
		t.Fatalf("classifyDirection(unknown) = %q, want empty", got)
	}
}

func TestIsHotPathConnectionError(t *testing.T) {
	if isHotPathConnectionError(nil) {
		t.Fatal("nil error must not be hot-path")
	}
	if !isHotPathConnectionError(errStreamAborted) {
		t.Fatal("errStreamAborted should be hot-path")
	}
	if !isHotPathConnectionError(io.EOF) {
		t.Fatal("io.EOF should be hot-path")
	}
	if !isHotPathConnectionError(net.ErrClosed) {
		t.Fatal("net.ErrClosed should be hot-path")
	}
	if !isHotPathConnectionError(&net.OpError{Op: "read", Err: errors.New("boom")}) {
		t.Fatal("read net.OpError should be hot-path")
	}
	if isHotPathConnectionError(&net.OpError{Op: "dial", Err: errors.New("boom")}) {
		t.Fatal("dial net.OpError should not be hot-path")
	}
}
