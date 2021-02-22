package endtoend_test

import (
	"context"
	"encoding/json"
	"flag"
	"io"
	"io/ioutil"
	"net/http"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/Debian/dcs/grpcutil"
	"github.com/Debian/dcs/internal/api"
	"github.com/Debian/dcs/internal/localdcs"
	"github.com/Debian/dcs/internal/proto/dcspb"
	"github.com/golang/protobuf/proto"
	"github.com/google/go-cmp/cmp"
)

func TestEndToEnd(t *testing.T) {
	temp, err := ioutil.TempDir("", "dcs-endtoend")
	if err != nil {
		t.Fatal(err)
	}
	// TODO: refactor localdcs.Start to take options
	flag.Set("localdcs_path", temp)
	flag.Set("shard_path", filepath.Join(temp, "shard"))
	flag.Set("shard_path", filepath.Join(temp, "shard"))

	instance, err := localdcs.Start(
		"-securecookie_hash_key=3270b4d09abccbf3fe59b957b1d429c8c58ac5def079ea4b245f66ade65168c2",
		"-securecookie_block_key=cdba47f8f82be74175a75ec864aca56d8dcdc5610c88af446005766c6f9e6fd5",
	)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		flag.Set("stop", "true")
		localdcs.Start()
	}()

	// Created using dcs apikey-create; subject is set to “unittest!”
	const apikey = "MTYxNDAxMzI4OXwyb2pFeXdGd0Q0VmdhTkZtMkRoeDdsa1JUa3ZwOTQtM3M2MG1ybGFWRkhacUZwZ1dmMmFlMG5lbkM3UWQ1SV96LXc9PXxdMAS04xLgPDL02_RXt7IftcfPZ4x839RuMhy0_WSX0g=="

	t.Run("GRPC", func(t *testing.T) {

		conn, err := grpcutil.DialTLS(instance.Addr,
			filepath.Join(temp, "cert.pem"),
			filepath.Join(temp, "key.pem"),
			grpc.WithBlock())
		if err != nil {
			t.Fatalf("could not connect to %q: %v", instance.Addr, err)
		}
		defer conn.Close()
		dcs := dcspb.NewDCSClient(conn)

		{
			// Verify API key are required:
			stream, err := dcs.Search(context.Background(), &dcspb.SearchRequest{
				Query: "i3Font",
			})
			if err != nil && status.Code(err) != codes.Unauthenticated {
				t.Fatalf("Search(without API key) = %v, want Unauthenticated", err)
			}
			if _, err := stream.Recv(); err == nil || status.Code(err) != codes.Unauthenticated {
				t.Fatalf("Search(without API key) = %v, want Unauthenticated", err)
			}
		}

		stream, err := dcs.Search(context.Background(), &dcspb.SearchRequest{
			Query:  "i3Font",
			Apikey: apikey,
		})
		if err != nil {
			t.Fatal(err)
		}
		var events []*dcspb.Event
		for {
			event, err := stream.Recv()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatal(err)
			}
			if prog, ok := event.Data.(*dcspb.Event_Progress); !ok {
				continue // TODO: compare the rest, too
			} else {
				if prog.Progress.FilesProcessed > 0 &&
					prog.Progress.FilesProcessed < prog.Progress.FilesTotal {
					continue // TODO: compare intermediate progress updates, too
				}
			}
			events = append(events, event)
		}
		t.Logf("%d events", len(events))
		for idx, ev := range events {
			t.Logf("event %d: %+v", idx, ev)
		}
		var queryId string
		last := events[len(events)-1]
		if p, ok := last.Data.(*dcspb.Event_Progress); ok {
			queryId = p.Progress.QueryId
		}

		want := []*dcspb.Event{
			&dcspb.Event{
				Data: &dcspb.Event_Progress{
					Progress: &dcspb.Progress{
						QueryId:    queryId,
						FilesTotal: 8,
					},
				},
			},

			&dcspb.Event{
				Data: &dcspb.Event_Progress{
					Progress: &dcspb.Progress{
						QueryId:        queryId,
						FilesProcessed: 8,
						FilesTotal:     8,
						Results:        17,
					},
				},
			},
		}

		if diff := cmp.Diff(want, events, cmp.Comparer(proto.Equal)); diff != "" {
			t.Fatalf("Search: events differ (-want +got)\n%s", diff)
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
			if got, want := resp.StatusCode, http.StatusNoContent; got != want {
				b, _ := ioutil.ReadAll(resp.Body)
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
			if got, want := resp.StatusCode, http.StatusForbidden; got != want {
				b, _ := ioutil.ReadAll(resp.Body)
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
			if got, want := resp.StatusCode, http.StatusOK; got != want {
				b, _ := ioutil.ReadAll(resp.Body)
				t.Fatalf("unexpected HTTP status code: got %v (%s), want %v",
					resp.Status,
					strings.TrimSpace(string(b)),
					want)
			}

			var results []api.SearchResult
			b, err := ioutil.ReadAll(resp.Body)
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
			if got, want := resp.StatusCode, http.StatusOK; got != want {
				b, _ := ioutil.ReadAll(resp.Body)
				t.Fatalf("unexpected HTTP status code: got %v (%s), want %v",
					resp.Status,
					strings.TrimSpace(string(b)),
					want)
			}

			var results []api.PerPackageResult
			b, err := ioutil.ReadAll(resp.Body)
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(b, &results); err != nil {
				t.Fatal(err)
			}
			if got, want := len(results), 1; got != want {
				t.Fatalf("len(results) = %d, want %d", got, want)
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
			for _, got := range results[0].Results {
				if reflect.DeepEqual(got, want) {
					return // test passed
				}
			}
			t.Fatalf("search result %+v not found in results %+v", want, results)
		})

	})
}
