// Exports runtime variables in a machine-readable format for monitoring.
package varz

import (
	"fmt"
	"net/http"
	"runtime"
	"sync"
)

var (
	counters = make(map[string]*counter)
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
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	fmt.Fprintf(w, "num-goroutine %d\n", runtime.NumGoroutine())
	fmt.Fprintf(w, "mem-alloc-bytes %d\n", m.Alloc)
	fmt.Fprintf(w, "last-gc-absolute-ns %d\n", m.LastGC)
	for key, counter := range counters {
		fmt.Fprintf(w, "%s %d\n", key, counter.Value())
	}
}

func Increment(key string) {
	if c, ok := counters[key]; ok {
		c.Add()
	} else {
		counters[key] = &counter{value: 1}
	}
}

// vim:ts=4:sw=4:noexpandtab
