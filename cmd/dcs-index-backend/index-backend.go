// vim:ts=4:sw=4:noexpandtab
package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	_ "net/http/pprof"
	"os"
	"path/filepath"
	"runtime/pprof"
	"sync"
	"time"

	"github.com/Debian/dcs/grpcutil"
	oldindex "github.com/Debian/dcs/index"
	"github.com/Debian/dcs/internal/index"
	"github.com/Debian/dcs/internal/proto/indexbackendpb"
	_ "github.com/Debian/dcs/varz"
	"github.com/google/codesearch/regexp"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/uber/jaeger-client-go"
	jaegercfg "github.com/uber/jaeger-client-go/config"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
)

var (
	listenAddress = flag.String("listen_address", ":28081", "listen address ([host]:port)")
	indexPath     = flag.String("index_path", "", "path to the index shard to serve, e.g. /dcs-ssd/index.0.idx")
	cpuProfile    = flag.String("cpuprofile", "", "write cpu profile to this file")
	tlsCertPath   = flag.String("tls_cert_path", "", "Path to a .pem file containing the TLS certificate.")
	tlsKeyPath    = flag.String("tls_key_path", "", "Path to a .pem file containing the TLS private key.")
	jaegerAgent   = flag.String("jaeger_agent",
		"localhost:5775",
		"host:port of a github.com/uber/jaeger agent")
)

type server struct {
	id      string
	ix      *index.Index
	ixMutex sync.Mutex
}

// doPostingQuery runs the actual query. This code is in a separate function so
// that we can use defer (to be safe against panics in the index querying code)
// and still don’t hold the mutex for longer than we need to.
func (s *server) doPostingQuery(query *oldindex.Query, stream indexbackendpb.IndexBackend_FilesServer) error {
	s.ixMutex.Lock()
	defer s.ixMutex.Unlock()
	t0 := time.Now()
	post := s.ix.PostingQuery(query)
	t1 := time.Now()
	fmt.Printf("[%s] postingquery done in %v, %d results\n", s.id, t1.Sub(t0), len(post))
	var reply indexbackendpb.FilesReply
	for _, docid := range post {
		var err error
		reply.Path, err = s.ix.DocidMap.Lookup(docid)
		if err != nil {
			return err
		}
		if err := stream.Send(&reply); err != nil {
			return err
		}
	}
	t2 := time.Now()
	fmt.Printf("[%s] filenames collected in %v\n", s.id, t2.Sub(t1))
	return nil
}

// Handles requests to /index by compiling the q= parameter into a regular
// expression (codesearch/regexp), searching the index for it and returning the
// list of matching filenames in a JSON array.
// TODO: This doesn’t handle file name regular expressions at all yet.
// TODO: errors aren’t properly signaled to the requester
func (s *server) Files(in *indexbackendpb.FilesRequest, stream indexbackendpb.IndexBackend_FilesServer) error {
	if *cpuProfile != "" {
		f, err := os.Create(*cpuProfile)
		if err != nil {
			log.Fatal(err)
		}
		defer f.Close()
		pprof.StartCPUProfile(f)
		defer pprof.StopCPUProfile()
	}

	re, err := regexp.Compile(in.Query)
	if err != nil {
		return fmt.Errorf("regexp.Compile: %s\n", err)
	}
	query := oldindex.RegexpQuery(re.Syntax)
	log.Printf("[%s] query: text = %s, regexp = %s\n", s.id, in.Query, query)
	return s.doPostingQuery(query, stream)
}

func (s *server) ReplaceIndex(ctx context.Context, in *indexbackendpb.ReplaceIndexRequest) (*indexbackendpb.ReplaceIndexReply, error) {
	newShard := in.ReplacementPath

	file, err := os.Open(filepath.Dir(*indexPath))
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()
	names, err := file.Readdirnames(-1)
	if err != nil {
		log.Fatal(err)
	}

	for _, name := range names {
		if name == newShard {
			newShard = filepath.Join(filepath.Dir(*indexPath), name)
			// We verified the given argument refers to an index shard within
			// this directory, so let’s load this shard.
			oldIndex := s.ix
			log.Printf("Trying to load %q\n", newShard)
			newIndex, err := index.Open(newShard)
			if err != nil {
				return nil, err
			}
			s.ixMutex.Lock()
			s.ix = newIndex
			s.ixMutex.Unlock()
			// Overwrite the old full shard with the new one. This is necessary
			// so that the state is persistent across restarts and has the nice
			// side-effect of cleaning up the old full shard.
			if err := os.Rename(newShard, *indexPath); err != nil {
				log.Fatal(err)
			}
			oldIndex.Close()
			return &indexbackendpb.ReplaceIndexReply{}, nil
		}
	}

	return nil, fmt.Errorf("No such shard.")
}

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	flag.Parse()
	if *indexPath == "" {
		log.Fatal("You need to specify a non-empty -index_path")
	}

	cfg := jaegercfg.Configuration{
		Sampler: &jaegercfg.SamplerConfig{
			Type:  "const",
			Param: 1,
		},
		Reporter: &jaegercfg.ReporterConfig{
			BufferFlushInterval: 1 * time.Second,
			LocalAgentHostPort:  *jaegerAgent,
		},
	}
	closer, err := cfg.InitGlobalTracer(
		"dcs-index-backend",
		jaegercfg.Logger(jaeger.StdLogger),
	)
	if err != nil {
		log.Fatal(err)
	}
	defer closer.Close()

	http.Handle("/metrics", prometheus.Handler())

	idx := *indexPath
	if _, err := os.Stat(idx); os.IsNotExist(err) {
		tmp, err := ioutil.TempDir("", "dcs-index-backend")
		if err != nil {
			log.Fatal(err)
		}
		defer os.Remove(tmp)
		idx = tmp
		ix, err := index.Create(idx)
		if err != nil {
			log.Fatal(err)
		}
		if err := ix.Flush(); err != nil {
			log.Fatal(err)
		}
	}

	ix, err := index.Open(idx)
	if err != nil {
		log.Fatal(err)
	}

	log.Fatal(grpcutil.ListenAndServeTLS(*listenAddress,
		*tlsCertPath,
		*tlsKeyPath,
		func(s *grpc.Server) {
			indexbackendpb.RegisterIndexBackendServer(s, &server{
				id: filepath.Base(*indexPath),
				ix: ix,
			})
		}))
}
