package feed

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"strings"
	"time"
)

var ErrBlockedAddress = errors.New("address is blocked by fetch policy")

type URLPolicy struct {
	Resolver     *net.Resolver
	AllowPrivate bool
	DialTimeout  time.Duration
}

func DefaultURLPolicy() URLPolicy {
	return URLPolicy{
		Resolver:    net.DefaultResolver,
		DialTimeout: 10 * time.Second,
	}
}

func (p URLPolicy) Validate(ctx context.Context, target *url.URL) error {
	if target == nil || (target.Scheme != "http" && target.Scheme != "https") || target.Hostname() == "" {
		return ErrInvalidURL
	}
	if target.User != nil {
		return fmt.Errorf("%w: embedded credentials", ErrBlockedAddress)
	}
	addresses, err := p.lookup(ctx, target.Hostname())
	if err != nil {
		return err
	}
	for _, address := range addresses {
		if !p.AllowPrivate && isBlockedAddress(address) {
			return fmt.Errorf("%w: %s", ErrBlockedAddress, address.String())
		}
	}
	return nil
}

func (p URLPolicy) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, fmt.Errorf("parse dial address: %w", err)
	}
	addresses, err := p.lookup(ctx, host)
	if err != nil {
		return nil, err
	}
	timeout := p.DialTimeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	dialer := net.Dialer{Timeout: timeout, KeepAlive: 30 * time.Second}
	var failures []error
	for _, candidate := range addresses {
		if !p.AllowPrivate && isBlockedAddress(candidate) {
			failures = append(failures, fmt.Errorf("%w: %s", ErrBlockedAddress, candidate.String()))
			continue
		}
		connection, dialErr := dialer.DialContext(ctx, network, net.JoinHostPort(candidate.String(), port))
		if dialErr == nil {
			return connection, nil
		}
		failures = append(failures, dialErr)
	}
	if len(failures) == 0 {
		return nil, fmt.Errorf("resolve %s: no addresses", host)
	}
	return nil, errors.Join(failures...)
}

func (p URLPolicy) lookup(ctx context.Context, host string) ([]netip.Addr, error) {
	host = strings.Trim(host, "[]")
	if parsed, err := netip.ParseAddr(host); err == nil {
		return []netip.Addr{parsed.Unmap()}, nil
	}
	resolver := p.Resolver
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	addresses, err := resolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return nil, fmt.Errorf("resolve %s: %w", host, err)
	}
	if len(addresses) == 0 {
		return nil, fmt.Errorf("resolve %s: no addresses", host)
	}
	result := make([]netip.Addr, 0, len(addresses))
	for _, address := range addresses {
		result = append(result, address.Unmap())
	}
	return result, nil
}

func isBlockedAddress(address netip.Addr) bool {
	if !address.IsValid() || address.IsUnspecified() || address.IsLoopback() || address.IsPrivate() ||
		address.IsLinkLocalUnicast() || address.IsLinkLocalMulticast() || address.IsMulticast() {
		return true
	}
	for _, prefix := range blockedPrefixes {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}

var blockedPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("100::/64"),
	netip.MustParsePrefix("2001:2::/48"),
	netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("2001::/32"),
	netip.MustParsePrefix("2001:10::/28"),
	netip.MustParsePrefix("2001:20::/28"),
	netip.MustParsePrefix("2002::/16"),
	netip.MustParsePrefix("64:ff9b::/96"),
	netip.MustParsePrefix("64:ff9b:1::/48"),
}
