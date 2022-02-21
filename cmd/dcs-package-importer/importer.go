// Accepts Debian packages via HTTP, unpacks, strips and indexes them.
package main

import (
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"runtime/pprof"
	"strings"
	"time"

	"github.com/Debian/dcs/grpcutil"
	"github.com/Debian/dcs/internal/filter"
	"github.com/Debian/dcs/internal/index"
	"github.com/prometheus/client_golang/prometheus"
	"golang.org/x/net/context"
	"google.golang.org/grpc"

	"github.com/Debian/dcs/internal/proto/packageimporterpb"
	"github.com/Debian/dcs/internal/proto/sourcebackendpb"

	_ "net/http/pprof"

	_ "github.com/Debian/dcs/varz"
	_ "golang.org/x/net/trace"
)

var (
	listenAddress = flag.String("listen_address",
		":21010",
		"listen address ([host]:port)")

	sourceBackendAddr = flag.String("source_backend",
		"localhost:28081",
		"source backend host:port address")

	shardPath = flag.String("shard_path",
		"/srv/dcs/shard0",
		"Path to the shard directory (containing src, idx, full)")

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
func (s *server) Import(stream packageimporterpb.PackageImporter_ImportServer) error {
	req, err := stream.Recv()
	if err != nil {
		return err
	}
	pkg := req.GetSourcePackage()
	filename := req.GetFilename()
	path := pkg + "/" + filename

	if err := os.Mkdir(filepath.Join(tmpdir, pkg), 0755); err != nil && !os.IsExist(err) {
		return err
	}
	file, err := os.Create(filepath.Join(tmpdir, path))
	if err != nil {
		return err
	}
	defer file.Close()
	var written int
	n, err := file.Write(req.GetContent())
	if err != nil {
		return err
	}
	written += n
	for {
		req, err := stream.Recv()
		if err != nil {
			if err == io.EOF {
				break
			}
			return err
		}

		n, err := file.Write(req.GetContent())
		if err != nil {
			return err
		}
		written += n
	}
	if err := file.Close(); err != nil {
		return err
	}
	log.Printf("Wrote %d bytes into %s\n", written, path)

	if strings.HasSuffix(filename, ".dsc") {
		s.unpacksem <- struct{}{}        // acquire
		defer func() { <-s.unpacksem }() // release
		if err := unpackAndIndex(path); err != nil {
			return err
		}
	}

	successfulPackageImports.Inc()
	return stream.SendAndClose(&packageimporterpb.ImportReply{})
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

func packageNames() ([]string, error) {
	var names []string

	file, err := os.Open(filepath.Join(*shardPath, "idx"))
	// If the directory does not yet exist, we just return an empty list of
	// packages.
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer file.Close()
	names, err = file.Readdirnames(-1)
	if err != nil {
		return nil, err
	}
	filtered := make([]string, 0, len(names))
	for _, n := range names {
		if strings.HasSuffix(n, ".tmp") {
			continue
		}
		filtered = append(filtered, n)
	}

	return filtered, nil
}

func (s *server) Packages(ctx context.Context, req *packageimporterpb.PackagesRequest) (*packageimporterpb.PackagesReply, error) {
	names, err := packageNames()
	if err != nil {
		return nil, err
	}
	return &packageimporterpb.PackagesReply{SourcePackage: names}, nil
}

func (s *server) GarbageCollect(ctx context.Context, req *packageimporterpb.GarbageCollectRequest) (*packageimporterpb.GarbageCollectReply, error) {
	pkg := req.GetSourcePackage()
	if pkg == "" {
		return nil, fmt.Errorf("no source_package provided")
	}

	names, err := packageNames()
	if err != nil {
		return nil, err
	}
	found := false
	for _, name := range names {
		// Note that the logic is inverted in comparison to earlier in the
		// code: for listPackages, we want to only return packages that have
		// been unpacked and indexed (so we strip .idx), but for garbage
		// collection, we also want to garbage collect packages that were not
		// indexed for some reason, so we ignore .idx.
		if name == pkg {
			found = true
			break
		}
	}

	if !found {
		return nil, fmt.Errorf("no such package")
	}

	if err := os.RemoveAll(filepath.Join(*shardPath, "src", pkg)); err != nil {
		return nil, err
	}

	if err := os.RemoveAll(filepath.Join(*shardPath, "idx", pkg)); err != nil {
		return nil, err
	}

	successfulGarbageCollects.Inc()
	return &packageimporterpb.GarbageCollectReply{}, nil
}

func cleanupUnsuccessfulMerges() error {
	fis, err := ioutil.ReadDir(*shardPath)
	if err != nil {
		return err
	}
	link, err := filepath.EvalSymlinks(filepath.Join(*shardPath, "full"))
	if err != nil {
		return err
	}
	var firstErr error
	for _, fi := range fis {
		if !strings.HasPrefix(fi.Name(), "full.") {
			continue
		}
		abs := filepath.Join(*shardPath, fi.Name())
		if abs == link {
			log.Printf("keeping %q (symlink destination)", fi.Name())
			continue
		}
		log.Printf("deleting unsuccessful merge attempt %q", fi.Name())
		if err := os.RemoveAll(abs); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// Merges all packages in *unpackedPath into a big index shard.
func mergeToShard() error {
	names, err := packageNames()
	if err != nil {
		return err
	}
	indexFiles := make([]string, len(names))
	for idx, name := range names {
		indexFiles[idx] = filepath.Join(*shardPath, "idx", name)
	}

	filesInIndex.Set(float64(len(indexFiles)))

	if len(indexFiles) < 2 {
		return fmt.Errorf("got %d index files, want at least 2", len(indexFiles))
	}

	if err := cleanupUnsuccessfulMerges(); err != nil {
		log.Printf("cleanupUnsuccessfulMerges: %v", err)
	}

	tmpIndexPath := filepath.Join(*shardPath, fmt.Sprintf("full.%d", time.Now().Unix()))
	if err := os.MkdirAll(tmpIndexPath, 0755); err != nil {
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
	if err := index.ConcatN(tmpIndexPath, indexFiles); err != nil {
		log.Printf("ConcatN: %v", err)
		return err
	}
	t1 := time.Now()
	log.Printf("merged in %v\n", t1.Sub(t0))
	//for i := 1; i < len(indexFiles); i++ {
	//	log.Printf("merging %s with %s\n", indexFiles[i-1], indexFiles[i])
	//	t0 := time.Now()
	//	index.Concat(tmpIndexPath.Name(), indexFiles[i-1], indexFiles[i])
	//	t1 := time.Now()
	//	log.Printf("merged in %v\n", t1.Sub(t0))
	//}
	log.Printf("merged into shard %s\n", tmpIndexPath)

	successfulMerges.Inc()

	conn, err := grpcutil.DialTLS(*sourceBackendAddr, *tlsCertPath, *tlsKeyPath)
	if err != nil {
		log.Fatalf("could not connect to %q: %v", *sourceBackendAddr, err)
	}
	defer conn.Close()
	sourceBackend := sourcebackendpb.NewSourceBackendClient(conn)

	// Replace the current index with the newly created index.
	_, err = sourceBackend.ReplaceIndex(
		context.Background(),
		&sourcebackendpb.ReplaceIndexRequest{
			ReplacementPath: filepath.Base(tmpIndexPath),
		})
	if err != nil {
		log.Printf("ReplaceIndex: %v", err)
		return fmt.Errorf("indexBackend.ReplaceIndex(): %v", err)
	}
	return nil
}

func indexPackage(pkg string) error {
	log.Printf("Indexing %s\n", pkg)
	unpacked := filepath.Join(tmpdir, pkg, pkg)
	if err := os.MkdirAll(filepath.Join(*shardPath, "idx"), os.FileMode(0755)); err != nil {
		return err
	}

	// Write to a temporary file first so that merges can happen at the same
	// time. If we don’t do that, merges will try to use incomplete index
	// files, which are interpreted as corrupted.
	tmpIndexPath := filepath.Join(*shardPath, "idx", pkg+".tmp")
	index, err := index.Create(tmpIndexPath)
	if err != nil {
		return err
	}
	// +1 because of the / that should not be included in the index.
	stripLen := len(filepath.Join(tmpdir, pkg)) + 1

	if err := index.AddDir(
		unpacked,
		filepath.Join(tmpdir, pkg)+"/",
		filter.Ignored,
		func(path string, info os.FileInfo, err error) error {
			if *debugSkip {
				log.Printf("skipping %q: %v", path, err)
			}
			// TODO: isn’t everything in |unpacked| deleted later on anyway?
			if info.IsDir() {
				return os.RemoveAll(path)
			}
			return os.Remove(path)
		},
		func(path string, info os.FileInfo) error {
			// Copy this file out of /tmp to our unpacked directory.
			outputPath := filepath.Join(*shardPath, "src", path[stripLen:])
			if err := os.MkdirAll(filepath.Dir(outputPath), os.FileMode(0755)); err != nil {
				return fmt.Errorf("Could not create directory: %v\n", err)
			}
			output, err := os.Create(outputPath)
			if err != nil {
				return fmt.Errorf("Could not create output file %q: %v\n", outputPath, err)
			}
			defer output.Close()
			input, err := os.Open(path)
			if err != nil {
				return fmt.Errorf("Could not open input file %q: %v\n", path, err)
			}
			defer input.Close()
			if _, err := io.Copy(output, input); err != nil {
				return fmt.Errorf("Could not copy %q to %q: %v\n", path, outputPath, err)
			}
			return nil
		},
	); err != nil {
		return err
	}
	if err := index.Flush(); err != nil {
		return err
	}

	finalIndexPath := filepath.Join(*shardPath, "idx", pkg)
	// Move the old index out of the way, if present
	oldIndexPath := filepath.Join(*shardPath, "idx", "O."+pkg)
	if err := os.Rename(finalIndexPath, oldIndexPath); err != nil {
		if os.IsNotExist(err) {
		} else {
			return err
		}
	} else {
		defer os.RemoveAll(oldIndexPath)
	}

	if err := os.Rename(tmpIndexPath, finalIndexPath); err != nil {
		return err
	}
	successfulPackageIndexes.Inc()
	return nil
}

var tarSuffixes = map[string]bool{
	".tar.gz":  true,
	".tar.lz":  true,
	".tar.bz2": true,
	".tar.xz":  true,
}

func isTar(filename string) bool {
	idx := strings.Index(filename, ".tar.")
	if idx == -1 {
		return false
	}
	return tarSuffixes[filename[idx:]]
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
		if isTar(file.Name()) {
			// shell out to tar so that we don’t need to deal with the various
			// compression formats
			cmd := exec.Command("tar", "xf", file.Name())
			cmd.Dir = unpacked
			cmd.Stderr = os.Stderr
			if err := cmd.Run(); err != nil {
				// Don’t fail unpacking if one of our tarballs which we
				// heuristically classified as interesting fails to unpack
				// (maybe because it is not a tarball after all?).
				log.Printf("(ignoring) %s: %v", cmd.Args, err)
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

	// Explicitly freeing OS memory prevents the importer from OOMing (running
	// Out Of Memory). For some reason, Go does not give back memory to the OS
	// even though it recognizes 90% of the heap as garbage:

	// viewcore /tmp/core.dcs-package-imp.23131.ex61.1543603411 --exe ./bin/dcs-package-importer breakdown
	//  all                  13451362304 100.00%
	//    text                  11816960   0.09%
	//    readonly               5779456   0.04%
	//    data                    516096   0.00%
	//    bss                  271654912   2.02% (grab bag, includes OS thread stacks, ...)
	//    heap               12750684160  94.79%
	//      in use spans     12155994112  90.37%
	//        alloc          12151868168  90.34%
	//          live            25765296   0.19%
	//          garbage      12126102872  90.15%
	//        free               4030056   0.03%
	//        round                95888   0.00%
	//      manual spans         1081344   0.01% (Go stacks)
	//        alloc               966656   0.01%
	//        free                114688   0.00%
	//      free spans         593608704   4.41%
	//        retained         593608704   4.41% (kept for reuse by Go)
	//        released                 0   0.00% (given back to the OS)
	//    ptr bitmap           398458880   2.96%
	//    span table            12451840   0.09%

	debug.FreeOSMemory()
	return os.RemoveAll(filepath.Join(tmpdir, pkg))
}

func packageImporter() error {
	if err := os.MkdirAll(*shardPath, 0755); err != nil {
		return err
	}

	filter.Init()

	var err error
	tmpdir, err = ioutil.TempDir("", "dcs-importer")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpdir)

	http.Handle("/metrics", prometheus.Handler())

	return grpcutil.ListenAndServeTLS(*listenAddress,
		*tlsCertPath,
		*tlsKeyPath,
		func(s *grpc.Server) {
			packageimporterpb.RegisterPackageImporterServer(s, &server{
				unpacksem: make(chan struct{}, runtime.NumCPU()),
				mergesem:  make(chan struct{}, 1),
			})
		})
}

func main() {
	flag.Parse()
	if err := packageImporter(); err != nil {
		log.Fatal(err)
	}
}
