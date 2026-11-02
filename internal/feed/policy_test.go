package feed

import (
	"net/netip"
	"testing"
)

func TestPrivateAccessStillBlocksLinkLocalAndMetadata(t *testing.T) {
	policy := URLPolicy{AllowPrivate: true}
	for _, raw := range []string{"127.0.0.1", "10.1.2.3", "192.168.1.1", "fd00::1"} {
		if policy.blocked(netip.MustParseAddr(raw)) {
			t.Errorf("%s should remain reachable with private access", raw)
		}
	}
	for _, raw := range []string{"169.254.169.254", "fd00:ec2::254", "fe80::1", "0.0.0.0"} {
		if !policy.blocked(netip.MustParseAddr(raw)) {
			t.Errorf("%s must stay blocked even with private access", raw)
		}
	}
}

func TestBlockedAddressesCoverIPv6TranslationPrefixes(t *testing.T) {
	blocked := []string{
		"64:ff9b::a9fe:a9fe",
		"64:ff9b:1::a9fe:a9fe",
		"2002:a9fe:a9fe::1",
		"2001::1",
		"2001:2::1",
	}
	for _, raw := range blocked {
		if !isBlockedAddress(netip.MustParseAddr(raw)) {
			t.Errorf("%s must be blocked", raw)
		}
	}
	if isBlockedAddress(netip.MustParseAddr("2606:4700:4700::1111")) {
		t.Fatal("a public IPv6 address must not be blocked")
	}
}
