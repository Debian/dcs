package endtoend_test

import (
	"bufio"
	"encoding/json"
	"flag"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Debian/dcs/internal/api"
	"github.com/Debian/dcs/internal/localdcs"
)

func TestEndToEnd(t *testing.T) {
	temp := t.TempDir()
	// TODO: refactor localdcs.Start to take options
	flag.Set("localdcs_path", temp)
	flag.Set("shard_path", filepath.Join(temp, "shard"))
	flag.Set("shard_path", filepath.Join(temp, "shard"))
	hashKeyPath := filepath.Join(temp, "hash.key")
	if err := os.WriteFile(hashKeyPath, []byte("3270b4d09abccbf3fe59b957b1d429c8c58ac5def079ea4b245f66ade65168c2"), 0600); err != nil {
		t.Fatal(err)
	}
	blockKeyPath := filepath.Join(temp, "block.key")
	if err := os.WriteFile(blockKeyPath, []byte("cdba47f8f82be74175a75ec864aca56d8dcdc5610c88af446005766c6f9e6fd5"), 0600); err != nil {
		t.Fatal(err)
	}
	instance, err := localdcs.Start(hashKeyPath, blockKeyPath, "testdata")
	if err != nil {
		t.Fatal(err)
	}

	// Created using dcs apikey-create; subject is set to “unittest!”
	const apikey = "MTYxNDAxMzI4OXwyb2pFeXdGd0Q0VmdhTkZtMkRoeDdsa1JUa3ZwOTQtM3M2MG1ybGFWRkhacUZwZ1dmMmFlMG5lbkM3UWQ1SV96LXc9PXxdMAS04xLgPDL02_RXt7IftcfPZ4x839RuMhy0_WSX0g=="

	t.Run("EventStream", func(t *testing.T) {
		urlPrefix := "https://" + instance.Addr
		req, err := http.NewRequest("GET", urlPrefix+"/events/i3Font", nil)
		if err != nil {
			t.Fatal(err)
		}
		// No API key needed, this is the API which instant.js (browser) uses.
		resp, err := instance.HTTPClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if got, want := resp.StatusCode, http.StatusOK; got != want {
			b, _ := io.ReadAll(resp.Body)
			t.Fatalf("unexpected HTTP status code: got %v (%s), want %v",
				resp.Status,
				strings.TrimSpace(string(b)),
				want)
		}

		type progress struct {
			Type           string `json:"Type"`
			QueryId        string `json:"QueryId"`
			FilesProcessed int    `json:"FilesProcessed"`
			FilesTotal     int    `json:"FilesTotal"`
		}
		// Verify progress messages are sent.
		scanner := bufio.NewScanner(resp.Body)
		var events []progress
		for scanner.Scan() {
			data, ok := strings.CutPrefix(scanner.Text(), "data: ")
			if !ok {
				continue
			}
			var ev progress
			if err := json.Unmarshal([]byte(data), &ev); err != nil {
				t.Fatal(err)
			}
			if ev.Type != "progress" {
				continue
			}
			events = append(events, ev)
		}
		if err := scanner.Err(); err != nil {
			t.Fatal(err)
		}
		t.Logf("printing %d events:", len(events))
		for _, ev := range events {
			t.Logf("event: %+v", ev)
		}
		if len(events) == 0 {
			t.Fatalf("no progress events received")
		}
		last := events[len(events)-1]
		if last.FilesProcessed == 0 {
			t.Errorf("last.FilesProcessed unexpectedly 0")
		}
		if last.FilesTotal == 0 {
			t.Errorf("last.FilesTotal unexpectedly 0")
		}
		if last.FilesProcessed != last.FilesTotal {
			t.Errorf("last progress event: FilesProcessed=%d != FilesTotal=%d",
				last.FilesProcessed, last.FilesTotal)
		}
	})

	t.Run("OpenAPI", func(t *testing.T) {
		urlPrefix := "https://" + instance.Addr + "/api"

		t.Run("OPTIONS", func(t *testing.T) {
			req, err := http.NewRequest("OPTIONS", urlPrefix+"/v1/search", nil)
			if err != nil {
				t.Fatal(err)
			}
			resp, err := instance.HTTPClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			if got, want := resp.StatusCode, http.StatusNoContent; got != want {
				b, _ := io.ReadAll(resp.Body)
				t.Fatalf("unexpected HTTP status code: got %v (%s), want %v",
					resp.Status,
					strings.TrimSpace(string(b)),
					want)
			}
			// TODO: verify CORS headers are present
		})

		t.Run("WithoutKey", func(t *testing.T) {
			req, err := http.NewRequest("GET", urlPrefix+"/v1/search", nil)
			if err != nil {
				t.Fatal(err)
			}
			resp, err := instance.HTTPClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			if got, want := resp.StatusCode, http.StatusForbidden; got != want {
				b, _ := io.ReadAll(resp.Body)
				t.Fatalf("unexpected HTTP status code: got %v (%s), want %v",
					resp.Status,
					strings.TrimSpace(string(b)),
					want)
			}
		})

		t.Run("GET", func(t *testing.T) {
			req, err := http.NewRequest("GET", urlPrefix+"/v1/search?query=i3Font", nil)
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("x-dcs-apikey", apikey)
			resp, err := instance.HTTPClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			if got, want := resp.StatusCode, http.StatusOK; got != want {
				b, _ := io.ReadAll(resp.Body)
				t.Fatalf("unexpected HTTP status code: got %v (%s), want %v",
					resp.Status,
					strings.TrimSpace(string(b)),
					want)
			}

			var results []api.SearchResult
			b, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(b, &results); err != nil {
				t.Fatal(err)
			}
			want := api.SearchResult{
				Package: "i3-wm_4.5.1-2",
				Path:    "i3-wm_4.5.1-2/libi3/font.c",
				Line:    0x8b,
				Context: "i3Font load_font(const char *pattern, const bool fallback) {",
				ContextBefore: []string{
					" *",
					" */",
				},
				ContextAfter: []string{
					"    i3Font font;",
					"    font.type = FONT_TYPE_NONE;",
				},
			}
			for _, got := range results {
				if reflect.DeepEqual(got, want) {
					return // test passed
				}
			}
			t.Fatalf("search result %+v not found in results %+v", want, results)
		})

		t.Run("PerPackage", func(t *testing.T) {
			req, err := http.NewRequest("GET", urlPrefix+"/v1/searchperpackage?query=i3Font", nil)
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("x-dcs-apikey", apikey)
			resp, err := instance.HTTPClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			if got, want := resp.StatusCode, http.StatusOK; got != want {
				b, _ := io.ReadAll(resp.Body)
				t.Fatalf("unexpected HTTP status code: got %v (%s), want %v",
					resp.Status,
					strings.TrimSpace(string(b)),
					want)
			}

			var results []api.PerPackageResult
			b, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(b, &results); err != nil {
				t.Fatal(err)
			}
			if got, want := len(results), 1; got != want {
				t.Fatalf("len(results) = %d, want %d", got, want)
			}
			for _, got := range results[0].Results {
				if got.Path == "i3-wm_4.5.1-2/libi3/font.c" &&
					strings.Contains(got.Context, "i3Font") {
					return // test passed
				}
			}
			t.Fatalf("search result i3-wm_4.5.1-2/libi3/font.c not found in results %+v", results)
		})

	})
}
