package main

import (
	"crypto/tls"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
)

const queryHelp = `query - performs a search query

The query subcommand queries Debian Code Search via HTTPS.

Example:
  % dcs query i3Font
`

func query(args []string) error {
	fset := flag.NewFlagSet("query", flag.ExitOnError)
	fset.Usage = usage(fset, queryHelp)
	var target string
	fset.StringVar(&target, "target", "https://codesearch.debian.net", "Debian Code Search instance to query")
	var insecure bool
	fset.BoolVar(&insecure, "insecure", false, "skip TLS certificate verification")
	var apikey string
	fset.StringVar(&apikey, "apikey",
		// API key for codesearch.debian.net for subject
		// “program!github.com/Debian/dcs/cmd/dcs”:
		"MTU5OTI5NzQzN3xDU1d4UE0yZllVUk9TVXRfdmxicEhIdUxiU3YzTkxGRjZNRl90WUc4bUg1OVdqNU9CM3RQaXFsa0xaRGdZRlZPSWNCZG1QNGZ3ZUNEcXp2SGdocVlEc1dkQmxRSUh0dmZoM0xKazRrPXx5ED9o0r-7uawKvV_K0Fb4QdbHsTV1qfY0XYFrl_904g==",
		"Debian Code Search API key to use, see https://codesearch.debian.net/apikeys/ for more details. Please get an API key if you are doing automated queries.")
	if err := fset.Parse(args); err != nil {
		return err
	}
	if fset.NArg() < 1 {
		return fmt.Errorf("Usage: query <search term(s)>")
	}

	queryParams := url.Values{}
	queryParams.Set("query", strings.Join(fset.Args(), " "))
	u := strings.TrimSuffix(target, "/") + "/api/v1/search?" + queryParams.Encode()
	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("x-dcs-apikey", apikey)
	tr := http.DefaultTransport.(*http.Transport).Clone()
	if insecure {
		if tr.TLSClientConfig == nil {
			tr.TLSClientConfig = &tls.Config{}
		}
		tr.TLSClientConfig.InsecureSkipVerify = true
	}
	client := &http.Client{
		Transport: tr,
	}
	log.Printf("Searching %s", u)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("unexpected HTTP status: %v (body: %s)", resp.Status, strings.TrimSpace(string(b)))
	}
	var results []struct {
		Path    string `json:"path"`
		Line    int    `json:"line"`
		Context string `json:"context"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&results); err != nil {
		return err
	}
	for _, r := range results {
		fmt.Printf("%s:%d: %s\n", r.Path, r.Line, r.Context)
	}
	return nil
}
