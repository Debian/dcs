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
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	net_url "net/url"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Debian/dcs/goroutinez"
	"github.com/Debian/dcs/shardmapping"
	_ "github.com/Debian/dcs/varz"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stapelberg/godebiancontrol"
)

type mergeState struct {
	firstRequest time.Time
	lastActivity time.Time
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

	shards []string

	mergeStates   = make(map[string]mergeState)
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
// after being called), or after 10 minutes in case there is continuous
// activity.
func merge() {
	for {
		time.Sleep(10 * time.Second)
		mergeStatesMu.Lock()
		for shard, state := range mergeStates {
			if time.Since(state.lastActivity) >= 2*time.Minute ||
				time.Since(state.firstRequest) >= 10*time.Minute {
				log.Printf("Calling /merge on shard %s now\n", shard)
				resp, err := http.Get(fmt.Sprintf("http://%s/merge", shard))
				if err != nil {
					log.Printf("/merge for shard %s failed (retry in 10s): %v\n", shard, err)
					continue
				}
				resp.Body.Close()
				if resp.StatusCode != 200 {
					log.Printf("/merge for shard %s failed (retry in 10s): %+v\n", shard, resp)
					continue
				}
				delete(mergeStates, shard)
			}
		}
		mergeStatesMu.Unlock()
	}
}

func requestMerge(shard string) {
	mergeStatesMu.Lock()
	defer mergeStatesMu.Unlock()
	if currentState, ok := mergeStates[shard]; !ok {
		mergeStates[shard] = mergeState{
			firstRequest: time.Now(),
			lastActivity: time.Now(),
		}
	} else {
		mergeStates[shard] = mergeState{
			firstRequest: currentState.firstRequest,
			lastActivity: time.Now(),
		}
	}
}

// feed uploads the file to the corresponding dcs-package-importer.
func feed(pkg, filename string, reader io.Reader) error {
	shard := shards[shardmapping.TaskIdxForPackage(pkg, len(shards))]
	url := fmt.Sprintf("http://%s/import/%s/%s", shard, pkg, filename)
	request, err := http.NewRequest("PUT", url, reader)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(request)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	log.Printf("HTTP response for %q: %q\n", url, resp.Status)

	if resp.StatusCode == 200 && strings.HasSuffix(filename, ".dsc") {
		requestMerge(shard)
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
		feed(pkg, filepath.Base(url), resp.Body)
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

	for _, shard := range shards {
		url := fmt.Sprintf("http://%s/listpkgs", shard)
		resp, err := http.Get(url)
		if err != nil {
			log.Printf("Could not get list of packages from %q: %v\n", url, err)
			continue
		}
		defer resp.Body.Close()

		type ListPackageReply struct {
			Packages []string
		}

		var reply ListPackageReply
		decoder := json.NewDecoder(resp.Body)
		if err := decoder.Decode(&reply); err != nil {
			log.Printf("Invalid json from %q: %v\n", url, err)
			continue
		}
		packages[shard] = make(map[string]pkgStatus)
		for _, foundpkg := range reply.Packages {
			packages[shard][foundpkg] = Present
		}
		log.Printf("shard %q has %d packages currently\n", shard, len(reply.Packages))
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

	// for every package, calculate who’d be responsible and see if it’s present on that shard.
	for _, pkg := range sourcePackages {
		if strings.HasSuffix(pkg["Package"], "-data") {
			continue
		}
		p := pkg["Package"] + "_" + pkg["Version"]
		shardIdx := shardmapping.TaskIdxForPackage(p, len(shards))
		shard := shards[shardIdx]
		// Skip shards that are offline (= for which we have no package list).
		if _, online := packages[shard]; !online {
			continue
		}
		status := packages[shard][p]
		//log.Printf("package %s: shard %d (%s), status %v\n", p, shardIdx, shard, status)
		if status == Present {
			packages[shard][p] = Confirmed
		} else if status == NotPresent {
			log.Printf("Feeding package %s to shard %d (%s)\n", p, shardIdx, shard)

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
			feedfiles(p, pkgfiles)

			successfulSanityFeed.Inc()
		}
	}

	// Garbage-collect all packages that have not been confirmed.
	for _, shard := range shards {
		for p, status := range packages[shard] {
			if status != Present {
				continue
			}

			log.Printf("garbage-collecting %q on shard %s\n", p, shard)

			shard := shards[shardmapping.TaskIdxForPackage(p, len(shards))]
			url := fmt.Sprintf("http://%s/garbagecollect", shard)
			if _, err := http.PostForm(url, net_url.Values{"package": {p}}); err != nil {
				log.Printf("Could not garbage-collect package %q on shard %s: %v\n", p, shard, err)
				continue
			}

			successfulGarbageCollect.Inc()
		}
	}
}

func main() {
	flag.Parse()

	shards = strings.Split(*shardsStr, ",")

	log.Printf("Configuration: %d shards:\n", len(shards))
	for _, shard := range shards {
		log.Printf("  %q\n", shard)
	}

	// Calls /merge once appropriate.
	go merge()

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
