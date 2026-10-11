// Package exit implements exit node functionality for Muti Metroo.
package exit

import (
	"context"
	"errors"
	"net"
	"sync"
	"time"
)

// DNSConfig contains DNS resolver configuration.
type DNSConfig struct {
	Servers []string
	Timeout time.Duration
}

// DefaultDNSConfig returns sensible defaults.
// By default, no servers are configured which means the system resolver is used.
// This allows resolution of local domains (e.g., printer.local) that public DNS cannot resolve.
func DefaultDNSConfig() DNSConfig {
	return DNSConfig{
		Servers: []string{}, // Empty = use system resolver
		Timeout: 5 * time.Second,
	}
}

// Resolver handles DNS resolution.
type Resolver struct {
	cfg    DNSConfig
	mu     sync.RWMutex
	cache  map[string]*cacheEntry
	dialer *net.Dialer
}

type cacheEntry struct {
	ip        net.IP
	expiresAt time.Time
}

// NewResolver creates a new DNS resolver.
// If no servers are configured, the system resolver is used.
func NewResolver(cfg DNSConfig) *Resolver {
	if cfg.Timeout == 0 {
		cfg.Timeout = DefaultDNSConfig().Timeout
	}

	return &Resolver{
		cfg:   cfg,
		cache: make(map[string]*cacheEntry),
		dialer: &net.Dialer{
			Timeout: cfg.Timeout,
		},
	}
}

// Resolve resolves a domain name to an IP address.
func (r *Resolver) Resolve(ctx context.Context, domain string) (net.IP, error) {
	// Check if it's already an IP
	if ip := net.ParseIP(domain); ip != nil {
		return ip, nil
	}

	// Check cache
	if ip := r.getCached(domain); ip != nil {
		return ip, nil
	}

	// Set timeout
	resolveCtx, cancel := context.WithTimeout(ctx, r.cfg.Timeout)
	defer cancel()

	var resolver *net.Resolver

	if len(r.cfg.Servers) > 0 {
		// Use explicitly configured DNS servers
		resolver = &net.Resolver{
			PreferGo: true,
			Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
				// Try each server until one works
				var lastErr error
				for _, server := range r.cfg.Servers {
					conn, err := r.dialer.DialContext(ctx, "udp", server)
					if err == nil {
						return conn, nil
					}
					lastErr = err
				}
				return nil, lastErr
			},
		}
	} else {
		// Use system default resolver (supports local domains like .local)
		resolver = net.DefaultResolver
	}

	// Resolve
	addrs, err := resolver.LookupIPAddr(resolveCtx, domain)
	if err != nil {
		return nil, err
	}

	if len(addrs) == 0 {
		return nil, errors.New("no addresses found")
	}

	// Prefer IPv4
	var selectedIP net.IP
	for _, addr := range addrs {
		if ipv4 := addr.IP.To4(); ipv4 != nil {
			selectedIP = ipv4
			break
		}
	}
	if selectedIP == nil {
		selectedIP = addrs[0].IP
	}

	// Cache for 5 minutes
	r.setCache(domain, selectedIP, 5*time.Minute)

	return selectedIP, nil
}

// getCached returns a cached IP if valid.
// Expired entries are deleted to prevent unbounded cache growth.
func (r *Resolver) getCached(domain string) net.IP {
	r.mu.Lock()
	defer r.mu.Unlock()

	entry, ok := r.cache[domain]
	if !ok {
		return nil
	}

	if time.Now().After(entry.expiresAt) {
		delete(r.cache, domain)
		return nil
	}

	return entry.ip
}

// setCache stores an IP in the cache.
func (r *Resolver) setCache(domain string, ip net.IP, ttl time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.cache[domain] = &cacheEntry{
		ip:        ip,
		expiresAt: time.Now().Add(ttl),
	}
}

// ClearCache clears the DNS cache.
func (r *Resolver) ClearCache() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cache = make(map[string]*cacheEntry)
}

// CacheSize returns the number of cached entries.
func (r *Resolver) CacheSize() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.cache)
}
