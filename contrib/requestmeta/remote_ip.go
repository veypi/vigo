package requestmeta

import (
	"net"
	"strings"

	"github.com/veypi/vigo"
)

func RemoteIP(x *vigo.X) string {
	if x == nil || x.Request == nil {
		return ""
	}
	ip, _, err := net.SplitHostPort(x.Request.RemoteAddr)
	if err != nil {
		return ""
	}
	if shouldTrustProxy(x, ip) {
		forwarded := x.Request.Header.Get("X-Forwarded-For")
		if forwarded != "" {
			for _, part := range strings.Split(forwarded, ",") {
				candidate := strings.TrimSpace(part)
				if parsed := net.ParseIP(candidate); parsed != nil {
					return parsed.String()
				}
			}
		}
		if realIP := strings.TrimSpace(x.Request.Header.Get("X-Real-IP")); realIP != "" {
			if parsed := net.ParseIP(realIP); parsed != nil {
				return parsed.String()
			}
		}
	}
	return ip
}

func shouldTrustProxy(x *vigo.X, remoteIP string) bool {
	cfg := x.Config()
	if cfg == nil || len(cfg.TrustedProxies) == 0 {
		return false
	}
	clientIP := net.ParseIP(remoteIP)
	if clientIP == nil {
		return false
	}
	for _, entry := range cfg.TrustedProxies {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		if ip := net.ParseIP(entry); ip != nil {
			if ip.Equal(clientIP) {
				return true
			}
			continue
		}
		if _, network, err := net.ParseCIDR(entry); err == nil && network.Contains(clientIP) {
			return true
		}
	}
	return false
}
