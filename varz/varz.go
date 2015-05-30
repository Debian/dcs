// Exports runtime variables in a machine-readable format for monitoring.
package varz

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

var (
	availFS = flag.String("varz_avail_fs",
		"/dcs-ssd",
		"If non-empty, /varz will contain the number of available bytes on the specified filesystem")
	counters = make(map[string]*counter)

	started = time.Now()
)

const (
	bytesPerSector = 512
)

var (
	memAllocBytesGauge = prometheus.NewGaugeFunc(
		prometheus.GaugeOpts{
			Subsystem: "process",
			Name:      "mem_alloc_bytes",
			Help:      "Bytes allocated and still in use.",
		},
		func() float64 {
			var m runtime.MemStats
			runtime.ReadMemStats(&m)
			return float64(m.Alloc)
		},
	)

	availFSGauge = prometheus.NewGaugeFunc(
		prometheus.GaugeOpts{
			Name: "avail_fs_bytes",
			Help: "Number of available bytes on -varz_avail_fs.",
		},
		func() float64 {
			if *availFS != "" {
				var stat syscall.Statfs_t
				if err := syscall.Statfs(*availFS, &stat); err != nil {
					log.Printf("Could not stat filesystem for %q: %v\n", *availFS, err)
				} else {
					return float64(stat.Bavail * uint64(stat.Bsize))
				}
			}
			return 0
		},
	)
)

type cpuTimeMetric struct {
	counter *prometheus.CounterVec
}

func (ct *cpuTimeMetric) Describe(ch chan<- *prometheus.Desc) {
	ct.counter.Describe(ch)
}

func (ct cpuTimeMetric) Collect(ch chan<- prometheus.Metric) {
	var rusage syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &rusage); err == nil {
		m := ct.counter.WithLabelValues("user")
		m.Set(float64(syscall.TimevalToNsec(rusage.Utime)))
		ch <- m

		m = ct.counter.WithLabelValues("system")
		m.Set(float64(syscall.TimevalToNsec(rusage.Stime)))
		ch <- m
	}
}

type diskStatsMetric struct {
	reads        *prometheus.CounterVec
	writes       *prometheus.CounterVec
	readbytes    *prometheus.CounterVec
	writtenbytes *prometheus.CounterVec
}

func (ct *diskStatsMetric) Describe(ch chan<- *prometheus.Desc) {
	ct.reads.Describe(ch)
	ct.writes.Describe(ch)
	ct.readbytes.Describe(ch)
	ct.writtenbytes.Describe(ch)
}

func (ct diskStatsMetric) Collect(ch chan<- prometheus.Metric) {
	diskstats, err := os.Open("/proc/diskstats")
	if err != nil {
		return
	}
	defer diskstats.Close()

	scanner := bufio.NewScanner(diskstats)
	for scanner.Scan() {
		// From http://sources.debian.net/src/linux/3.16.7-2/block/genhd.c/?hl=1141#L1141
		// seq_printf(seqf, "%4d %7d %s %lu %lu %lu %u %lu %lu %lu %u %u %u %u\n",
		var major, minor uint64
		var device string
		var reads, mergedreads, readsectors, readms uint64
		var writes, mergedwrites, writtensectors, writems uint64
		var inflight, ioticks, timeinqueue uint64
		fmt.Sscanf(scanner.Text(), "%4d %7d %s %d %d %d %d %d %d %d %d %d %d %d",
			&major, &minor, &device,
			&reads, &mergedreads, &readsectors, &readms,
			&writes, &mergedwrites, &writtensectors, &writems,
			&inflight, &ioticks, &timeinqueue)
		// Matches sda, xvda, …
		if !strings.HasSuffix(device, "da") {
			continue
		}
		m := ct.reads.WithLabelValues(device)
		m.Set(float64(reads))
		ch <- m
		m = ct.writes.WithLabelValues(device)
		m.Set(float64(writes))
		ch <- m
		m = ct.readbytes.WithLabelValues(device)
		m.Set(float64(readsectors * bytesPerSector))
		ch <- m
		m = ct.writtenbytes.WithLabelValues(device)
		m.Set(float64(writtensectors * bytesPerSector))
		ch <- m
	}
}

func init() {
	prometheus.MustRegister(memAllocBytesGauge)
	prometheus.MustRegister(availFSGauge)

	prometheus.MustRegister(&cpuTimeMetric{prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Subsystem: "process",
			Name:      "cpu_nsec",
			Help:      "CPU time spent in ns, split by user/system.",
		},
		[]string{"mode"},
	)})

	prometheus.MustRegister(&diskStatsMetric{
		reads: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Subsystem: "system",
				Name:      "disk_reads",
				Help:      "Disk reads, per device name (e.g. xvda).",
			},
			[]string{"device"},
		),
		writes: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Subsystem: "system",
				Name:      "disk_writes",
				Help:      "Disk writes, per device name (e.g. xvda).",
			},
			[]string{"device"},
		),
		readbytes: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Subsystem: "system",
				Name:      "disk_read_bytes",
				Help:      "Bytes read from disk, per device name (e.g. xvda).",
			},
			[]string{"device"},
		),
		writtenbytes: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Subsystem: "system",
				Name:      "disk_written_bytes",
				Help:      "Bytes written to disk, per device name (e.g. xvda).",
			},
			[]string{"device"},
		),
	})
}

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

func (c *counter) Subtract() {
	c.lock.Lock()
	c.value -= 1
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

	var rusage syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &rusage); err == nil {
		fmt.Fprintf(w, "cpu-time-user-ns %d\n", syscall.TimevalToNsec(rusage.Utime))
		fmt.Fprintf(w, "cpu-time-system-ns %d\n", syscall.TimevalToNsec(rusage.Stime))
	}

	diskstats, err := os.Open("/proc/diskstats")
	if err != nil {
		return
	}
	defer diskstats.Close()

	scanner := bufio.NewScanner(diskstats)
	for scanner.Scan() {
		// From http://sources.debian.net/src/linux/3.16.7-2/block/genhd.c/?hl=1141#L1141
		// seq_printf(seqf, "%4d %7d %s %lu %lu %lu %u %lu %lu %lu %u %u %u %u\n",
		var major, minor uint64
		var device string
		var reads, mergedreads, readsectors, readms uint64
		var writes, mergedwrites, writtensectors, writems uint64
		var inflight, ioticks, timeinqueue uint64
		fmt.Sscanf(scanner.Text(), "%4d %7d %s %d %d %d %d %d %d %d %d %d %d %d",
			&major, &minor, &device,
			&reads, &mergedreads, &readsectors, &readms,
			&writes, &mergedwrites, &writtensectors, &writems,
			&inflight, &ioticks, &timeinqueue)
		// Matches sda, xvda, …
		if !strings.HasSuffix(device, "da") {
			continue
		}
		fmt.Fprintf(w, "dev-reads.%s %d\n", device, reads)
		fmt.Fprintf(w, "dev-bytes-read.%s %d\n", device, readsectors*bytesPerSector)
		fmt.Fprintf(w, "dev-writes.%s %d\n", device, writes)
		fmt.Fprintf(w, "dev-bytes-written.%s %d\n", device, writtensectors*bytesPerSector)
	}
}

func Increment(key string) {
	if c, ok := counters[key]; ok {
		c.Add()
	} else {
		counters[key] = &counter{value: 1}
	}
}

func Decrement(key string) {
	if c, ok := counters[key]; ok {
		c.Subtract()
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
