// This is a re-implementation of debmirror which is optimized for the Debian
// Code Search use-case: a full sid sources-only mirror, downloaded from
// high-bandwidth mirrors with a Gigabit uplink.
//
// See this blog post for details:
// http://people.debian.org/~stapelberg/2014/01/17/debmirror-rackspace.html
package main

import (
	"compress/gzip"
	"flag"
	"fmt"
	"github.com/stapelberg/godebiancontrol"
	"io"
	"log"
	"math/rand"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

var (
	numTcpConns = flag.Int("tcp_conns",
		10,
		"How many TCP connections to use in parallel. Note that there is no "+
			"per-mirror connection limit, i.e. when you use -tcp_conns=30 with "+
			"3 mirrors, 10 connections will be opened to each mirror.")

	// "http://mirror.steadfast.net/debian/", // doesn’t do keep-alive
	// "http://debian.mirrors.tds.net/debian/", // doesn’t do keep-alive
	// "http://debian.corenetworks.net/debian/", // doesn’t do keep-alive
	// "http://ftp.us.debian.org/debian/", // had download errors (EOF)
	// "http://mirrors.liquidweb.com/debian/", // rather slow
	// "http://mirror.nexcess.net/debian/", // doesn’t do keep-alive
	// "http://mirror.thelinuxfix.com/debian/", // often outdated
	// "http://debian.savoirfairelinux.net/debian/", // much slower than the other mirrors
	// "http://mirrors.xmission.com/debian/", // rather slow
	mirrorList = flag.String("mirrors",
		"http://mirror.fdcservers.net/debian/,"+
			"http://mirrors.gigenet.com/debian/,"+
			"http://debian.mirror.constant.com/debian/,",
		"Comma-separated list of Debian mirrors.")

	mirrorDir = flag.String("mirror_path",
		"/dcs/source-mirror/",
		"Local filesystem path where the mirror should be downloaded to")
)

type queuedFile struct {
	Path string
	Size int
}

type Queue []queuedFile

func (v Queue) Len() int {
	return len(v)
}

func (v Queue) Swap(i, j int) {
	v[i], v[j] = v[j], v[i]
}

func (v Queue) Less(i, j int) bool {
	return v[i].Size < v[j].Size
}

// Reads the Sources.gz file and returns a slice of queuedFile entries
// containing all files that should be downloaded.
func buildQueue(sourcesPath string, mirrorDir string) (Queue, error) {
	queue := Queue{}
	file, err := os.Open(sourcesPath)
	if err != nil {
		return queue, err
	}
	defer file.Close()

	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		return queue, err
	}
	defer gzipReader.Close()

	sourcePackages, err := godebiancontrol.Parse(gzipReader)
	if err != nil {
		return queue, err
	}

	// On average, there are 3 files per source package, so allocate a slice
	// with that capacity to avoid re-allocating memory all the time.
	queue = make(Queue, 0, len(sourcePackages)*3)
	seen := make(map[string]bool, len(sourcePackages)*3)
	for _, pkg := range sourcePackages {
		for _, line := range strings.Split(pkg["Files"], "\n") {
			parts := strings.Split(strings.TrimSpace(line), " ")
			// pkg["Files"] has a newline at the end, so we get one empty line.
			if len(parts) < 3 {
				continue
			}
			size, err := strconv.Atoi(parts[1])
			if err != nil {
				return queue, err
			}
			path := pkg["Directory"] + "/" + parts[2]

			// Skip duplicates.
			if seen[path] {
				continue
			}

			// Skip the file if it is already present and has the same size.
			// Files with the same name (in /pool/) cannot change, so we don’t
			// need to consider the checksum at all.
			fi, err := os.Stat(filepath.Join(mirrorDir, path))
			if err == nil && fi.Size() == int64(size) {
				continue
			}

			queue = append(queue, queuedFile{
				Path: path,
				Size: size,
			})
			seen[path] = true
		}
	}

	return queue, nil
}

func work(c chan []queuedFile, done chan int, reschedule chan queuedFile, moreWorkers chan string, index int, mirror string) {
	u, err := url.Parse(mirror)
	if err != nil {
		log.Fatalf(`Cannot parse URL "%s": %v\n`, mirror, err)
	}
	conn, err := net.Dial("tcp", u.Host+":80")
	if err != nil {
		log.Fatalf(`Could not connect to mirror "%s": %v\n`, mirror, err)
	}
	defer conn.Close()
	hconn := httputil.NewClientConn(conn, nil)

	type requestSlot struct {
		req     *http.Request
		started time.Time
		queued  queuedFile
	}
	slots := [99]requestSlot{}

	bytesRead := int64(0)
	filesRead := 0
	start := time.Now()

	batch := <-c
	if len(batch) > len(slots) {
		log.Fatal("BUG: len(batch) > len(slots)")
	}

	// Send ALL the requests!
	for i, current := range batch {
		req, err := http.NewRequest("GET", mirror+current.Path, nil)
		if err != nil {
			log.Fatalf("Could not create new HTTP request: %v\n", err)
		}
		req.Header.Add("Connection", "keep-alive")
		// TODO: this is not deleted for some reason.
		req.Header.Del("Accept-Encoding")

		fmt.Printf("[%d] slot %d sending request %s\n", index, i, mirror+current.Path)
		if err := hconn.Write(req); err != nil {
			log.Fatalf("Could not write HTTP request: %v\n", err)
		}
		slots[i] = requestSlot{req, time.Now(), current}
	}

	i := 0
	for i = 0; i < len(batch); i++ {
		current := slots[i].queued
		response, err := hconn.Read(slots[i].req)
		if err != nil {
			log.Printf("Could not read %s: %v\n", mirror+current.Path, err)

			// Reschedule all remaining requests onto a different worker.
			for j := i; j < len(batch); j++ {
				reschedule <- slots[j].queued
			}
			if i <= 75 {
				moreWorkers <- mirror
			}
			log.Printf("[%d] DEATHSTATS: read %d files with %d bytes total from %s\n",
				index, filesRead, bytesRead, mirror)
			done <- filesRead
			return
		}
		defer response.Body.Close()

		if response.StatusCode != 200 {
			log.Printf("[%d] status code %d for %s, rescheduling\n",
				index, response.StatusCode, mirror+current.Path)
			reschedule <- slots[i].queued
			continue
		}

		localPath := filepath.Join(*mirrorDir, current.Path)

		err = os.MkdirAll(filepath.Dir(localPath), 0755)
		if err != nil {
			log.Fatal(err)
		}
		out, err := os.Create(localPath)
		if err != nil {
			log.Fatal(err)
		}
		defer out.Close()
		written, err := io.Copy(out, response.Body)
		if err != nil {
			log.Fatal(err)
		}

		//fmt.Printf("[%d] status code %d, connection %s, read %d bytes \n", index, response.StatusCode, response.Header.Get("Connection"), written)

		bytesRead += written
		filesRead++

		if i == 75 {
			moreWorkers <- mirror
		}
	}

	// If this batch was small (or had a lot of non-200 responses), we did not
	// get around to start a worker, so request one.
	if i < 75 {
		moreWorkers <- mirror
	}

	duration := time.Since(start)
	log.Printf("[%d] DEATHSTATS: read %d files with %d bytes total from %s, that is %f MB/s\n",
		index, filesRead, bytesRead, mirror, float64(bytesRead)/1024/1024/duration.Seconds())
	done <- filesRead
}

func downloadSourcesGz(mirrors []string, sourcesUrl, sourcesPath string) error {
	log.Printf(`Downloading "%s" to "%s"`, sourcesUrl, sourcesPath)
	if err := os.MkdirAll(filepath.Dir(sourcesPath), 0755); err != nil {
		return err
	}

	out, err := os.Create(sourcesPath)
	if err != nil {
		return err
	}
	defer out.Close()

	response, err := http.Get(sourcesUrl)
	if err != nil {
		return err
	}
	defer response.Body.Close()

	if _, err := io.Copy(out, response.Body); err != nil {
		return err
	}

	return nil
}

func main() {
	flag.Parse()
	rand.Seed(time.Now().Unix())

	mirrors := strings.Split(strings.TrimRight(*mirrorList, ","), ",")
	log.Printf("Using mirrors %v\n", mirrors)

	// Download a fresh Sources.gz file first. We use gzip because it unpacks
	// significantly faster, and we have the bandwidth for the extra megabyte
	// or two.
	sourcesSuffix := "dists/sid/main/source/Sources.gz"
	sourcesPath := filepath.Join(*mirrorDir, sourcesSuffix)
	sourcesUrl := mirrors[rand.Int()%len(mirrors)] + sourcesSuffix
	if err := downloadSourcesGz(mirrors, sourcesUrl, sourcesPath); err != nil {
		log.Fatalf(`Error downloading sources.gz: %v`, err)
	}

	queue, err := buildQueue(sourcesPath, *mirrorDir)
	if err != nil {
		log.Fatal(err)
	}
	bytesTotal := 0
	for _, entry := range queue {
		bytesTotal += entry.Size
	}
	log.Printf("Downloading %d files (%d MB total)\n", len(queue), bytesTotal/1024/1024)

	// Sort the queue by size.
	sort.Sort(queue)

	stepWidth := len(queue) / 99

	// At 75 of its 99 requests, a worker will send its mirror string to this
	// channel to request a new worker.
	moreWorkers := make(chan string)

	// Every queue entry that cannot be downloaded (e.g. because of a 404, or
	// the mirror closing connection) will be sent to this channel and
	// rescheduled onto any worker (hopefully a different one at some point).
	reschedule := make(chan queuedFile)

	// Batches of up to 99 requests will be sent to the workers using this
	// channel.
	batches := make(chan []queuedFile)

	// Workers report how many files they processed
	done := make(chan int)

	for i := 0; i < *numTcpConns; i++ {
		go work(batches, done, reschedule, moreWorkers, i, mirrors[i%len(mirrors)])
	}

	go func() {
		i := *numTcpConns
		for {
			<-moreWorkers
			log.Printf("starting worker %d for mirror %s\n", i, mirrors[i%len(mirrors)])
			go work(batches, done, reschedule, moreWorkers, i, mirrors[i%len(mirrors)])
			i += 1
		}
	}()

	// This Goroutine collects unsuccessful downloads (non-200, broken
	// connections, file not yet on specific mirror, …) and sends them to the
	// workqueue as either a full 99-file batch or as a smaller batch after a
	// 10 second timeout elapsed (to avoid deadlocks).
	go func() {
		batch := []queuedFile{}

		for {
			select {
			case current := <-reschedule:
				log.Printf("Re-scheduling URL %s (%d)\n", current.Path, current.Size)
				batch = append(batch, current)
				if len(batch) == 99 {
					batches <- batch
					batch = []queuedFile{}
				}
			case <-time.After(10 * time.Second):
				if len(batch) > 0 {
					batches <- batch
					batch = []queuedFile{}
				}
			}
		}
	}()

	downloadStart := time.Now()

	// This Goroutine distributes the entire queue in batches of 99 files.
	// Each batch has a good mix of files, starting with a big file to get the
	// TCP window sizes up.
	go func() {
		for j := 0; j < stepWidth; j++ {
			batch := make([]queuedFile, 99)
			for i := 98; i >= 0; i-- {
				log.Printf("Batch %d includes entry %d: %s (%d)\n", j, j+(i*stepWidth), queue[j+(i*stepWidth)].Path, queue[j+(i*stepWidth)].Size)
				batch[98-i] = queue[j+(i*stepWidth)]
			}

			batches <- batch
		}

		// The previous loop does not reach all elements of the queue because the
		// queue is not a multiple of 99, so distribute the left-over entries.
		// It is important to distribute many small batches here since the end
		// of the queue is where the biggest files live. If we distributed just
		// one big batch, one mirror would end up serving a lot of big files.
		for i := 99 * stepWidth; i < len(queue); i++ {
			batches <- []queuedFile{queue[i]}
		}
	}()

	remaining := len(queue)
	for remaining > 0 {
		select {
		case processed := <-done:
			remaining -= processed
		case <-time.After(10 * time.Second):
			fmt.Printf("[STATUS] Downloaded %d/%d files\n", len(queue)-remaining, len(queue))
		}
	}

	elapsed := time.Since(downloadStart)
	log.Printf("All %d files downloaded in %v. Download rate is %f MB/s\n",
		len(queue), elapsed, float64(bytesTotal)/1024/1024/elapsed.Seconds())
}
