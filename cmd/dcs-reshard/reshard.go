// Generates shell scripts to be run on each old shard. These scripts copy data
// around to the new shards. Use this whenever your -shards= configuration on
// dcs-feeder changes and you don’t want to start over with downloading data.
package main

import (
	"compress/gzip"
	"crypto/md5"
	"flag"
	"fmt"
	"github.com/stapelberg/godebiancontrol"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
)

var (
	mirrorUrl = flag.String("mirror_url",
		"http://ftp.ch.debian.org/debian",
		"Debian mirror URL")
	oldShardsStr = flag.String("old_shards",
		"10.209.68.76:21010,10.209.68.12:21010,10.209.68.22:21010,10.209.68.74:21010,10.209.66.198:21010",
		"comma-separated list of shards")
	newShardsStr = flag.String("new_shards",
		"10.209.68.76:21010,10.209.68.12:21010,10.209.68.22:21010,10.209.68.74:21010,10.209.66.198:21010,10.209.102.194:21010",
		"comma-separated list of shards")

	oldShards []string
	newShards []string
)

func taskIdxForPackage(pkg string, tasks int) int {
	h := md5.New()
	io.WriteString(h, pkg)
	i, err := strconv.ParseInt(fmt.Sprintf("%x", h.Sum(nil)[:6]), 16, 64)
	if err != nil {
		log.Fatal(err)
	}
	return int(i) % tasks
}

func main() {
	flag.Parse()

	oldShards = strings.Split(*oldShardsStr, ",")
	newShards = strings.Split(*newShardsStr, ",")

	sourcesSuffix := "/dists/sid/main/source/Sources.gz"
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

	scripts := make([]*os.File, len(oldShards))
	for idx, _ := range oldShards {
		scripts[idx], err = os.Create(fmt.Sprintf("/tmp/dcs-instant-%d.rackspace.zekjur.net", idx) + ".sh")
		if err != nil {
			log.Fatal(err)
		}
		defer scripts[idx].Close()
	}

	// for every package, calculate who’d be responsible and see if it’s present on that shard.
	for _, pkg := range sourcePackages {
		p := pkg["Package"] + "_" + pkg["Version"]
		oldIdx := taskIdxForPackage(p, len(oldShards))
		newIdx := taskIdxForPackage(p, len(newShards))
		log.Printf("oldidx = %d, newidx = %d\n", oldIdx, newIdx)
		if oldIdx == newIdx {
			continue
		}
		fmt.Fprintf(scripts[oldIdx], "scp -o StrictHostKeyChecking=no -i ~/.ssh/dcs-auto-rs -r /dcs-ssd/unpacked/%s /dcs-ssd/unpacked/%s.idx root@%s:/dcs-ssd/unpacked/ && rm -rf /dcs-ssd/unpacked/%s /dcs-ssd/unpacked/%s.idx\n",
			p, p, strings.TrimSuffix(newShards[newIdx], ":21010"), p, p)
	}
}
