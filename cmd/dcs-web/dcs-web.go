// vim:ts=4:sw=4:noexpandtab
package main

import (
	"dcs/cmd/dcs-web/index"
	"dcs/cmd/dcs-web/search"
	"dcs/cmd/dcs-web/show"
	"flag"
	"fmt"
	"log"
	"net/http"
)

func main() {
	flag.Parse()
	fmt.Println("Debian Code Search webapp")

	http.HandleFunc("/", index.Index)
	http.HandleFunc("/search", search.Search)
	http.HandleFunc("/show", show.Show)
	log.Fatal(http.ListenAndServe(":28080", nil))
}
