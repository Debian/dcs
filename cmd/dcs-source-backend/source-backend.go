// vim:ts=4:sw=4:noexpandtab
package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"math/rand"
	"net/http"
	_ "net/http/pprof"
	"os"
	"strings"
	"time"

	"github.com/Debian/dcs/grpcutil"
	"github.com/Debian/dcs/internal/index"
	"github.com/Debian/dcs/internal/proto/sourcebackendpb"
	"github.com/Debian/dcs/internal/sourcebackend"
	"github.com/Debian/dcs/ranking"
	_ "github.com/Debian/dcs/varz"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/uber/jaeger-client-go"
	jaegercfg "github.com/uber/jaeger-client-go/config"
	"google.golang.org/grpc"
)

var (
	listenAddress = flag.String("listen_address", ":28082", "listen address ([host]:port)")

	indexPath    = flag.String("index_path", "", "path to the index shard to serve, e.g. /dcs-ssd/index.0.idx")
	unpackedPath = flag.String("unpacked_path",
		"/dcs-ssd/unpacked/",
		"Path to the unpacked sources")
	rankingDataPath = flag.String("ranking_data_path",
		"/var/dcs/ranking.json",
		"Path to the JSON containing ranking data")
	tlsCertPath = flag.String("tls_cert_path", "", "Path to a .pem file containing the TLS certificate.")
	tlsKeyPath  = flag.String("tls_key_path", "", "Path to a .pem file containing the TLS private key.")
	jaegerAgent = flag.String("jaeger_agent",
		"localhost:5775",
		"host:port of a github.com/uber/jaeger agent")

	usePositionalIndex = flag.Bool("use_positional_index",
		false,
		"use the pos and posrel index sections for identifier queries")
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	flag.Parse()

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
		"dcs-source-backend",
		jaegercfg.Logger(jaeger.StdLogger),
	)
	if err != nil {
		log.Fatal(err)
	}
	defer closer.Close()

	rand.Seed(time.Now().UnixNano())
	if !strings.HasSuffix(*unpackedPath, "/") {
		*unpackedPath = *unpackedPath + "/"
	}
	fmt.Println("Debian Code Search source-backend")

	if err := ranking.ReadRankingData(*rankingDataPath); err != nil {
		log.Fatal(err)
	}

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

	srv := &sourcebackend.Server{
		Index:              ix,
		UnpackedPath:       *unpackedPath,
		IndexPath:          *indexPath,
		UsePositionalIndex: *usePositionalIndex,
	}

	http.Handle("/metrics", prometheus.Handler())
	log.Fatal(grpcutil.ListenAndServeTLS(*listenAddress,
		*tlsCertPath,
		*tlsKeyPath,
		func(s *grpc.Server) {
			sourcebackendpb.RegisterSourceBackendServer(s, srv)
		}))
}
