package localdcs

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"time"

	"google.golang.org/grpc"

	"github.com/Debian/dcs/internal/computeranking"
	"github.com/Debian/dcs/internal/grpcutil"
	"github.com/Debian/dcs/internal/index"
	"github.com/Debian/dcs/internal/packageimporter"
	"github.com/Debian/dcs/internal/proto/packageimporterpb"
	"github.com/Debian/dcs/internal/proto/sourcebackendpb"
	"github.com/Debian/dcs/internal/ranking"
	"github.com/Debian/dcs/internal/sourcebackend"
	"github.com/Debian/dcs/internal/web"
	"github.com/Debian/dcs/static"
	"github.com/evanw/esbuild/pkg/api"
)

var (
	shardPath = flag.String("shard_path",
		"/tmp/dcs-hacking",
		"Path to the unpacked sources")
	localdcsPath = flag.String("localdcs_path",
		"~/.config/dcs-localdcs",
		"Directory in which to keep state for dcs-localdcs (TLS certificates, PID files, etc.)")
	listenPackageImporter = flag.String("listen_package_importer",
		"localhost:0",
		"listen address ([host]:port) for dcs-package-importer")
	listenIndexBackend = flag.String("listen_index_backend",
		"localhost:0",
		"listen address ([host]:port) for dcs-index-backend")
	listenSourceBackend = flag.String("listen_source_backend",
		"localhost:0",
		"listen address ([host]:port) for dcs-source-backend")
	listenWeb = flag.String("listen_web",
		"localhost:0",
		"listen address ([host]:port) for dcs-web (gRPC/TLS)")
)

func feed(packageImporter packageimporterpb.PackageImporterClient, pkg, file string) error {
	stream, err := packageImporter.Import(context.Background())
	if err != nil {
		return err
	}
	f, err := os.Open(file)
	if err != nil {
		return err
	}
	defer f.Close()
	buffer := make([]byte, 1*1024*1024) // 1 MB
	for {
		n, err := f.Read(buffer)
		if err != nil && err != io.EOF {
			return err
		}
		if err := stream.Send(&packageimporterpb.ImportRequest{
			SourcePackage: pkg,
			Filename:      filepath.Base(file),
			Content:       buffer[:n],
		}); err != nil {
			return err
		}
		if err == io.EOF {
			break
		}
	}
	_, err = stream.CloseAndRecv()
	return err
}

func importTestdata(packageImporterAddr string) error {
	conn, err := grpcutil.DialTLS(
		packageImporterAddr,
		filepath.Join(*localdcsPath, "cert.pem"),
		filepath.Join(*localdcsPath, "key.pem"))
	if err != nil {
		return fmt.Errorf("grpcutil.DialTLS(%s): %v", packageImporterAddr, err)
	}
	packageImporter := packageimporterpb.NewPackageImporterClient(conn)
	testdataFiles := make(map[string][]string)
	if err := filepath.Walk("testdata/pool", func(path string, info os.FileInfo, err error) error {
		if info.IsDir() {
			return nil
		}
		dir := filepath.Dir(path)
		testdataFiles[dir] = append(testdataFiles[dir], path)
		return nil
	}); err != nil {
		return err
	}
	// e.g. testdataFiles = map[
	//   testdata/pool/main/i/i3-wm:[
	//     testdata/pool/main/i/i3-wm/i3-wm_4.5.1-2.debian.tar.gz
	//     testdata/pool/main/i/i3-wm/i3-wm_4.5.1-2.dsc
	//     testdata/pool/main/i/i3-wm/i3-wm_4.5.1.orig.tar.bz2]
	//   testdata/pool/main/z/zsh:[
	//     testdata/pool/main/z/zsh/zsh_5.2-3.debian.tar.xz
	//     testdata/pool/main/z/zsh/zsh_5.2-3.dsc
	//     testdata/pool/main/z/zsh/zsh_5.2.orig.tar.xz]]
	numPackages := 0
	for _, files := range testdataFiles {
		var dsc string
		var rest []string
		for _, file := range files {
			if filepath.Ext(file) == ".dsc" {
				dsc = file
			} else {
				rest = append(rest, file)
			}
		}
		if dsc == "" {
			continue
		}
		numPackages++
		// e.g.:
		// dsc "testdata/pool/main/i/i3-wm/i3-wm_4.5.1-2.dsc"
		// rest [
		//   testdata/pool/main/i/i3-wm/i3-wm_4.5.1-2.debian.tar.gz
		//   testdata/pool/main/i/i3-wm/i3-wm_4.5.1.orig.tar.bz2]
		pkg := strings.TrimSuffix(filepath.Base(dsc), ".dsc")
		log.Printf("Importing package %q (files %v, dsc %s)\n", pkg, rest, dsc)
		for _, file := range append(rest, dsc) {
			for _, dir := range []string{"idx", "src"} {
				if err := os.RemoveAll(filepath.Join(*shardPath, dir, pkg)); err != nil {
					return err
				}
			}
			if err := feed(packageImporter, pkg, file); err != nil {
				return err
			}
		}
	}

	// Merge twice to always exercise the overwriting code path (localdcs is
	// used by endtoend_test.go):
	for range 2 {
		if _, err := packageImporter.Merge(context.Background(), &packageimporterpb.MergeRequest{}); err != nil {
			return err
		}
	}
	return nil
}

type Instance struct {
	localdcsPath string
	Addr         string
	HTTPClient   *http.Client
}

func Start(hashKey, blockKey string) (*Instance, error) {
	if len(*localdcsPath) >= 2 && (*localdcsPath)[:2] == "~/" {
		usr, err := user.Current()
		if err != nil {
			return nil, fmt.Errorf("Cannot expand -localdcs_path: %v", err)
		}
		*localdcsPath = strings.Replace(*localdcsPath, "~/", usr.HomeDir+"/", 1)
	}

	if err := os.MkdirAll(*localdcsPath, 0700); err != nil {
		return nil, fmt.Errorf("Could not create directory %q for dcs-localdcs state: %v", *localdcsPath, err)
	}

	for _, dir := range []string{
		*shardPath,
		filepath.Join(*shardPath, "src"),
		filepath.Join(*shardPath, "idx"),
	} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("Could not create directory %q for unpacked files/index: %v", dir, err)
		}
	}

	if _, err := os.Stat(filepath.Join(*localdcsPath, "key.pem")); os.IsNotExist(err) {
		log.Printf("Generating TLS certificate\n")
		if err := generatecert(*localdcsPath); err != nil {
			return nil, fmt.Errorf("Could not generate TLS certificate: %v", err)
		}
	}

	rankingPath := filepath.Join(*localdcsPath, "ranking.json")
	if stat, err := os.Stat(rankingPath); err != nil || time.Since(stat.ModTime()) > 7*24*time.Hour {
		log.Printf("Computing ranking data\n")
		const mirrorURL = "http://deb.debian.org/debian"
		const verbose = false
		os.Setenv("TMPDIR", *localdcsPath)
		if err := computeranking.Main(mirrorURL, rankingPath, verbose); err != nil {
			return nil, fmt.Errorf("Could not compute ranking data: %v", err)
		}
	} else {
		log.Printf("Recent-enough rankings file %q found, not re-generating (delete to force)\n", rankingPath)
	}

	rankingMap, err := ranking.ReadRankingData(rankingPath)
	if err != nil {
		log.Fatal(err)
	}

	indexPath := filepath.Join(*shardPath, "full")
	openPath := indexPath
	if _, err := os.Stat(openPath); os.IsNotExist(err) {
		tmp, err := os.MkdirTemp("", "dcs-index-backend")
		if err != nil {
			log.Fatal(err)
		}
		defer os.Remove(tmp)
		openPath = tmp
		ix, err := index.Create(openPath)
		if err != nil {
			log.Fatal(err)
		}
		if err := ix.Flush(); err != nil {
			log.Fatal(err)
		}
	}

	ix, err := index.Open(openPath)
	if err != nil {
		log.Fatal(err)
	}

	unpacked, err := os.OpenRoot(filepath.Join(*shardPath, "src"))
	if err != nil {
		log.Fatal(err)
	}

	srv := &sourcebackend.Server{
		Index:              ix,
		UnpackedPath:       unpacked,
		IndexPath:          indexPath,
		UsePositionalIndex: true,
		RankingMap:         rankingMap,
	}
	ln, err := net.Listen("tcp", *listenSourceBackend)
	if err != nil {
		log.Fatal(err)
	}
	sourceBackend := ln.Addr().String()
	go func() {
		log.Fatal(grpcutil.ListenAndServeTLS(ln,
			http.DefaultServeMux,
			filepath.Join(*localdcsPath, "cert.pem"),
			filepath.Join(*localdcsPath, "key.pem"),
			false,
			func(s *grpc.Server) {
				sourcebackendpb.RegisterSourceBackendServer(s, srv)
			}))
	}()

	// TODO: check for healthiness

	log.Printf("dcs-source-backend running at https://%s\n", sourceBackend)

	// Start package importer and import testdata/
	impOpts := packageimporter.Opts{
		SourceBackendAddr: sourceBackend,
		DebugSkip:         true,
		TLSCertPath:       filepath.Join(*localdcsPath, "cert.pem"),
		TLSKeyPath:        filepath.Join(*localdcsPath, "key.pem"),
		ListenAddress:     *listenPackageImporter,
		ShardPath:         *shardPath,
	}

	impLn, err := net.Listen("tcp", impOpts.ListenAddress)
	if err != nil {
		return nil, err
	}
	packageImporter := impLn.Addr().String()

	go func() {
		log.Fatal(impOpts.Main(impLn))
	}()

	if err := importTestdata(packageImporter); err != nil {
		return nil, fmt.Errorf("Could not import testdata/: %v", err)
	}

	// TODO: check for healthiness

	// Minify all assets and serve them on an HTTP ServeMux
	// (in production, this happens in the reverse proxy, not DCS).
	webMux := http.NewServeMux()
	for _, js := range []string{
		"cssrelpreload.js",
		"instant.js",
		"loadCSS.js",
		"service-worker.js",
	} {
		min := strings.TrimSuffix(js, ".js") + ".min.js"
		b, err := fs.ReadFile(static.FS, js)
		if err != nil {
			return nil, err
		}
		res := api.Transform(string(b), api.TransformOptions{
			Loader:            api.LoaderJS,
			MinifyWhitespace:  true,
			MinifyIdentifiers: true,
			MinifySyntax:      true,
		})
		if len(res.Errors) > 0 {
			return nil, fmt.Errorf("esbuild.minify(%s): %v", min, res.Errors)
		}
		webMux.HandleFunc("GET /"+min, func(w http.ResponseWriter, r *http.Request) {
			http.ServeContent(w, r, min, time.Time{}, bytes.NewReader(res.Code))
		})
	}
	var criticalCSS []byte
	for _, css := range []string{
		"critical.css",
		"non-critical.css",
	} {
		min := strings.TrimSuffix(css, ".css") + ".min.css"
		b, err := fs.ReadFile(static.FS, css)
		if err != nil {
			return nil, err
		}
		res := api.Transform(string(b), api.TransformOptions{
			Loader:            api.LoaderCSS,
			MinifyWhitespace:  true,
			MinifyIdentifiers: true,
			MinifySyntax:      true,
		})
		if len(res.Errors) > 0 {
			return nil, fmt.Errorf("esbuild.minify(%s): %v", min, res.Errors)
		}
		if css == "critical.css" {
			criticalCSS = res.Code
		}
		webMux.HandleFunc("GET /"+min, func(w http.ResponseWriter, r *http.Request) {
			http.ServeContent(w, r, min, time.Time{}, bytes.NewReader(res.Code))
		})
	}
	// Concatenate debian.css and debcodesearch.css to debcodesearch.min.css.
	{
		const min = "debcodesearch.min.css"
		debianCSS, err := fs.ReadFile(static.FS, "debian.css")
		if err != nil {
			return nil, err
		}
		dcsCSS, err := fs.ReadFile(static.FS, "debcodesearch.css")
		if err != nil {
			return nil, err
		}
		res := api.Transform(string(append(debianCSS, dcsCSS...)), api.TransformOptions{
			Loader:            api.LoaderCSS,
			MinifyWhitespace:  true,
			MinifyIdentifiers: true,
			MinifySyntax:      true,
		})
		if len(res.Errors) > 0 {
			return nil, fmt.Errorf("esbuild.minify(%s): %v", min, res.Errors)
		}
		webMux.HandleFunc("GET /"+min, func(w http.ResponseWriter, r *http.Request) {
			http.ServeContent(w, r, min, time.Time{}, bytes.NewReader(res.Code))
		})
	}
	webOpts := web.Opts{
		Mux:                webMux,
		ListenAddress:      *listenWeb,
		ListenAddressPlain: "localhost:0",
		TLSCertPath:        filepath.Join(*localdcsPath, "cert.pem"),
		TLSKeyPath:         filepath.Join(*localdcsPath, "key.pem"),
		SourceBackends:     sourceBackend,
		QueryResultsPath:   filepath.Join(*localdcsPath, "qr"),
		HashKeyStr:         hashKey,
		BlockKeyStr:        blockKey,
		CriticalCSS:        criticalCSS,
	}
	webLn, err := net.Listen("tcp", webOpts.ListenAddress)
	if err != nil {
		return nil, err
	}
	dcsWeb := webLn.Addr().String()

	go func() {
		log.Fatal(webOpts.Main(webLn))
	}()

	log.Printf("dcs-web running at https://%s\n", dcsWeb)

	instance := &Instance{
		localdcsPath: *localdcsPath,
		Addr:         dcsWeb,
	}
	instance.HTTPClient, err = instance.httpClient()
	if err != nil {
		return nil, err
	}
	return instance, nil
}

func (i *Instance) httpClient() (*http.Client, error) {
	certFile := filepath.Join(i.localdcsPath, "cert.pem")
	keyFile := filepath.Join(i.localdcsPath, "key.pem")

	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, err
	}

	// Load CA cert
	caCert, err := os.ReadFile(certFile)
	if err != nil {
		return nil, err
	}
	caCertPool := x509.NewCertPool()
	caCertPool.AppendCertsFromPEM(caCert)

	// Setup HTTPS client
	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		RootCAs:      caCertPool,
	}
	tlsConfig.BuildNameToCertificate()
	transport := &http.Transport{TLSClientConfig: tlsConfig}
	return &http.Client{Transport: transport}, nil
}
