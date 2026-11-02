package service

import (
	"errors"
	"net"
	"net/url"
	"strings"
)

// validateRemoteTransport keeps passwords, bearer tokens, and AI API keys off
// plaintext links. Loopback services are exempt; self-hosted LAN endpoints need
// the account's explicit private-network opt-in.
func validateRemoteTransport(endpoint string, allowPrivate bool) error {
	parsed, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil || parsed.Host == "" {
		return errors.New("endpoint is not a valid URL")
	}
	if strings.EqualFold(parsed.Scheme, "https") || allowPrivate {
		return nil
	}
	host := strings.Trim(parsed.Hostname(), "[]")
	if strings.EqualFold(host, "localhost") {
		return nil
	}
	if address := net.ParseIP(host); address != nil && address.IsLoopback() {
		return nil
	}
	return errors.New("remote endpoints must use HTTPS unless private-network access is enabled")
}
