package proxy

import (
	"fmt"
	"net"
	"sync"
	"time"
)

type ifaceBinding struct {
	name     string
	index    int
	sourceIP net.IP
}

type ipCacheEntry struct {
	ip        net.IP
	expiresAt time.Time
}

// IPCache caches resolved source IPs per interface with optional prefetch mode.
type IPCache struct {
	ttl      time.Duration
	prefetch bool
	mu       sync.RWMutex
	entries  map[int]ipCacheEntry
}

// NewIPCache creates an IP cache and optionally prefetches all interface source IPs.
func NewIPCache(ttl time.Duration, prefetch bool, bindings map[string]ifaceBinding, preferIPv4 bool) (*IPCache, error) {
	cache := &IPCache{
		ttl:      ttl,
		prefetch: prefetch,
		entries:  make(map[int]ipCacheEntry, len(bindings)),
	}

	for _, binding := range bindings {
		if binding.sourceIP != nil {
			continue
		}

		if !prefetch {
			continue
		}

		resolvedIP, err := ResolveBindIPByIndex(binding.index, preferIPv4)
		if err != nil {
			return nil, fmt.Errorf("prefetch bind ip for %q (index=%d): %w", binding.name, binding.index, err)
		}
		cache.entries[binding.index] = ipCacheEntry{ip: cloneIP(resolvedIP)}
	}

	return cache, nil
}

// GetBindIP returns the source IP for a binding using source_ip override, prefetch cache, or TTL cache refresh.
func (c *IPCache) GetBindIP(binding ifaceBinding, preferIPv4 bool) (net.IP, error) {
	if binding.sourceIP != nil {
		return cloneIP(binding.sourceIP), nil
	}

	if c.prefetch {
		c.mu.RLock()
		entry, ok := c.entries[binding.index]
		c.mu.RUnlock()
		if !ok || entry.ip == nil {
			return nil, fmt.Errorf("prefetched bind ip not found for %q (index=%d)", binding.name, binding.index)
		}
		return cloneIP(entry.ip), nil
	}

	if c.ttl > 0 {
		now := time.Now()
		c.mu.RLock()
		entry, ok := c.entries[binding.index]
		c.mu.RUnlock()
		if ok && entry.ip != nil && now.Before(entry.expiresAt) {
			return cloneIP(entry.ip), nil
		}
	}

	resolvedIP, err := ResolveBindIPByIndex(binding.index, preferIPv4)
	if err != nil {
		return nil, err
	}

	if c.ttl > 0 {
		c.mu.Lock()
		c.entries[binding.index] = ipCacheEntry{
			ip:        cloneIP(resolvedIP),
			expiresAt: time.Now().Add(c.ttl),
		}
		c.mu.Unlock()
	}

	return cloneIP(resolvedIP), nil
}

func cloneIP(ip net.IP) net.IP {
	if ip == nil {
		return nil
	}

	cloned := make(net.IP, len(ip))
	copy(cloned, ip)
	return cloned
}
