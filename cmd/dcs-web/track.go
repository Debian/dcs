package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"
)

var (
	clickLogPath = flag.String("click_log_path",
		"",
		"Where to write the click.log entries (JSON-encoded, timestamped). Disabled if empty.")

	clickLog *os.File
)

func Track(w http.ResponseWriter, r *http.Request) {
	var t struct {
		Searchterm string `json:"searchterm"`
		Path       string `json:"path"`
		Line       string `json:"line"`
	}
	// Limit requests to 4K to prevent flooding our logs too easily.
	rd := &io.LimitedReader{R: r.Body, N: 4096}
	if err := json.NewDecoder(rd).Decode(&t); err != nil {
		log.Printf("Could not decode /track request: %v\n", err)
		return
	}
	if t.Path == "" || t.Line == "" {
		log.Printf("Ignoring /track request without path/line: %+v\n", t)
		return
	}
	if clickLog == nil {
		return
	}

	b, err := json.Marshal(&t)
	if err != nil {
		log.Printf("Could not encode /track request: %v\n", err)
		return
	}

	fmt.Fprintf(clickLog, "%s - %s\n",
		time.Now().Format("02/Jan/2006:15:04:05 -0700"),
		string(b))
}
