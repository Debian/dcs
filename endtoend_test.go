package endtoend_test

import (
	"context"
	"flag"
	"io"
	"io/ioutil"
	"path/filepath"
	"testing"

	"google.golang.org/grpc"

	"github.com/Debian/dcs/grpcutil"
	"github.com/Debian/dcs/internal/localdcs"
	"github.com/Debian/dcs/internal/proto/dcspb"
	"github.com/gogo/protobuf/proto"
	"github.com/google/go-cmp/cmp"
)

func TestEndToEnd(t *testing.T) {
	temp, err := ioutil.TempDir("", "dcs-endtoend")
	if err != nil {
		t.Fatal(err)
	}
	// TODO: refactor localdcs.Start to take options
	flag.Set("localdcs_path", temp)
	dcsAddress, err := localdcs.Start()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		flag.Set("stop", "true")
		localdcs.Start()
	}()

	conn, err := grpcutil.DialTLS(dcsAddress,
		filepath.Join(temp, "cert.pem"),
		filepath.Join(temp, "key.pem"),
		grpc.WithBlock())
	if err != nil {
		t.Fatalf("could not connect to %q: %v", dcsAddress, err)
	}
	defer conn.Close()
	dcs := dcspb.NewDCSClient(conn)
	stream, err := dcs.Search(context.Background(), &dcspb.SearchRequest{
		Query: "i3Font",
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
}
