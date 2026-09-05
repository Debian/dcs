// vim:ts=4:sw=4:noexpandtab

// common flags for dcs-web
package common

import (
	"flag"
	"html/template"
	"os"
	"slices"

	"log"
	"net/url"
	"path/filepath"
	"reflect"
	"strings"

	"google.golang.org/grpc"

	"github.com/Debian/dcs/internal/grpcutil"
	"github.com/Debian/dcs/internal/proto/sourcebackendpb"
)

var CriticalCss template.CSS
var templatePattern = flag.String("template_pattern",
	"templates/*",
	"Pattern matching the HTML templates (./templates/* by default)")
var sourceBackends = flag.String("source_backends",
	"localhost:28082",
	"host:port (multiple values are comma-separated) of the source-backend(s)")
var SourceBackendStubs []sourcebackendpb.SourceBackendClient
var UseSourcesDebianNet = flag.Bool("use_sources_debian_net",
	false,
	"Redirect to sources.debian.net instead of handling /show on our own.")
var Templates *template.Template

// Must be called after flag.Parse()
func Init(tlsCertPath, tlsKeyPath, staticPath string) {
	loadTemplates()
	b, err := os.ReadFile(filepath.Join(staticPath, "critical.min.css"))
	if err != nil {
		log.Fatal(err)
	}
	CriticalCss = template.CSS(string(b))
	addrs := strings.Split(*sourceBackends, ",")
	SourceBackendStubs = make([]sourcebackendpb.SourceBackendClient, len(addrs))
	for idx, addr := range addrs {
		conn, err := grpcutil.DialTLS(addr, tlsCertPath, tlsKeyPath, grpc.WithBlock())
		if err != nil {
			log.Fatalf("could not connect to %q: %v", addr, err)
		}
		SourceBackendStubs[idx] = sourcebackendpb.NewSourceBackendClient(conn)
	}
}

func loadTemplates() {
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
	Templates, err = Templates.ParseGlob(*templatePattern)
	if err != nil {
		log.Fatalf(`Could not load templates from "%s": %v`, *templatePattern, err)
	}
}
