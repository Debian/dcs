// vim:ts=4:sw=4:noexpandtab
package main

import (
	"dcs/cmd/dcs-web/common"
	"dcs/cmd/dcs-web/health"
	"dcs/cmd/dcs-web/index"
	"dcs/cmd/dcs-web/search"
	"dcs/cmd/dcs-web/show"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"runtime/pprof"
)

var listenHost = flag.String("listen_host",
	":28080",
	"host:port to listen on")
var memprofile = flag.String("memprofile", "", "Write memory profile to this file")

func main() {
	flag.Parse()
	common.LoadTemplates()

	fmt.Println("Debian Code Search webapp")

	search.OpenTimingFiles()

	health.StartChecking()

	http.HandleFunc("/", index.Index)
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
	log.Fatal(http.ListenAndServe(*listenHost, nil))
}
