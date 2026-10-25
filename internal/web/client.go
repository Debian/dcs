package web

import (
	"context"
	"net"
	"net/http"
	"strings"

	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
)

// client identifies who started a query, for logging. dcs-web runs behind a
// reverse proxy (Caddy) on localhost, so the client address must be taken from
// X-Forwarded-For.
type client struct {
	ip        string
	userAgent string
	entry     string // e.g. /search, /events, /api, grpc
	apiKey    string // apikeys.Key.Subject, empty for anonymous requests
}

// String returns a compact representation for the existing log.Printf lines.
func (c client) String() string {
	if c.apiKey != "" {
		return c.apiKey + "@" + c.ip
	}
	return c.ip
}

// maxUserAgentLen bounds the log line size for clients sending huge headers.
const maxUserAgentLen = 256

func isLoopback(ip string) bool {
	parsed := net.ParseIP(ip)
	return parsed != nil && parsed.IsLoopback()
}

// clientIP returns the client address, trusting the forwarded address only
// when the direct peer is the local reverse proxy. The last X-Forwarded-For
// element is the one appended by our proxy.
func clientIP(remoteAddr string, forwardedFor []string) string {
	ip := remoteAddr
	if host, _, err := net.SplitHostPort(remoteAddr); err == nil {
		ip = host
	}
	if !isLoopback(ip) || len(forwardedFor) == 0 {
		return ip
	}
	last := forwardedFor[len(forwardedFor)-1]
	if idx := strings.LastIndexByte(last, ','); idx > -1 {
		last = last[idx+1:]
	}
	if last = strings.TrimSpace(last); last != "" {
		return last
	}
	return ip
}

func truncateUserAgent(ua string) string {
	if len(ua) > maxUserAgentLen {
		return ua[:maxUserAgentLen]
	}
	return ua
}

func clientFromRequest(r *http.Request, entry, apiKey string) client {
	return client{
		ip:        clientIP(r.RemoteAddr, r.Header.Values("X-Forwarded-For")),
		userAgent: truncateUserAgent(r.UserAgent()),
		entry:     entry,
		apiKey:    apiKey,
	}
}

func clientFromGRPC(ctx context.Context, apiKey string) client {
	c := client{
		ip:     "unknown",
		entry:  "grpc",
		apiKey: apiKey,
	}
	md, _ := metadata.FromIncomingContext(ctx)
	if p, ok := peer.FromContext(ctx); ok && p.Addr != nil {
		c.ip = clientIP(p.Addr.String(), md.Get("x-forwarded-for"))
	}
	if ua := md.Get("user-agent"); len(ua) > 0 {
		c.userAgent = truncateUserAgent(ua[0])
	}
	return c
}
