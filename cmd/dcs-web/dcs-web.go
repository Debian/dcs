// vim:ts=4:sw=4:noexpandtab
package main

import (
	"flag"
	"fmt"
	"github.com/Debian/dcs/cmd/dcs-web/common"
	"github.com/Debian/dcs/cmd/dcs-web/goroutinez"
	"github.com/Debian/dcs/cmd/dcs-web/health"
	"github.com/Debian/dcs/cmd/dcs-web/index"
	"github.com/Debian/dcs/cmd/dcs-web/search"
	"github.com/Debian/dcs/cmd/dcs-web/show"
	"github.com/Debian/dcs/cmd/dcs-web/varz"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime/pprof"
)

var (
	listenAddress = flag.String("listen_address",
		":28080",
		"listen address ([host]:port)")
	memprofile = flag.String("memprofile", "", "Write memory profile to this file")
	staticPath = flag.String("static_path",
		"./static/",
		"Path to static assets such as *.css")
)

func main() {
	flag.Parse()
	common.LoadTemplates()

	fmt.Println("Debian Code Search webapp")

	search.OpenTimingFiles()

	health.StartChecking()

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Check if a static file was requested with full name
		name := filepath.Join(*staticPath, r.URL.Path)
		if _, err := os.Stat(name); err == nil {
			http.ServeFile(w, r, name)
			return
		}

		// Or maybe /faq, which resolves to /faq.html
		name = name + ".html"
		if _, err := os.Stat(name); err == nil {
			http.ServeFile(w, r, name)
			return
		}

		index.Index(w, r)
	})
	http.HandleFunc("/favicon.ico", http.NotFound)
	http.HandleFunc("/varz", varz.Varz)
	http.HandleFunc("/goroutinez", goroutinez.Goroutinez)
	http.HandleFunc("/search", search.Search)
	http.HandleFunc("/show", show.Show)
	http.HandleFunc("/memprof", func(w http.ResponseWriter, r *http.Request) {
		fmt.Println("writing memprof")
		if *memprofile != "" {
			f, err := os.Create(*memprofile)
			if err != nil {
				log.Fatal(err)
			}
			pprof.WriteHeapProfile(f)
			f.Close()
			return
		}
	})
	log.Fatal(http.ListenAndServe(*listenAddress, nil))
}
