// Accepts Debian packages via HTTP, unpacks, strips and indexes them.
package main

import (
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"strings"
	"time"
	"unicode/utf8"

	"google.golang.org/grpc"

	"github.com/Debian/dcs/grpcutil"
	"github.com/Debian/dcs/index"
	"github.com/Debian/dcs/internal/proto/indexbackendpb"
	"github.com/Debian/dcs/internal/proto/packageimporterpb"
	_ "github.com/Debian/dcs/varz"
	"github.com/prometheus/client_golang/prometheus"
	"golang.org/x/net/context"
	_ "golang.org/x/net/trace"
)

var (
	listenAddress = flag.String("listen_address",
		":21010",
		"listen address ([host]:port)")

	indexBackendAddr = flag.String("index_backend",
		"localhost:28081",
		"index backend host:port address")

	unpackedPath = flag.String("unpacked_path",
		"/dcs-ssd/unpacked/",
		"Path to the unpacked sources")

	cpuProfile = flag.String("cpuprofile",
		"",
		"write cpu profile to this file")

	debugSkip = flag.Bool("debug_skip",
		false,
		"Print log messages when files are skipped")

	tmpdir string

	failedDpkgSourceExtracts = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "dpkg_source_extracts_failed",
			Help: "Failed dpkg source extracts.",
		})

	failedPackageImports = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "package_imports_failed",
			Help: "Failed package imports.",
		})

	successfulDpkgSourceExtracts = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "dpkg_source_extracts_successful",
			Help: "Successful dpkg source extracts.",
		})

	successfulGarbageCollects = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "garbage_collects_successful",
			Help: "Successful garbage collects.",
		})

	successfulMerges = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "merges_successful",
			Help: "Successful merges.",
		})

	successfulPackageImports = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "package_imports_successful",
			Help: "Successful package imports.",
		})

	successfulPackageIndexes = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "package_indexes_successful",
			Help: "Successful package indexes.",
		})

	filesInIndex = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "index_files",
			Help: "Number of files in the index.",
		})

	tlsCertPath = flag.String("tls_cert_path", "", "Path to a .pem file containing the TLS certificate.")
	tlsKeyPath  = flag.String("tls_key_path", "", "Path to a .pem file containing the TLS private key.")
)

func init() {
	prometheus.MustRegister(failedDpkgSourceExtracts)
	prometheus.MustRegister(failedPackageImports)
	prometheus.MustRegister(successfulDpkgSourceExtracts)
	prometheus.MustRegister(successfulGarbageCollects)
	prometheus.MustRegister(successfulMerges)
	prometheus.MustRegister(successfulPackageImports)
	prometheus.MustRegister(successfulPackageIndexes)
	prometheus.MustRegister(filesInIndex)
}

type server struct {
	unpacksem chan struct{} // semaphore for unpackAndIndex
	mergesem  chan struct{} // semaphore for merge
}

// Accepts arbitrary files for a given package and starts unpacking once a .dsc
// file is uploaded. E.g.:
//
// curl -X PUT --data-binary @i3-wm_4.7.2-1.debian.tar.xz \
//     http://localhost:21010/import/i3-wm_4.7.2-1/i3-wm_4.7.2-1.debian.tar.xz
// curl -X PUT --data-binary @i3-wm_4.7.2.orig.tar.bz2 \
//     http://localhost:21010/import/i3-wm_4.7.2-1/i3-wm_4.7.2.orig.tar.bz2
// curl -X PUT --data-binary @i3-wm_4.7.2-1.dsc \
//     http://localhost:21010/import/i3-wm_4.7.2-1/i3-wm_4.7.2-1.dsc
//
// All the files are stored in the same directory and after the .dsc is stored,
// the package is unpacked with dpkg-source, then indexed.
func (s *server) Import(ctx context.Context, req *packageimporterpb.ImportRequest) (*packageimporterpb.ImportReply, error) {
	pkg := req.GetSourcePackage()
	filename := req.GetFilename()
	path := pkg + "/" + filename

	err := os.Mkdir(filepath.Join(tmpdir, pkg), 0755)
	if err != nil && !os.IsExist(err) {
		return nil, err
	}
	file, err := os.Create(filepath.Join(tmpdir, path))
	if err != nil {
		return nil, err
	}
	defer file.Close()
	written, err := file.Write(req.GetContent())
	if err != nil {
		return nil, err
	}
	log.Printf("Wrote %d bytes into %s\n", written, path)

	if strings.HasSuffix(filename, ".dsc") {
		s.unpacksem <- struct{}{}        // acquire
		defer func() { <-s.unpacksem }() // release
		if err := unpackAndIndex(path); err != nil {
			return nil, err
		}
	}

	successfulPackageImports.Inc()
	return &packageimporterpb.ImportReply{}, nil
}

// Tries to start a merge and errors in case one is already in progress.
func (s *server) Merge(context.Context, *packageimporterpb.MergeRequest) (*packageimporterpb.MergeReply, error) {
	select {
	case s.mergesem <- struct{}{}: // acquire
	default:
		return nil, fmt.Errorf("Merge already in progress, please try again later.")
	}
	defer func() { <-s.mergesem }() // release
	if err := mergeToShard(); err != nil {
		return nil, err
	}
	return &packageimporterpb.MergeReply{}, nil
}

func packageNames() []string {
	var names []string

	file, err := os.Open(*unpackedPath)
	// If the directory does not yet exist, we just return an empty list of
	// packages.
	if err != nil && !os.IsNotExist(err) {
		log.Fatal(err)
	}
	if err == nil {
		defer file.Close()
		names, err = file.Readdirnames(-1)
		if err != nil {
			log.Fatal(err)
		}
	}

	return names
}

func (s *server) Packages(ctx context.Context, req *packageimporterpb.PackagesRequest) (*packageimporterpb.PackagesReply, error) {
	names := packageNames()
	var reply packageimporterpb.PackagesReply
	reply.SourcePackage = make([]string, 0, len(names))
	for _, name := range names {
		if strings.HasSuffix(name, ".idx") && name != "full.idx" {
			reply.SourcePackage = append(reply.SourcePackage, name[:len(name)-len(".idx")])
		}
	}
	return &reply, nil
}

func (s *server) GarbageCollect(ctx context.Context, req *packageimporterpb.GarbageCollectRequest) (*packageimporterpb.GarbageCollectReply, error) {
	pkg := req.GetSourcePackage()
	if pkg == "" {
		return nil, fmt.Errorf("no source_package provided")
	}

	names := packageNames()
	found := false
	for _, name := range names {
		// Note that the logic is inverted in comparison to earlier in the
		// code: for listPackages, we want to only return packages that have
		// been unpacked and indexed (so we strip .idx), but for garbage
		// collection, we also want to garbage collect packages that were not
		// indexed for some reason, so we ignore .idx.
		if name == pkg && !strings.HasSuffix(name, ".idx") {
			found = true
			break
		}
	}

	if !found {
		return nil, fmt.Errorf("no such package")
	}

	if err := os.RemoveAll(filepath.Join(*unpackedPath, pkg)); err != nil {
		return nil, err
	}

	if err := os.Remove(filepath.Join(*unpackedPath, pkg+".idx")); err != nil {
		return nil, err
	}

	successfulGarbageCollects.Inc()
	return &packageimporterpb.GarbageCollectReply{}, nil
}

// Merges all packages in *unpackedPath into a big index shard.
func mergeToShard() error {
	names := packageNames()
	indexFiles := make([]string, 0, len(names))
	for _, name := range names {
		if strings.HasSuffix(name, ".idx") && name != "full.idx" {
			indexFiles = append(indexFiles, filepath.Join(*unpackedPath, name))
		}
	}

	filesInIndex.Set(float64(len(indexFiles)))

	if len(indexFiles) < 2 {
		return fmt.Errorf("got %d index files, want at least 2", len(indexFiles))
	}
	tmpIndexPath, err := ioutil.TempFile(*unpackedPath, "newshard")
	if err != nil {
		return err
	}

	if *cpuProfile != "" {
		f, err := os.Create(*cpuProfile)
		if err != nil {
			log.Fatal(err)
		}
		defer f.Close()
		pprof.StartCPUProfile(f)
		defer pprof.StopCPUProfile()
	}

	t0 := time.Now()
	index.ConcatN(tmpIndexPath.Name(), indexFiles...)
	t1 := time.Now()
	log.Printf("merged in %v\n", t1.Sub(t0))
	//for i := 1; i < len(indexFiles); i++ {
	//	log.Printf("merging %s with %s\n", indexFiles[i-1], indexFiles[i])
	//	t0 := time.Now()
	//	index.Concat(tmpIndexPath.Name(), indexFiles[i-1], indexFiles[i])
	//	t1 := time.Now()
	//	log.Printf("merged in %v\n", t1.Sub(t0))
	//}
	log.Printf("merged into shard %s\n", tmpIndexPath.Name())

	// If full.idx does not exist (i.e. on initial deployment), just move the
	// new index to full.idx, the dcs-index-backend will not be running anyway.
	fullIdxPath := filepath.Join(*unpackedPath, "full.idx")
	if _, err := os.Stat(fullIdxPath); os.IsNotExist(err) {
		if err := os.Rename(tmpIndexPath.Name(), fullIdxPath); err != nil {
			return err
		}
		return nil
	}

	successfulMerges.Inc()

	conn, err := grpcutil.DialTLS(*indexBackendAddr, *tlsCertPath, *tlsKeyPath)
	if err != nil {
		log.Fatalf("could not connect to %q: %v", "localhost:28081", err)
	}
	defer conn.Close()
	indexBackend := indexbackendpb.NewIndexBackendClient(conn)

	// Replace the current index with the newly created index.
	_, err = indexBackend.ReplaceIndex(
		context.Background(),
		&indexbackendpb.ReplaceIndexRequest{
			ReplacementPath: filepath.Base(tmpIndexPath.Name()),
		})
	if err != nil {
		return fmt.Errorf("indexBackend.ReplaceIndex(): %v", err)
	}
	return nil
}

func indexPackage(pkg string) error {
	log.Printf("Indexing %s\n", pkg)
	unpacked := filepath.Join(tmpdir, pkg, pkg)
	if err := os.MkdirAll(*unpackedPath, os.FileMode(0755)); err != nil {
		return err
	}

	// Write to a temporary file first so that merges can happen at the same
	// time. If we don’t do that, merges will try to use incomplete index
	// files, which are interpreted as corrupted.
	tmpIndexPath := filepath.Join(*unpackedPath, pkg+".tmp")
	index := index.Create(tmpIndexPath)
	// +1 because of the / that should not be included in the index.
	stripLen := len(filepath.Join(tmpdir, pkg)) + 1

	err := filepath.Walk(unpacked, func(path string, info os.FileInfo, err error) error {
		if dir, filename := filepath.Split(path); filename != "" {
			skip := ignored(info, dir, filename)
			if *debugSkip && skip != nil {
				log.Printf("Skipping %q: %v", path, skip)
			}
			if skip != nil && info.IsDir() {
				if err := os.RemoveAll(path); err != nil {
					log.Fatalf("Could not remove directory %q: %v\n", path, err)
				}
				return filepath.SkipDir
			}
			if skip != nil && !info.IsDir() {
				if err := os.Remove(path); err != nil {
					log.Fatalf("Could not remove file %q: %v\n", path, err)
				}
				return nil
			}
		}

		if info == nil || !info.Mode().IsRegular() {
			return nil
		}

		// Some filenames (e.g.
		// "xblast-tnt-levels_20050106-2/reconstruct\xeeon2.xal") contain
		// invalid UTF-8 and will break when sending them via JSON later
		// on. Filter those out early to avoid breakage.
		if !utf8.ValidString(path) {
			log.Printf("Skipping due to invalid UTF-8: %s\n", path)
			return nil
		}

		if err := index.AddFile(path, path[stripLen:]); err != nil {
			log.Printf("Could not index %q: %v\n", path, err)
			if err := os.Remove(path); err != nil {
				log.Fatalf("Could not remove file %q: %v\n", path, err)
			}
		} else {
			// Copy this file out of /tmp to our unpacked directory.
			outputPath := filepath.Join(*unpackedPath, path[stripLen:])
			if err := os.MkdirAll(filepath.Dir(outputPath), os.FileMode(0755)); err != nil {
				log.Fatalf("Could not create directory: %v\n", err)
			}
			output, err := os.Create(outputPath)
			if err != nil {
				log.Fatalf("Could not create output file %q: %v\n", outputPath, err)
			}
			defer output.Close()
			input, err := os.Open(path)
			if err != nil {
				log.Fatalf("Could not open input file %q: %v\n", path, err)
			}
			defer input.Close()
			if _, err := io.Copy(output, input); err != nil {
				log.Fatalf("Could not copy %q to %q: %v\n", path, outputPath, err)
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	index.Flush()

	finalIndexPath := filepath.Join(*unpackedPath, pkg+".idx")
	if err := os.Rename(tmpIndexPath, finalIndexPath); err != nil {
		return err
	}
	successfulPackageIndexes.Inc()
	return nil
}

func unpack(dscPath, unpacked string) error {
	cmd := exec.Command("dpkg-source", "--no-copy", "--no-check", "-x",
		dscPath, unpacked)
	// Just display dpkg-source’s stderr in our process’s stderr.
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s: %v", cmd.Args, err)
	}

	files, err := ioutil.ReadDir(unpacked)
	if err != nil {
		return err
	}

	for _, file := range files {
		if !file.Mode().IsRegular() {
			continue
		}
		if strings.Contains(file.Name(), ".tar.") {
			// shell out to tar so that we don’t need to deal with the various
			// compression formats
			cmd := exec.Command("tar", "xf", file.Name())
			cmd.Dir = unpacked
			cmd.Stderr = os.Stderr
			if err := cmd.Run(); err != nil {
				return err
			}
			// The tarball will be discarded later, but we might as well remove
			// it now to speed things up.
			os.Remove(filepath.Join(unpacked, file.Name()))
		}
	}

	return nil
}

// unpackAndIndex unpacks a .dsc file, indexes its contents and deletes the .dsc
// and referenced files.
func unpackAndIndex(dscPath string) error {
	pkg := filepath.Dir(dscPath)
	unpacked := filepath.Join(tmpdir, pkg, pkg)
	log.Printf("Unpacking source package %s into %s", pkg, unpacked)

	// Delete previous attempts, if any.
	if err := os.RemoveAll(unpacked); err != nil {
		return err
	}

	if err := unpack(filepath.Join(tmpdir, dscPath), unpacked); err != nil {
		failedDpkgSourceExtracts.Inc()
		return err
	}

	successfulDpkgSourceExtracts.Inc()
	if err := indexPackage(pkg); err != nil {
		return err
	}
	return os.RemoveAll(filepath.Join(tmpdir, pkg))
}

func main() {
	flag.Parse()

	if err := os.MkdirAll(*unpackedPath, 0755); err != nil {
		log.Fatal(err)
	}

	setupFilters()

	var err error
	tmpdir, err = ioutil.TempDir("", "dcs-importer")
	if err != nil {
		log.Fatal(err)
	}

	http.Handle("/metrics", prometheus.Handler())

	log.Fatal(grpcutil.ListenAndServeTLS(*listenAddress,
		*tlsCertPath,
		*tlsKeyPath,
		func(s *grpc.Server) {
			packageimporterpb.RegisterPackageImporterServer(s, &server{
				unpacksem: make(chan struct{}, runtime.NumCPU()),
				mergesem:  make(chan struct{}, 1),
			})
		}))
}
