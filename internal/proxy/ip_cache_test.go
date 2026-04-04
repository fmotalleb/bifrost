package proxy

import (
	"net"
	"strings"
	"testing"
	"time"
)

func TestCloneIP(t *testing.T) {
	orig := net.ParseIP("10.1.1.2")
	cloned := cloneIP(orig)
	if cloned == nil || cloned.String() != "10.1.1.2" {
		t.Fatalf("cloneIP returned %v", cloned)
	}

	cloned[0] = 42
	if orig[0] == 42 {
		t.Fatal("cloneIP must return a deep copy")
	}
}

func TestGetBindIPUsesSourceIPOverride(t *testing.T) {
	cache := &IPCache{}
	binding := ifaceBinding{name: "eth0", index: 1, sourceIP: net.ParseIP("10.0.0.7")}

	got, err := cache.GetBindIP(binding, true)
	if err != nil {
		t.Fatalf("GetBindIP returned error: %v", err)
	}
	if got.String() != "10.0.0.7" {
		t.Fatalf("GetBindIP returned %q, want 10.0.0.7", got.String())
	}

	got[0] = 33
	if binding.sourceIP[0] == 33 {
		t.Fatal("GetBindIP must return cloned sourceIP")
	}
}

func TestGetBindIPPrefetchMissingEntry(t *testing.T) {
	cache := &IPCache{
		prefetch: true,
		entries:  map[int]ipCacheEntry{},
	}
	_, err := cache.GetBindIP(ifaceBinding{name: "eth0", index: 99}, true)
	if err == nil {
		t.Fatal("expected missing prefetch entry error")
	}
	if !strings.Contains(err.Error(), "prefetched bind ip not found") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGetBindIPTTLUsesCachedEntry(t *testing.T) {
	cache := &IPCache{
		ttl:     time.Minute,
		entries: map[int]ipCacheEntry{2: {ip: net.ParseIP("10.0.0.9"), expiresAt: time.Now().Add(time.Minute)}},
	}
	got, err := cache.GetBindIP(ifaceBinding{name: "eth1", index: 2}, true)
	if err != nil {
		t.Fatalf("GetBindIP returned error: %v", err)
	}
	if got.String() != "10.0.0.9" {
		t.Fatalf("GetBindIP cached result = %q, want 10.0.0.9", got.String())
	}
}
