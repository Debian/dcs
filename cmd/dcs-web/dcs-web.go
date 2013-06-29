// vim:ts=4:sw=4:noexpandtab
package main

import (
	"github.com/Debian/dcs/cmd/dcs-web/common"
	"github.com/Debian/dcs/cmd/dcs-web/health"
	"github.com/Debian/dcs/cmd/dcs-web/index"
	"github.com/Debian/dcs/cmd/dcs-web/search"
	"github.com/Debian/dcs/cmd/dcs-web/show"
	"github.com/Debian/dcs/cmd/dcs-web/varz"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"runtime/pprof"
)

var listenAddress = flag.String("listen_address",
	":28080",
	"listen address ([host]:port)")
var memprofile = flag.String("memprofile", "", "Write memory profile to this file")

func main() {
	flag.Parse()
	common.LoadTemplates()

	fmt.Println("Debian Code Search webapp")

	search.OpenTimingFiles()

	health.StartChecking()

	http.HandleFunc("/", index.Index)
	http.HandleFunc("/favicon.ico", http.NotFound)
	http.HandleFunc("/varz", varz.Varz)
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
