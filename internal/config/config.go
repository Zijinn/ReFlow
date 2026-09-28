package config

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const defaultAddress = "127.0.0.1:7381"

type Config struct {
	Address        string
	DataDir        string
	DBPath         string
	MasterKeyPath  string
	WebDir         string
	TLSCertPath    string
	TLSKeyPath     string
	LogLevel       string
	LANMode        bool
	RSSHubBase     string
	AllowedOrigins []string
	TrustedProxies []netip.Prefix
}

func Load() (Config, error) {
	dataDir, err := defaultDataDir()
	if err != nil {
		return Config{}, err
	}

	cfg := Config{
		Address:        envOr("REFLOW_ADDR", defaultAddress),
		DataDir:        envOr("REFLOW_DATA_DIR", dataDir),
		WebDir:         envOr("REFLOW_WEB_DIR", "web/dist"),
		TLSCertPath:    strings.TrimSpace(os.Getenv("REFLOW_TLS_CERT_PATH")),
		TLSKeyPath:     strings.TrimSpace(os.Getenv("REFLOW_TLS_KEY_PATH")),
		LogLevel:       strings.ToLower(envOr("REFLOW_LOG_LEVEL", "info")),
		RSSHubBase:     envOr("REFLOW_RSSHUB_BASE", "https://rsshub.app"),
		AllowedOrigins: splitList(os.Getenv("REFLOW_ALLOWED_ORIGINS")),
	}
	cfg.TrustedProxies, err = parseTrustedProxies(os.Getenv("REFLOW_TRUSTED_PROXIES"))
	if err != nil {
		return Config{}, err
	}

	if raw := os.Getenv("REFLOW_LAN_MODE"); raw != "" {
		cfg.LANMode, err = strconv.ParseBool(raw)
		if err != nil {
			return Config{}, fmt.Errorf("parse REFLOW_LAN_MODE: %w", err)
		}
	}

	if dbPath := os.Getenv("REFLOW_DB_PATH"); dbPath != "" {
		cfg.DBPath = dbPath
	} else {
		cfg.DBPath = filepath.Join(cfg.DataDir, "reflow.db")
	}
	if keyPath := os.Getenv("REFLOW_MASTER_KEY_PATH"); keyPath != "" {
		cfg.MasterKeyPath = keyPath
	} else {
		cfg.MasterKeyPath = filepath.Join(cfg.DataDir, "master.key")
	}

	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	if err := os.MkdirAll(cfg.DataDir, 0o700); err != nil {
		return Config{}, fmt.Errorf("create data directory: %w", err)
	}
	return cfg, nil
}

func (c Config) Validate() error {
	host, _, err := net.SplitHostPort(c.Address)
	if err != nil {
		return fmt.Errorf("invalid REFLOW_ADDR: %w", err)
	}

	if !c.LANMode && !isLoopbackHost(host) {
		return errors.New("non-loopback REFLOW_ADDR requires REFLOW_LAN_MODE=true")
	}

	switch c.LogLevel {
	case "debug", "info", "warn", "error":
	default:
		return fmt.Errorf("unsupported REFLOW_LOG_LEVEL %q", c.LogLevel)
	}

	if strings.TrimSpace(c.DataDir) == "" {
		return errors.New("data directory cannot be empty")
	}
	if strings.TrimSpace(c.DBPath) == "" {
		return errors.New("database path cannot be empty")
	}
	if strings.TrimSpace(c.MasterKeyPath) == "" {
		return errors.New("master key path cannot be empty")
	}
	if (c.TLSCertPath == "") != (c.TLSKeyPath == "") {
		return errors.New("REFLOW_TLS_CERT_PATH and REFLOW_TLS_KEY_PATH must be set together")
	}
	rssHubBase := c.RSSHubBase
	if strings.TrimSpace(rssHubBase) == "" {
		rssHubBase = "https://rsshub.app"
	}
	rssHubURL, err := url.Parse(rssHubBase)
	if err != nil || rssHubURL.Hostname() == "" || (rssHubURL.Scheme != "http" && rssHubURL.Scheme != "https") {
		return errors.New("REFLOW_RSSHUB_BASE must be an HTTP or HTTPS URL")
	}
	for _, origin := range c.AllowedOrigins {
		parsed, err := url.Parse(origin)
		if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") || (parsed.Path != "" && parsed.Path != "/") {
			return fmt.Errorf("invalid REFLOW_ALLOWED_ORIGINS value %q", origin)
		}
	}
	for _, proxy := range c.TrustedProxies {
		if !proxy.IsValid() || proxy != proxy.Masked() {
			return fmt.Errorf("invalid REFLOW_TRUSTED_PROXIES prefix %q", proxy)
		}
	}
	return nil
}

func parseTrustedProxies(value string) ([]netip.Prefix, error) {
	items := splitList(value)
	proxies := make([]netip.Prefix, 0, len(items))
	for _, item := range items {
		prefix, err := netip.ParsePrefix(item)
		if err != nil {
			address, addressErr := netip.ParseAddr(item)
			if addressErr != nil {
				return nil, fmt.Errorf("invalid REFLOW_TRUSTED_PROXIES value %q", item)
			}
			prefix = netip.PrefixFrom(address, address.BitLen())
		}
		if prefix != prefix.Masked() {
			return nil, fmt.Errorf("REFLOW_TRUSTED_PROXIES CIDR %q has host bits set", item)
		}
		proxies = append(proxies, prefix)
	}
	return proxies, nil
}

func defaultDataDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve user config directory: %w", err)
	}
	return filepath.Join(base, "ReFlow"), nil
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func isLoopbackHost(host string) bool {
	host = strings.Trim(host, "[]")
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func splitList(value string) []string {
	items := make([]string, 0)
	for _, item := range strings.Split(value, ",") {
		item = strings.TrimRight(strings.TrimSpace(item), "/")
		if item != "" {
			items = append(items, item)
		}
	}
	return items
}
