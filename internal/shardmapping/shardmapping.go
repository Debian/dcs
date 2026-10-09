package shardmapping

import (
	"crypto/md5"
	"fmt"
	"io"
	"log"
	"strconv"
	"strings"
)

func TaskIdxForPackage(pkg string, tasks int) int {
	// Every Debian package follows name_version.
	name, _, _ := strings.Cut(pkg, "_")
	h := md5.New()
	// We hash the package name only (excluding the version)
	// so that when a new version enters the index,
	// the same shard is responsible for the old and new version
	// and can make the decision to keep the new one only.
	// See https://github.com/Debian/dcs/issues/136
	io.WriteString(h, name)
	i, err := strconv.ParseInt(fmt.Sprintf("%x", h.Sum(nil)[:6]), 16, 64)
	if err != nil {
		log.Fatal(err)
	}
	return int(i) % tasks
}
