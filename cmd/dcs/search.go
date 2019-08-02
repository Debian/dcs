package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/Debian/dcs/internal/index"
	"github.com/Debian/dcs/internal/proto/sourcebackendpb"
	"github.com/Debian/dcs/internal/rpctest"
	"github.com/Debian/dcs/internal/sourcebackend"
	"google.golang.org/grpc"
)

const searchHelp = `search - list the filename[:pos] matches for the specified search query

Example:
  % dcs search -idx=/srv/dcs/shard4/full -unpacked_path=/srv/dcs/shard4/src -query=i3Font
  /srv/dcs/shard4/src/i3-wm_4.16.1-1/i3-config-wizard/main.c:95
  /srv/dcs/shard4/src/i3-wm_4.16.1-1/i3-config-wizard/main.c:96
  /srv/dcs/shard4/src/i3-wm_4.16.1-1/i3-nagbar/main.c:64
  /srv/dcs/shard4/src/i3-wm_4.16.1-1/i3-nagbar/main.c:471
  /srv/dcs/shard4/src/i3-wm_4.16.1-1/i3bar/src/xcb.c:68
  […]
`

func search(args []string) error {
	fset := flag.NewFlagSet("search", flag.ExitOnError)
	fset.Usage = usage(fset, searchHelp)
	var idx string
	fset.StringVar(&idx, "idx", "", "path to the index file to work with")
	var unpacked string
	fset.StringVar(&unpacked, "unpacked_path", "", "path to the source files to work with")
	var query string
	fset.StringVar(&query, "query", "", "search query")
	var pos bool
	fset.BoolVar(&pos, "pos", false, "do a positional query for identifier searches")
	if err := fset.Parse(args); err != nil {
		return err
	}
	if idx == "" || query == "" {
		fset.Usage()
		os.Exit(1)
	}

	ix, err := index.Open(idx)
	if err != nil {
		return fmt.Errorf("Could not open index: %v", err)
	}

	srv := &sourcebackend.Server{
		Index:              ix,
		UnpackedPath:       unpacked,
		IndexPath:          idx,
		UsePositionalIndex: pos,
	}

	conn, cleanup := rpctest.Loopback(func(s *grpc.Server) {
		sourcebackendpb.RegisterSourceBackendServer(s, srv)
	})
	defer cleanup()
	cl := sourcebackendpb.NewSourceBackendClient(conn)
	stream, err := cl.Search(context.Background(), &sourcebackendpb.SearchRequest{
		Query:        query,
		RewrittenUrl: "",
	})
	if err != nil {
		return err
	}
	for {
		msg, err := stream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("decoding result stream: %v", err)
		}
		if msg.Type != sourcebackendpb.SearchReply_MATCH {
			continue
		}
		fmt.Printf("%s:%d\n", unpacked+msg.Match.Path, msg.Match.Line)
	}
	return nil
}
