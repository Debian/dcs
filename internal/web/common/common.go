// vim:ts=4:sw=4:noexpandtab

// common flags for dcs-web
package common

import (
	"html/template"
	"io/fs"
	"slices"

	"log"
	"net/url"
	"reflect"
	"strings"

	"github.com/Debian/dcs/internal/grpcutil"
	"github.com/Debian/dcs/internal/proto/sourcebackendpb"
)

var CriticalCss template.CSS
var SourceBackendStubs []sourcebackendpb.SourceBackendClient
var Templates *template.Template

func Init(tlsCertPath, tlsKeyPath, sourceBackends string, templates fs.FS) {
	loadTemplates(templates)
	addrs := strings.Split(sourceBackends, ",")
	SourceBackendStubs = make([]sourcebackendpb.SourceBackendClient, len(addrs))
	for idx, addr := range addrs {
		conn, err := grpcutil.DialTLS(addr, tlsCertPath, tlsKeyPath)
		if err != nil {
			log.Fatalf("could not connect to %q: %v", addr, err)
		}
		SourceBackendStubs[idx] = sourcebackendpb.NewSourceBackendClient(conn)
	}
}

func loadTemplates(templates fs.FS) {
	var err error
	Templates = template.New("foo").Funcs(template.FuncMap{
		"appendToQuery": func(unparsedURL, extra string) string {
			u, err := url.Parse(unparsedURL)
			if err != nil {
				log.Printf("appendToQuery(%q) = %v", unparsedURL, err)
				return unparsedURL
			}
			basequery := u.Query()
			basequery.Set("q", basequery.Get("q")+extra)
			u.RawQuery = basequery.Encode()
			return u.String()
		},
		"eq": func(args ...any) bool {
			if len(args) == 0 {
				return false
			}
			x := args[0]
			switch x := x.(type) {
			case string, int, int64, byte, float32, float64:
				return slices.Contains(args[1:], x)
			}

			for _, y := range args[1:] {
				if reflect.DeepEqual(x, y) {
					return true
				}
			}
			return false
		},
	})
	Templates, err = Templates.ParseFS(templates, "templates/*.html")
	if err != nil {
		log.Fatalf("Could not load embedded templates: %v", err)
	}
}
