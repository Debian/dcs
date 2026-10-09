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
	io.WriteString(h, name)
	i, err := strconv.ParseInt(fmt.Sprintf("%x", h.Sum(nil)[:6]), 16, 64)
	if err != nil {
		log.Fatal(err)
	}
	return int(i) % tasks
}
