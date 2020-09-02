package main

import (
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"strings"

	"github.com/Debian/dcs/internal/proto/dcspb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/grpclog"
)

const queryHelp = `query - performs a search query

The query subcommand connects to Debian Code Search via gRPC and performs a
search query.

The subcommand currently prints raw API messages received,
which is good for debugging and bad for human consumption.
Friendlier frontends welcome :)

Example:
  % dcs query i3Font
`

func query(args []string) error {
	fset := flag.NewFlagSet("query", flag.ExitOnError)
	fset.Usage = usage(fset, queryHelp)
	var target string
	fset.StringVar(&target, "target", "", "gRPC target address")
	var insecure bool
	fset.BoolVar(&insecure, "insecure", false, "skip TLS certificate verification")
	var grpcEnableLog bool
	fset.BoolVar(&grpcEnableLog, "grpclog", false, "enable gRPC verbose logging for debugging")
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
	log.Printf("dialing %s", target)

	if grpcEnableLog {
		grpclog.SetLoggerV2(grpclog.NewLoggerV2WithVerbosity(os.Stderr, os.Stderr, os.Stderr, 2))
	}

	creds := credentials.NewTLS(&tls.Config{
		InsecureSkipVerify: insecure,
	})
	conn, err := grpc.Dial(target,
		grpc.WithTransportCredentials(creds),
		grpc.WithBlock())
	if err != nil {
		return err
	}
	log.Printf("sending Search query")
	dcs := dcspb.NewDCSClient(conn)
	stream, err := dcs.Search(context.Background(), &dcspb.SearchRequest{
		Query:  strings.Join(fset.Args(), " "),
		Apikey: apikey,
	})
	if err != nil {
		return err
	}
	for {
		event, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		// if prog, ok := event.Data.(*dcspb.Event_Progress); !ok {
		// 	continue // TODO: compare the rest, too
		// } else {
		// 	if prog.Progress.FilesProcessed > 0 &&
		// 		prog.Progress.FilesProcessed < prog.Progress.FilesTotal {
		// 		continue // TODO: compare intermediate progress updates, too
		// 	}
		// }
		log.Printf("event: %+v", event)
		//events = append(events, event)
	}

	return nil
}
