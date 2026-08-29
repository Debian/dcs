package shardmapping

import (
	"crypto/md5"
	"fmt"
	"io"
	"log"
	"strconv"
)

func TaskIdxForPackage(pkg string, tasks int) int {
	h := md5.New()
	io.WriteString(h, pkg)
	i, err := strconv.ParseInt(fmt.Sprintf("%x", h.Sum(nil)[:6]), 16, 64)
	if err != nil {
		log.Fatal(err)
	}
	return int(i) % tasks
}
