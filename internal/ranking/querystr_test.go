package ranking

import (
	"testing"

	"github.com/Debian/dcs/internal/regexp"
)

var benchMatches = []regexp.Match{
	{
		Path:    "golang-1.27_1.27.3-1/src/net/http/httptest/server.go",
		Context: "func (s *Server) closeConnChan(c net.Conn, done chan<- struct{}) {",
	},
	{
		Path:    "golang-sorcix-irc-dev_1.1.4-1/debian/changelog",
		Context: " -- Michael Stapelberg <stapelberg@debian.org>  Fri, 09 Feb 2018 17:09:26 +0100",
	},
	{
		Path:    "gokr-rsync_0.3.8-1/rsyncclient/rsyncclient.go",
		Context: "	done, err := maincmd.StartInbandExchange(c.osenv, c.opts, conn, remotePath)",
	},
	{
		Path:    "i3-wm_4.25.1-1/libi3/font.c",
		Context: "            return predict_text_width_xcb(i3string_as_ucs2(text), i3string_get_num_glyphs(text));",
	},
	{
		Path:    "systemd_262.1-1/units/systemd-update-done.service.in",
		Context: "Documentation=man:systemd-update-done.service(8)",
	},
}

func BenchmarkMatch(b *testing.B) {
	querystr := NewQueryStr("done")
	for b.Loop() {
		for i := range benchMatches {
			querystr.Match(&benchMatches[i].Path)
		}
	}
}

func TestMatch(t *testing.T) {
	for _, tt := range []struct {
		querystr     string
		path         string
		wantBoundary int
		wantAnywhere int
	}{
		{
			querystr:     "hash",
			path:         "gothashtray_entirely_black",
			wantBoundary: -1,
			wantAnywhere: 3,
		},

		{
			querystr:     "(?i)hash",
			path:         "#define HASH(x) sha256(x)",
			wantBoundary: 8,
			wantAnywhere: 8,
		},

		{
			querystr:     "hash",
			path:         "hash_table.c",
			wantBoundary: -1, // perhaps surprisingly, _ is a word.
			wantAnywhere: 0,
		},
	} {
		querystr := NewQueryStr(tt.querystr)
		got := querystr.indexBoundary(tt.path)
		if got != tt.wantBoundary {
			t.Errorf("QueryStr(%s).indexBoundary(%s) = %v, want %v", tt.querystr, tt.path, got, tt.wantBoundary)
		}
		got = querystr.indexAnywhere(tt.path)
		if got != tt.wantAnywhere {
			t.Errorf("QueryStr(%s).indexAnywhere(%s) = %v, want %v", tt.querystr, tt.path, got, tt.wantAnywhere)
		}
	}
}
