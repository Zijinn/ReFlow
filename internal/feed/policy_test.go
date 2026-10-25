package feed

import (
	"net/netip"
	"testing"
)

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
