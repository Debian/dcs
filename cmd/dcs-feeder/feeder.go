// Takes care of feeding packages to the dcs-package-importer processes running
// on index/source backends.
//
// Notifications about new packages can be delivered via the /lookfor endpoint
// on demand (e.g. by dcs-tail-fedmsg).
//
// Additionally, every hour, the “Sources” file will be downloaded and its
// contents are compared to the contents of our index/source backends.
package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"flag"
	"io"
	"log"
	"math"
	"net/http"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/Debian/dcs/goroutinez"
	"github.com/Debian/dcs/grpcutil"
	"github.com/Debian/dcs/internal/proto/packageimporterpb"
	"github.com/Debian/dcs/shardmapping"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stapelberg/godebiancontrol"
	"pault.ag/go/debian/version"

	_ "net/http/pprof"

	_ "github.com/Debian/dcs/varz"
)

type mergeState struct {
	firstRequest time.Time
	lastActivity time.Time
}

type packageImporter struct {
	shard string
	packageimporterpb.PackageImporterClient
}

var (
	shardsStr = flag.String("shards",
		"localhost:21010",
		"comma-separated list of shards")

	mirrorUrl = flag.String("mirror_url",
		"http://deb.debian.org/debian",
		"Debian mirror url")

	listenAddress = flag.String("listen_address",
		":21020",
		"listen address ([host]:port)")

	dist = flag.String("dist",
		"sid",
		"Debian distribution to feed")

	tlsCertPath = flag.String("tls_cert_path", "", "Path to a .pem file containing the TLS certificate.")

	tlsKeyPath = flag.String("tls_key_path", "", "Path to a .pem file containing the TLS private key.")

	initial = flag.Bool("initial", false, "Whether this is the initial feeder call, in which case Merge will not be called on the dcs-package-importer jobs, speeding up the run.")

	packageImporters []*packageImporter

	mergeStates   = make(map[int]mergeState)
	mergeStatesMu sync.Mutex

	failedLookfor = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "lookfor_failed",
			Help: "Failed lookfor requests.",
		})

	successfulLookfor = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "lookfor_successful",
			Help: "Successful lookfor requests.",
		})

	successfulGarbageCollect = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "garbage_collect_successful",
			Help: "Successful garbage collects.",
		})

	successfulSanityFeed = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "sanity_feed_successful",
			Help: "Successful sanity feeds.",
		})

	lastSanityCheckStarted = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "last_sanity_check_started",
			Help: "Unix timestamp of when the last sanity check was started.",
		})
)

func init() {
	prometheus.MustRegister(failedLookfor)
	prometheus.MustRegister(successfulLookfor)
	prometheus.MustRegister(successfulGarbageCollect)
	prometheus.MustRegister(successfulSanityFeed)
	prometheus.MustRegister(lastSanityCheckStarted)
}

// Calls /merge on the specified shard in a period of inactivity (2 minutes
// after being called), or every 10 minutes in case there is continuous
// activity.
func merge() {
	for {
		time.Sleep(10 * time.Second)
		mergeStatesMu.Lock()
		for shardIdx, state := range mergeStates {
			if time.Since(state.lastActivity) >= 2*time.Minute ||
				time.Since(state.firstRequest) >= 10*time.Minute {
				importer := packageImporters[shardIdx]
				log.Printf("Calling /merge on shard %d (%s) now\n", shardIdx, importer.shard)
				if _, err := importer.Merge(context.Background(), &packageimporterpb.MergeRequest{}); err != nil {
					log.Printf("/merge for shard %s failed (retry in 10s): %v\n", importer.shard, err)
					continue
				}
				delete(mergeStates, shardIdx)
			}
		}
		mergeStatesMu.Unlock()
	}
}

func requestMerge(idx int) {
	mergeStatesMu.Lock()
	defer mergeStatesMu.Unlock()
	if currentState, ok := mergeStates[idx]; !ok {
		mergeStates[idx] = mergeState{
			firstRequest: time.Now(),
			lastActivity: time.Now(),
		}
	} else {
		mergeStates[idx] = mergeState{
			firstRequest: currentState.firstRequest,
			lastActivity: time.Now(),
		}
	}
}

// feed uploads the file to the corresponding dcs-package-importer.
func feed(pkg, filename string, reader io.Reader) error {
	shardIdx := shardmapping.TaskIdxForPackage(pkg, len(packageImporters))
	shard := packageImporters[shardIdx]

	stream, err := shard.Import(context.Background())
	if err != nil {
		return err
	}
	buffer := make([]byte, 1*1024*1024) // 1 MB
	for {
		n, err := reader.Read(buffer)
		if err != nil && err != io.EOF {
			return err
		}
		if err := stream.Send(&packageimporterpb.ImportRequest{
			SourcePackage: pkg,
			Filename:      filename,
			Content:       buffer[:n],
		}); err != nil {
			return err
		}
		if err == io.EOF {
			break
		}
	}
	if _, err := stream.CloseAndRecv(); err != nil {
		return err
	}

	if strings.HasSuffix(filename, ".dsc") {
		requestMerge(shardIdx)
	}

	return nil
}

func feedfiles(pkg string, pkgfiles []string) {
	for _, url := range pkgfiles {
		resp, err := http.Get(url)
		if err != nil {
			log.Printf("Skipping %s: %v\n", pkg, err)
			// Skip packages that can not be downloaded fully.
			break
		}
		if resp.StatusCode != 200 {
			log.Printf("Skipping %s: URL %q: %v\n", pkg, url, resp.Status)
			break
		}
		defer resp.Body.Close()
		if err := feed(pkg, filepath.Base(url), resp.Body); err != nil {
			log.Printf("feed(%q, %q): %v", pkg, filepath.Base(url), err)
		}
	}
}

func lookforHandler(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	r.Body.Close()

	go lookfor(r.Form.Get("file"))
}

func poolPath(filename string) string {
	firstLevel := string(filename[0])
	if strings.HasPrefix(filename, "lib") {
		firstLevel = filename[:len("libx")]
	}
	return "pool/main/" + firstLevel + "/" + filename[:strings.Index(filename, "_")] + "/" + filename
}

// Tries to download a package directly from http://incoming.debian.org
// (typically called from dcs-tail-fedmsg).
// See also https://lists.debian.org/debian-devel-announce/2014/08/msg00008.html
func lookfor(dscName string) {
	log.Printf("Looking for %q\n", dscName)
	startedLooking := time.Now()
	attempt := 0
	for {
		if attempt > 0 {
			// Exponential backoff starting with 8s.
			backoff := time.Duration(math.Pow(2, float64(attempt)+2)) * time.Second
			log.Printf("Starting attempt %d. Waiting %v\n", attempt+1, backoff)
			time.Sleep(backoff)
		}
		attempt++

		// We only try to get this file for 25 minutes. Something is probably
		// wrong if it does not succeed within that time, and we want to keep
		// goroutines from piling up. The periodic sanity check will find the
		// package a bit later then.
		if time.Since(startedLooking) > 25*time.Minute {
			failedLookfor.Inc()
			log.Printf("Not looking for %q anymore. Sanity check will catch it.\n", dscName)
			return
		}

		url := "http://incoming.debian.org/debian-buildd/" + poolPath(dscName)
		resp, err := http.Get(url)
		if err != nil {
			log.Printf("Could not HTTP GET %q: %v\n", url, err)
			continue
		}
		defer resp.Body.Close()

		if resp.StatusCode != 200 {
			log.Printf("HTTP status for %q: %s\n", url, resp.Status)
			continue
		}
		log.Printf("Downloading %q from incoming.debian.org\n", dscName)
		var dscContents bytes.Buffer
		// Store a copy of the content in dscContents.
		reader := io.TeeReader(resp.Body, &dscContents)
		// Strip the PGP signature. The worst thing that can happen is that an
		// attacker gives us bad source code to index and serve. Verifying PGP
		// signatures is harder since we need an up-to-date debian-keyring.
		reader = godebiancontrol.PGPSignatureStripper(reader)
		paragraphs, err := godebiancontrol.Parse(reader)
		if err != nil {
			log.Printf("Invalid dsc file: %v\n", err)
			return
		}

		if len(paragraphs) != 1 {
			log.Printf("Expected parsing exactly one paragraph, got %d. Skipping.\n", len(paragraphs))
		}
		pkg := paragraphs[0]

		for _, line := range strings.Split(pkg["Files"], "\n") {
			parts := strings.Split(strings.TrimSpace(line), " ")
			// pkg["Files"] has a newline at the end, so we get one empty line.
			if len(parts) < 3 {
				continue
			}
			fileUrl := "http://incoming.debian.org/debian-buildd/" + poolPath(parts[2])
			resp, err := http.Get(fileUrl)
			if err != nil {
				log.Printf("Could not HTTP GET %q: %v\n", url, err)
				return
			}
			defer resp.Body.Close()
			if err := feed(strings.TrimSuffix(dscName, ".dsc"), parts[2], resp.Body); err != nil {
				log.Printf("Could not feed %q: %v\n", url, err)
			}
		}
		dscReader := bytes.NewReader(dscContents.Bytes())
		if err := feed(strings.TrimSuffix(dscName, ".dsc"), dscName, dscReader); err != nil {
			log.Printf("Could not feed %q: %v\n", dscName, err)
		}
		log.Printf("Fed %q.\n", dscName)
		successfulLookfor.Inc()
		return
	}
}

func checkSources() {
	log.Printf("checking sources\n")
	lastSanityCheckStarted.Set(float64(time.Now().Unix()))

	// Store packages by shard.
	type pkgStatus int
	const (
		NotPresent pkgStatus = iota
		Present
		Confirmed
	)
	packages := make(map[string]map[string]pkgStatus)

	for _, importer := range packageImporters {
		resp, err := importer.Packages(context.Background(), &packageimporterpb.PackagesRequest{})
		if err != nil {
			log.Printf("packageImporter.Packages: %v", err)
			return
		}
		packages[importer.shard] = make(map[string]pkgStatus)
		for _, foundpkg := range resp.SourcePackage {
			packages[importer.shard][foundpkg] = Present
		}
		log.Printf("shard %q has %d packages currently\n", importer.shard, len(resp.SourcePackage))
	}

	sourcesSuffix := "/dists/" + *dist + "/main/source/Sources.gz"
	resp, err := http.Get(*mirrorUrl + sourcesSuffix)
	if err != nil {
		log.Printf("Could not get Sources.gz: %v\n", err)
		return
	}
	defer resp.Body.Close()
	reader, err := gzip.NewReader(resp.Body)
	if err != nil {
		log.Printf("Could not initialize gzip reader: %v\n", err)
		return
	}
	defer reader.Close()

	sourcePackages, err := godebiancontrol.Parse(reader)
	if err != nil {
		log.Printf("Could not parse Sources.gz: %v\n", err)
		return
	}

	// Only keep the most recent version for each source package:
	mostRecent := make(map[string]godebiancontrol.Paragraph)
	for _, pkg := range sourcePackages {
		n := pkg["Package"]
		if current, ok := mostRecent[n]; ok {
			old, err := version.Parse(current["Version"])
			if err != nil {
				log.Printf("version %q: %v", current["Version"], err)
				return
			}
			new, err := version.Parse(pkg["Version"])
			if err != nil {
				log.Printf("version %q: %v", pkg["Version"], err)
				return
			}
			if version.Compare(new, old) > 0 {
				mostRecent[n] = pkg
			}
		} else {
			mostRecent[n] = pkg
		}
	}

	sem := make(chan struct{}, runtime.NumCPU())
	shardMu := make([]sync.Mutex, len(packageImporters))

	// for every package, calculate who’d be responsible and see if it’s present on that shard.
	for _, pkg := range mostRecent {
		if strings.HasSuffix(pkg["Package"], "-data") {
			continue
		}
		if pkg["Package"] == "kicad-packages3d" {
			continue // TODO: should this have been called -data?
		}
		p := pkg["Package"] + "_" + pkg["Version"]
		shardIdx := shardmapping.TaskIdxForPackage(p, len(packageImporters))
		importer := packageImporters[shardIdx]
		// Skip shards that are offline (= for which we have no package list).
		if _, online := packages[importer.shard]; !online {
			continue
		}
		status := packages[importer.shard][p]
		//log.Printf("package %s: shard %d (%s), status %v\n", p, shardIdx, shard, status)
		if status == Present {
			packages[importer.shard][p] = Confirmed
		} else if status == NotPresent {
			var pkgfiles []string
			for _, line := range strings.Split(pkg["Files"], "\n") {
				parts := strings.Split(strings.TrimSpace(line), " ")
				// pkg["Files"] has a newline at the end, so we get one empty line.
				if len(parts) < 3 {
					continue
				}
				url := *mirrorUrl + "/" + pkg["Directory"] + "/" + parts[2]

				// Append the .dsc to the end, prepend the other files.
				if strings.HasSuffix(url, ".dsc") {
					pkgfiles = append(pkgfiles, url)
				} else {
					pkgfiles = append([]string{url}, pkgfiles...)
				}
			}
			sem <- struct{}{}
			go func() {
				defer func() { <-sem }()
				shardMu[shardIdx].Lock()
				defer shardMu[shardIdx].Unlock()
				log.Printf("Feeding package %s to shard %d (%s)\n", p, shardIdx, importer.shard)
				feedfiles(p, pkgfiles)

				successfulSanityFeed.Inc()
			}()
		}
	}
	// Wait for completion
	for n := cap(sem); n > 0; n-- {
		sem <- struct{}{}
	}

	// Garbage-collect all packages that have not been confirmed.
	for _, importer := range packageImporters {
		for p, status := range packages[importer.shard] {
			if status != Present {
				continue
			}

			log.Printf("garbage-collecting %q on shard %s\n", p, importer.shard)

			//importer := packageImporters[shardmapping.TaskIdxForPackage(p, len(packageImporters))]
			if _, err := importer.GarbageCollect(context.Background(), &packageimporterpb.GarbageCollectRequest{
				SourcePackage: p,
			}); err != nil {
				log.Printf("Could not garbage-collect package %q on shard %s: %v\n", p, importer.shard, err)
				continue
			}

			successfulGarbageCollect.Inc()
		}
	}
}

func main() {
	flag.Parse()

	shards := strings.Split(*shardsStr, ",")
	packageImporters = make([]*packageImporter, len(shards))
	log.Printf("Configuration: %d shards:\n", len(shards))
	for idx, shard := range shards {
		log.Printf("  %q\n", shard)

		conn, err := grpcutil.DialTLS(shard, *tlsCertPath, *tlsKeyPath)
		if err != nil {
			log.Fatalf("could not connect to %q: %v", shard, err)
		}
		defer conn.Close()
		packageImporters[idx] = &packageImporter{
			shard:                 shard,
			PackageImporterClient: packageimporterpb.NewPackageImporterClient(conn),
		}
	}

	// Calls /merge once appropriate.
	if !*initial {
		go merge()
	}

	// Calls checkSources() every hour (sanity check, so that /lookfor is not critical).
	go func() {
		for {
			checkSources()
			time.Sleep(1 * time.Hour)
		}
	}()

	http.HandleFunc("/lookfor", lookforHandler)
	http.HandleFunc("/goroutinez", goroutinez.Goroutinez)
	http.Handle("/metrics", prometheus.Handler())

	log.Fatal(http.ListenAndServe(*listenAddress, nil))
}
