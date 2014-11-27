// Exports runtime variables in a machine-readable format for monitoring.
package varz

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"runtime"
	"sync"
	"syscall"
	"time"
)

var (
	availFS = flag.String("varz_avail_fs",
		"/dcs-ssd",
		"If non-empty, /varz will contain the amount of available bytes on the specified filesystem")
	counters = make(map[string]*counter)

	started = time.Now()
)

// A counter which is safe to use from multiple goroutines.
type counter struct {
	lock  sync.Mutex
	value uint64
}

func (c *counter) Add() {
	c.lock.Lock()
	c.value += 1
	c.lock.Unlock()
}

func (c *counter) Value() uint64 {
	return c.value
}

func Varz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Uptime", fmt.Sprintf("%d", time.Since(started)))
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	fmt.Fprintf(w, "num-goroutine %d\n", runtime.NumGoroutine())
	fmt.Fprintf(w, "mem-alloc-bytes %d\n", m.Alloc)
	fmt.Fprintf(w, "last-gc-absolute-ns %d\n", m.LastGC)
	for key, counter := range counters {
		fmt.Fprintf(w, "%s %d\n", key, counter.Value())
	}
	if *availFS != "" {
		var stat syscall.Statfs_t
		if err := syscall.Statfs(*availFS, &stat); err != nil {
			log.Printf("Could not stat filesystem for %q: %v\n", *availFS, err)
		} else {
			fmt.Fprintf(w, "available-bytes %d\n", stat.Bavail*uint64(stat.Bsize))
		}
	}
}

func Increment(key string) {
	if c, ok := counters[key]; ok {
		c.Add()
	} else {
		counters[key] = &counter{value: 1}
	}
}

func Set(key string, value uint64) {
	if c, ok := counters[key]; ok {
		c.value = value
	} else {
		counters[key] = &counter{value: value}
	}
}

// vim:ts=4:sw=4:noexpandtab
