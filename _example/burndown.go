package main

import (
	"context"
	"encoding/csv"
	"log"
	"os"
	"strconv"

	openapiclient "github.com/Debian/dcs/internal/openapiclient"
	"github.com/antihax/optional"
)

const apiKey = "TODO: copy&paste API key from https://codesearch.debian.net/apikeys/"

func burndown() error {
	cfg := openapiclient.NewConfiguration()
	cfg.AddDefaultHeader("x-dcs-apikey", apiKey)
	client := openapiclient.NewAPIClient(cfg)
	ctx := context.Background()

	// Search through the full Debian Code Search corpus, blocking until all
	// results are available:
	results, _, err := client.SearchApi.Search(ctx, "fmt.Sprint(err)", &openapiclient.SearchApiSearchOpts{
		// Literal searches are faster and do not require escaping special
		// characters, regular expression searches are more powerful.
		MatchMode: optional.NewString("literal"),
	})
	if err != nil {
		return err
	}

	// Print to stdout a CSV file with the path and number of occurrences:
	wr := csv.NewWriter(os.Stdout)
	if err := wr.Write([]string{"path", "number of occurrences"}); err != nil {
		return err
	}
	occurrences := make(map[string]int)
	for _, result := range results {
		occurrences[result.Path]++
	}
	for _, result := range results {
		o, ok := occurrences[result.Path]
		if !ok {
			continue
		}
		// Print one CSV record per path:
		delete(occurrences, result.Path)
		if err := wr.Write([]string{result.Path, strconv.Itoa(o)}); err != nil {
			return err
		}
	}
	wr.Flush()
	return wr.Error()
}

func main() {
	if err := burndown(); err != nil {
		log.Fatal(err)
	}
}
