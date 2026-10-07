package common

import (
	"context"
	"fmt"
	"io"

	"github.com/Debian/dcs/internal/proto/sourcebackendpb"
	"github.com/Debian/dcs/internal/sourcebackend"
	"google.golang.org/grpc"
)

func GRPCBackend(conn grpc.ClientConnInterface) Backend {
	return &grpcBackend{
		client: sourcebackendpb.NewSourceBackendClient(conn),
	}
}

type grpcBackend struct {
	client sourcebackendpb.SourceBackendClient
}

func (g *grpcBackend) Search(ctx context.Context, in *sourcebackendpb.SearchRequest, sink sourcebackend.SearchSink) error {
	stream, err := g.client.Search(ctx, in)
	if err != nil {
		return fmt.Errorf("Search: %v", err)
	}
	for {
		msg, err := stream.Recv()
		if err != nil {
			if err != io.EOF {
				return fmt.Errorf("stream.Recv: %v", err)
			}
			return nil
		}
		switch msg.Type {
		case sourcebackendpb.SearchReply_MATCH:
			if err := sink.Match(msg.Match); err != nil {
				return err
			}

		case sourcebackendpb.SearchReply_PROGRESS_UPDATE:
			if err := sink.Progress(int(msg.ProgressUpdate.FilesProcessed), int(msg.ProgressUpdate.FilesTotal)); err != nil {
				return err
			}
		}
	}
}

func (g *grpcBackend) ReadFile(ctx context.Context, path string) ([]byte, error) {
	resp, err := g.client.File(ctx, &sourcebackendpb.FileRequest{Path: path})
	if err != nil {
		return nil, err
	}
	return resp.Contents, nil
}
