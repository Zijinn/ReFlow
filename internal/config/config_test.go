package config

import (
	"net/netip"
	"testing"
)

func TestValidateRejectsLANAddressWithoutLANMode(t *testing.T) {
	cfg := Config{
		Address:       "0.0.0.0:7381",
		DataDir:       t.TempDir(),
		DBPath:        t.TempDir() + "/reflow.db",
		MasterKeyPath: t.TempDir() + "/master.key",
		LogLevel:      "info",
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected non-loopback address to be rejected")
	}
}

func TestValidateAllowsLoopback(t *testing.T) {
	cfg := Config{
		Address:       "127.0.0.1:7381",
		DataDir:       t.TempDir(),
		DBPath:        t.TempDir() + "/reflow.db",
		MasterKeyPath: t.TempDir() + "/master.key",
		LogLevel:      "info",
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected loopback config to be valid: %v", err)
	}
}

func TestValidateAllowsLANAddressInLANMode(t *testing.T) {
	cfg := Config{
		Address:       "0.0.0.0:7381",
		DataDir:       t.TempDir(),
		DBPath:        t.TempDir() + "/reflow.db",
		MasterKeyPath: t.TempDir() + "/master.key",
		LogLevel:      "info",
		LANMode:       true,
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected LAN config to be valid: %v", err)
	}
}

func TestValidateRequiresTLSCertificateAndKeyTogether(t *testing.T) {
	cfg := Config{
		Address: "0.0.0.0:7381", DataDir: t.TempDir(), DBPath: t.TempDir() + "/reflow.db",
		MasterKeyPath: t.TempDir() + "/master.key", LogLevel: "info", LANMode: true,
		TLSCertPath: t.TempDir() + "/server.crt",
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected a missing TLS key to be rejected")
	}
}

func TestParseTrustedProxiesAcceptsExactIPsAndCanonicalCIDRs(t *testing.T) {
	proxies, err := parseTrustedProxies("127.0.0.1, 10.20.0.0/16, ::1, 2001:db8::/32")
	if err != nil {
		t.Fatal(err)
	}
	want := []netip.Prefix{
		netip.MustParsePrefix("127.0.0.1/32"), netip.MustParsePrefix("10.20.0.0/16"),
		netip.MustParsePrefix("::1/128"), netip.MustParsePrefix("2001:db8::/32"),
	}
	if len(proxies) != len(want) {
		t.Fatalf("got %d proxies, want %d", len(proxies), len(want))
	}
	for index := range want {
		if proxies[index] != want[index] {
			t.Fatalf("proxy %d = %s, want %s", index, proxies[index], want[index])
		}
	}
}

func TestParseTrustedProxiesRejectsNamesAndNonCanonicalCIDRs(t *testing.T) {
	for _, value := range []string{"localhost", "proxy.example", "10.0.0.1/8", "192.168.1.999"} {
		if _, err := parseTrustedProxies(value); err == nil {
			t.Fatalf("expected %q to be rejected", value)
		}
	}
}

func TestLoadParsesTrustedProxies(t *testing.T) {
	t.Setenv("REFLOW_DATA_DIR", t.TempDir())
	t.Setenv("REFLOW_TRUSTED_PROXIES", "127.0.0.1,10.0.0.0/8")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.TrustedProxies) != 2 || !cfg.TrustedProxies[1].Contains(netip.MustParseAddr("10.2.3.4")) {
		t.Fatalf("unexpected trusted proxies: %v", cfg.TrustedProxies)
	}
}
