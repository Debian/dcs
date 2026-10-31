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
