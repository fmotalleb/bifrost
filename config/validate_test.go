package config

import (
	"net"
	"net/netip"
	"strings"
	"testing"
	"time"
)

func validConfig() Config {
	return Config{
		Listen: netip.MustParseAddrPort("127.0.0.1:8080"),
		Server: netip.MustParseAddrPort("1.1.1.1:443"),
		Cache:  CacheConfig{TTL: time.Second},
		IFaces: map[string]Iface{
			"eth0": {
				Weight:   1,
				SourceIP: net.ParseIP("8.8.8.8"),
			},
		},
	}
}

func TestValidateSuccess(t *testing.T) {
	cfg := validConfig()
	if err := Validate(cfg); err != nil {
		t.Fatalf("Validate returned error for valid config: %v", err)
	}
}

func TestValidateSocksPairing(t *testing.T) {
	cfg := validConfig()
	cfg.Socks.Username = "user"
	cfg.Socks.Password = ""
	err := Validate(cfg)
	if err == nil || !strings.Contains(err.Error(), "must be set together") {
		t.Fatalf("expected socks pairing error, got: %v", err)
	}
}

func TestValidateInvalidAddresses(t *testing.T) {
	cfg := validConfig()
	cfg.Listen = netip.AddrPort{}
	err := Validate(cfg)
	if err == nil || !strings.Contains(err.Error(), "listen must be a valid address:port") {
		t.Fatalf("expected listen validation error, got: %v", err)
	}
}

func TestValidateSourceIPRules(t *testing.T) {
	cfg := validConfig()
	cfg.IFaces["eth0"] = Iface{
		Weight:   1,
		SourceIP: net.IPv4zero,
	}
	err := Validate(cfg)
	if err == nil || !strings.Contains(err.Error(), "cannot be unspecified") {
		t.Fatalf("expected source_ip unspecified error, got: %v", err)
	}
}
