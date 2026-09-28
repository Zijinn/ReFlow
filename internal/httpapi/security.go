package httpapi

import (
	"context"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"

	"github.com/Zijinn/ReFlow/internal/domain"
	"github.com/Zijinn/ReFlow/internal/storage"
)

type SecurityConfig struct {
	RequireDeviceAuth bool
	AllowedOrigins    []string
	TrustedProxies    []netip.Prefix
}

type ingressClass uint8

const (
	ingressExternal ingressClass = iota
	ingressLoopback
	ingressProxy
)

type deviceContextKey struct{}

func (s *Server) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ingress := s.classifyIngress(r.RemoteAddr)
		// DNS-rebinding guard: a web page served from an attacker-controlled
		// domain can rebind it to 127.0.0.1, after which its same-origin
		// requests arrive from a loopback peer with the attacker's Host header
		// and would otherwise inherit loopback trust (no device auth, CORS
		// origin == Host). Direct loopback traffic only serves loopback hosts.
		// A configured proxy peer is instead an explicit ingress boundary and
		// may forward external Host values, but protected APIs still require a
		// device token. Forwarded client IP headers are never authorization input.
		if ingress == ingressLoopback && !isLoopbackHostHeader(r.Host) {
			writeProblem(w, r, http.StatusForbidden, "host_not_allowed", "Host not allowed", "The loopback listener only accepts requests addressed to a loopback host.")
			return
		}
		if !strings.HasPrefix(r.URL.Path, "/api/") || publicAPIPath(r.URL.Path) || !s.deviceAuthRequired(ingress) {
			next.ServeHTTP(w, r)
			return
		}
		token := deviceTokenFromRequest(r)
		if token == "" {
			writeProblem(w, r, http.StatusUnauthorized, "authentication_required", "Authentication required", "Pair this device or provide its bearer token.")
			return
		}
		device, err := storage.AuthenticateDevice(r.Context(), s.db, token)
		if err != nil {
			writeProblem(w, r, http.StatusUnauthorized, "invalid_device_token", "Invalid device token", "The device token is invalid or has been revoked.")
			return
		}
		ctx := context.WithValue(r.Context(), deviceContextKey{}, device)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func deviceTokenFromRequest(r *http.Request) string {
	const bearerPrefix = "Bearer "
	authorization := r.Header.Get("Authorization")
	if strings.HasPrefix(authorization, bearerPrefix) {
		return strings.TrimSpace(strings.TrimPrefix(authorization, bearerPrefix))
	}
	if cookie, err := r.Cookie("reflow_device"); err == nil {
		return cookie.Value
	}
	return ""
}

func (s *Server) cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := strings.TrimSpace(r.Header.Get("Origin"))
		if origin == "" {
			next.ServeHTTP(w, r)
			return
		}
		if !s.originAllowed(origin, r.Host) {
			writeProblem(w, r, http.StatusForbidden, "origin_not_allowed", "Origin not allowed", "This origin is not trusted by ReFlow Server.")
			return
		}
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Access-Control-Allow-Credentials", "true")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Request-ID")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, PUT, DELETE, OPTIONS")
		w.Header().Add("Vary", "Origin")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) originAllowed(origin, requestHost string) bool {
	parsed, err := url.Parse(origin)
	if err == nil && strings.EqualFold(parsed.Host, requestHost) && (parsed.Scheme == "http" || parsed.Scheme == "https") {
		return true
	}
	for _, allowed := range s.security.AllowedOrigins {
		if origin == strings.TrimRight(strings.TrimSpace(allowed), "/") {
			return true
		}
	}
	return false
}

func publicAPIPath(path string) bool {
	return path == "/api/v1/status" || path == "/api/v1/devices/pair"
}

func (s *Server) deviceAuthRequired(ingress ingressClass) bool {
	return ingress == ingressProxy || (ingress == ingressExternal && s.security.RequireDeviceAuth)
}

func (s *Server) classifyIngress(remoteAddress string) ingressClass {
	address, ok := remoteIP(remoteAddress)
	if !ok {
		return ingressExternal
	}
	for _, proxy := range s.security.TrustedProxies {
		if proxy.Contains(address) {
			return ingressProxy
		}
	}
	if address.IsLoopback() {
		return ingressLoopback
	}
	return ingressExternal
}

func remoteIP(remoteAddress string) (netip.Addr, bool) {
	host, _, err := net.SplitHostPort(remoteAddress)
	if err != nil {
		return netip.Addr{}, false
	}
	address, err := netip.ParseAddr(strings.Trim(host, "[]"))
	return address, err == nil
}

func isLoopbackRemote(remoteAddress string) bool {
	address, ok := remoteIP(remoteAddress)
	return ok && address.IsLoopback()
}

func isLoopbackHostHeader(hostHeader string) bool {
	host := hostHeader
	if h, _, err := net.SplitHostPort(hostHeader); err == nil {
		host = h
	}
	host = strings.ToLower(strings.Trim(strings.TrimSpace(host), "[]"))
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func deviceFromContext(ctx context.Context) *domain.Device {
	device, ok := ctx.Value(deviceContextKey{}).(domain.Device)
	if !ok {
		return nil
	}
	return &device
}
