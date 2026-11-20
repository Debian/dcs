package web

import (
	"net"
	"net/http"
	"net/netip"
	"strings"
)

func anonymousKey(apiKey string) bool {
	switch apiKey {
	case "program!github.com/Debian/dcs/cmd/dcs",
		"salsa!https://salsa.debian.org/debian/codesearch-cli":
		// As of November 2026, the API keys which dcs and codesearch-cli send
		// only identifies the traffic, but has no effect on query ownership.
		return true

	default:
		// A self-service API key (via salsa.debian.org login),
		// or a manually created API key. Treat it as one owner,
		// so that as many different users as possible can query DCS
		// even when the service might need to throttle / block.
		// https://codesearch.debian.net/apikeys/
		return false
	}
}

// client identifies who started a query, for logging. dcs-web runs behind a
// reverse proxy (Caddy) on localhost, so the client address must be taken from
// X-Forwarded-For.
type client struct {
	ip        string
	userAgent string
	entry     string // e.g. /search, /events, /api
	apiKey    string // apikeys.Key.Subject, empty for anonymous requests
}

// personalKey reports whether the request came from a personal API key
// (self-service created via /apikeys/, or manually allocated).
func (c client) personalKey() bool {
	return c.apiKey != "" && !anonymousKey(c.apiKey)
}

func (c client) owner() string {
	// For IPv4, one IP address is one device (often many).
	const bits4 = 32
	// For IPv6, one /64 represents one device (SLAAC).
	const bits6 = 64
	network := c.network(bits4, bits6)
	if c.personalKey() {
		// A personal key grants more searches from the same IP.
		return c.apiKey + "@" + network
	}
	// We might make this stricter for certain IP ranges or ASNs.
	return network
}

func (c client) resultsOwner() string {
	// For IPv4 addresses, we map each address to
	// its containing network (CIDR /16 granularity).
	// This is a good compromise between effectiveness
	// (grouping individual IP addresses to their owner)
	// and over-matching unrelated IP addresses.
	const bits4 = 16
	// For IPv6, look at the entire /48 network.
	const bits6 = 48
	network := c.network(bits4, bits6)
	if c.personalKey() {
		// A personal key gets its own owner slots on the same network.
		return c.apiKey + "@" + network
	}
	return network
}

func (c client) network(bits4, bits6 int) string {
	addr, err := netip.ParseAddr(c.ip)
	if err != nil {
		return c.ip
	}
	// addr might be an IPv6 address containing a mapped IPv4
	addr = addr.Unmap()
	bits := bits4
	if addr.Is6() {
		bits = bits6
	}
	prefix, err := addr.Prefix(bits)
	if err != nil {
		return c.ip
	}
	return prefix.String()
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
