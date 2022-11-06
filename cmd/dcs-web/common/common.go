// vim:ts=4:sw=4:noexpandtab

// common flags for dcs-web
package common

import (
	"flag"
	"html/template"
	"io/ioutil"
	"log"
	"path/filepath"
	"reflect"
	"runtime/debug"
	"strings"

	"google.golang.org/grpc"

	"github.com/Debian/dcs/grpcutil"
	"github.com/Debian/dcs/internal/proto/sourcebackendpb"
)

type StructuredVersion struct {
	Revision string
	Modified bool
}

func (sv StructuredVersion) String() string {
	if len(sv.Revision) > 7 {
		sv.Revision = sv.Revision[:7]
	}
	modifiedSuffix := ""
	if sv.Modified {
		modifiedSuffix = " (modified)"
	}
	return sv.Revision + modifiedSuffix
}

func Version() StructuredVersion {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return StructuredVersion{Revision: "<ReadBuildInfo() failed>"}
	}
	settings := make(map[string]string)
	for _, s := range info.Settings {
		settings[s.Key] = s.Value
	}
	return StructuredVersion{
		Revision: settings["vcs.revision"],
		Modified: settings["vcs.modified"] == "true",
	}
}

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
	b, err := ioutil.ReadFile(filepath.Join(staticPath, "critical.min.css"))
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
