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
	ip, _, err := net.SplitHostPort(x.Request.RemoteAddr)
	if err != nil {
		return ""
	}
	return ip
}
