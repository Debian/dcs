package sourcebackend

import (
	"context"
	"log"
	"sync"

	"github.com/Debian/dcs/internal/proto/sourcebackendpb"
)

type GRPCServer struct {
	// For forward compatibility
	sourcebackendpb.UnimplementedSourceBackendServer

	Server *Server
}

// Serves a single file for displaying it in /show
func (s *GRPCServer) File(ctx context.Context, in *sourcebackendpb.FileRequest) (*sourcebackendpb.FileReply, error) {
	log.Printf("requested filename *%s*\n", in.Path)
	contents, err := s.Server.ReadFile(ctx, in.Path)
	if err != nil {
		return nil, err
	}
	return &sourcebackendpb.FileReply{
		Contents: contents,
	}, nil
}

func (s *GRPCServer) ReplaceIndex(ctx context.Context, in *sourcebackendpb.ReplaceIndexRequest) (*sourcebackendpb.ReplaceIndexReply, error) {
	if err := s.Server.ReplaceIndex(in.ReplacementPath); err != nil {
		return nil, err
	}
	return &sourcebackendpb.ReplaceIndexReply{}, nil
}

type streamSink struct {
	mu     sync.Mutex
	stream sourcebackendpb.SourceBackend_SearchServer
}

func (ss *streamSink) Match(m *sourcebackendpb.Match) error {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	return ss.stream.Send(&sourcebackendpb.SearchReply{
		Type:  sourcebackendpb.SearchReply_MATCH,
		Match: m,
	})
}

func (ss *streamSink) Progress(filesProcessed, filesTotal int) error {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	return ss.stream.Send(&sourcebackendpb.SearchReply{
		Type: sourcebackendpb.SearchReply_PROGRESS_UPDATE,
		ProgressUpdate: &sourcebackendpb.ProgressUpdate{
			FilesProcessed: uint64(filesProcessed),
			FilesTotal:     uint64(filesTotal),
		},
	})
}

func (s *GRPCServer) Search(in *sourcebackendpb.SearchRequest, stream sourcebackendpb.SourceBackend_SearchServer) error {
	return s.Server.Search(stream.Context(), in, &streamSink{
		stream: stream,
	})
}
