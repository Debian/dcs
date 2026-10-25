package web

import "testing"

func TestClientIP(t *testing.T) {
	for _, tt := range []struct {
		name         string
		remoteAddr   string
		forwardedFor []string
		want         string
	}{
		{
			name:       "direct",
			remoteAddr: "203.0.113.5:1234",
			want:       "203.0.113.5",
		},
		{
			name:         "direct ignores forwarded",
			remoteAddr:   "203.0.113.5:1234",
			forwardedFor: []string{"198.51.100.1"},
			want:         "203.0.113.5",
		},
		{
			name:         "proxy v4",
			remoteAddr:   "127.0.0.1:50920",
			forwardedFor: []string{"198.51.100.1"},
			want:         "198.51.100.1",
		},
		{
			name:         "proxy v6",
			remoteAddr:   "[::1]:50920",
			forwardedFor: []string{"2001:db8::1"},
			want:         "2001:db8::1",
		},
		// Only the last X-Forwarded-For element is trusted: it is the one our
		// reverse proxy appended (the address it actually saw). Everything
		// before it was supplied by the client and can be forged. (Caddy
		// replaces the header for untrusted clients today, but would append
		// to it if trusted_proxies were ever configured.)
		{
			name:         "proxy chain",
			remoteAddr:   "127.0.0.1:50920",
			forwardedFor: []string{"10.0.0.1, 198.51.100.1"},
			want:         "198.51.100.1",
		},
		{
			name:         "proxy chain with spoofed client-supplied entry",
			remoteAddr:   "127.0.0.1:50920",
			forwardedFor: []string{"8.8.8.8, 127.0.0.1, 198.51.100.1"},
			want:         "198.51.100.1",
		},
		{
			name:         "proxy multiple headers",
			remoteAddr:   "127.0.0.1:50920",
			forwardedFor: []string{"10.0.0.1", "198.51.100.1"},
			want:         "198.51.100.1",
		},
		{
			name:         "proxy multiple headers with spoofed client-supplied header",
			remoteAddr:   "127.0.0.1:50920",
			forwardedFor: []string{"8.8.8.8", "198.51.100.1"},
			want:         "198.51.100.1",
		},
		{
			name:       "proxy without header",
			remoteAddr: "127.0.0.1:50920",
			want:       "127.0.0.1",
		},
		{
			name:         "proxy empty header",
			remoteAddr:   "127.0.0.1:50920",
			forwardedFor: []string{" "},
			want:         "127.0.0.1",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := clientIP(tt.remoteAddr, tt.forwardedFor); got != tt.want {
				t.Errorf("clientIP(%q, %q) = %q, want %q", tt.remoteAddr, tt.forwardedFor, got, tt.want)
			}
		})
	}
}
