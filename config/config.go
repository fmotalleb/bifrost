// Package config provides runtime configuration structures and validation.
package config

import (
	"net"
	"net/netip"
	"time"
)

// Config contains the runtime settings for bifrost.
type Config struct {
	Listen  netip.AddrPort   `mapstructure:"listen"`
	Server  netip.AddrPort   `mapstructure:"server"`
	Metrics netip.AddrPort   `mapstructure:"metrics"`
	Socks   SocksConfig      `mapstructure:"socks"`
	Cache   CacheConfig      `mapstructure:"cache"`
	IFaces  map[string]Iface `mapstructure:"ifaces" validate:"required"`
}

// CacheConfig controls source IP lookup caching behavior.
type CacheConfig struct {
	TTL      time.Duration `mapstructure:"ttl" validate:"gte=0"`
	Prefetch bool          `mapstructure:"prefetch"`
}

// Iface defines configuration for a network interface.
type Iface struct {
	Weight   int    `mapstructure:"weight" validate:"gt=0"`
	SourceIP net.IP `mapstructure:"source_ip"`
}

// SocksConfig controls optional SOCKS5 authentication settings.
type SocksConfig struct {
	Listen   netip.AddrPort `mapstructure:"listen"`
	Username string         `mapstructure:"username"`
	Password string         `mapstructure:"password"`
}
