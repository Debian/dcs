// vim:ts=4:sw=4:noexpandtab

// common flags for dcs-web
package common

import (
	"flag"
	"html/template"
	"log"
	"reflect"
	"strings"

	"github.com/Debian/dcs/grpcutil"
	"github.com/Debian/dcs/proto"
)

var Version string = "unknown"
var templatePattern = flag.String("template_pattern",
	"templates/*",
	"Pattern matching the HTML templates (./templates/* by default)")
var sourceBackends = flag.String("source_backends",
	"localhost:28082",
	"host:port (multiple values are comma-separated) of the source-backend(s)")
var tlsCertPath = flag.String("tls_cert_path", "", "Path to a .pem file containing the TLS certificate.")
var tlsKeyPath = flag.String("tls_key_path", "", "Path to a .pem file containing the TLS private key.")
var SourceBackendStubs []proto.SourceBackendClient
var UseSourcesDebianNet = flag.Bool("use_sources_debian_net",
	false,
	"Redirect to sources.debian.net instead of handling /show on our own.")
var Templates *template.Template

// Must be called after flag.Parse()
func Init() {
	loadTemplates()
	addrs := strings.Split(*sourceBackends, ",")
	SourceBackendStubs = make([]proto.SourceBackendClient, len(addrs))
	for idx, addr := range addrs {
		conn, err := grpcutil.DialTLS(addr, *tlsCertPath, *tlsKeyPath)
		if err != nil {
			log.Fatalf("could not connect to %q: %v", addr, err)
		}
		SourceBackendStubs[idx] = proto.NewSourceBackendClient(conn)
	}
}

func loadTemplates() {
	var err error
	Templates = template.New("foo").Funcs(template.FuncMap{
		"eq": func(args ...interface{}) bool {
			if len(args) == 0 {
				return false
			}
			x := args[0]
			switch x := x.(type) {
			case string, int, int64, byte, float32, float64:
				for _, y := range args[1:] {
					if x == y {
						return true
					}
				}
				return false
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
