// Exports runtime variables using prometheus.
package varz

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"os"
	"runtime"
	"strings"
	"syscall"

	"github.com/prometheus/client_golang/prometheus"
)

var (
	availFS = flag.String("varz_avail_fs",
		"/dcs-ssd",
		"If non-empty, /varz will contain the number of available bytes on the specified filesystem")
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
	desc *prometheus.Desc
}

func (ct *cpuTimeMetric) Describe(ch chan<- *prometheus.Desc) {
	ch <- ct.desc
}

func (ct cpuTimeMetric) Collect(ch chan<- prometheus.Metric) {
	var rusage syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &rusage); err == nil {
		ch <- prometheus.MustNewConstMetric(ct.desc, prometheus.CounterValue, float64(syscall.TimevalToNsec(rusage.Utime)), "user")

		ch <- prometheus.MustNewConstMetric(ct.desc, prometheus.CounterValue, float64(syscall.TimevalToNsec(rusage.Stime)), "system")
	}
}

type diskStatsMetric struct {
	reads        *prometheus.Desc
	writes       *prometheus.Desc
	readbytes    *prometheus.Desc
	writtenbytes *prometheus.Desc
}

func (ct *diskStatsMetric) Describe(ch chan<- *prometheus.Desc) {
	ch <- ct.reads
	ch <- ct.writes
	ch <- ct.readbytes
	ch <- ct.writtenbytes
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
		ch <- prometheus.MustNewConstMetric(ct.reads, prometheus.CounterValue, float64(reads), device)
		ch <- prometheus.MustNewConstMetric(ct.writes, prometheus.CounterValue, float64(writes), device)
		ch <- prometheus.MustNewConstMetric(ct.readbytes, prometheus.CounterValue, float64(readsectors*bytesPerSector), device)
		ch <- prometheus.MustNewConstMetric(ct.writtenbytes, prometheus.CounterValue, float64(writtensectors*bytesPerSector), device)
	}
}

func init() {
	prometheus.MustRegister(memAllocBytesGauge)
	prometheus.MustRegister(availFSGauge)

	prometheus.MustRegister(&cpuTimeMetric{
		desc: prometheus.NewDesc("process_cpu_nsec",
			"CPU time spent in ns, split by user/system.",
			[]string{"mode"},
			nil),
	})

	prometheus.MustRegister(&diskStatsMetric{
		reads: prometheus.NewDesc("system_disk_reads",
			"Disk reads, per device name (e.g. xvda).",
			[]string{"device"},
			nil),

		writes: prometheus.NewDesc("system_disk_writes",
			"Disk writes, per device name (e.g. xvda).",
			[]string{"device"},
			nil),

		readbytes: prometheus.NewDesc("disk_read_bytes",
			"Bytes read from disk, per device name (e.g. xvda).",
			[]string{"device"},
			nil),

		writtenbytes: prometheus.NewDesc("disk_written_bytes",
			"Bytes written to disk, per device name (e.g. xvda).",
			[]string{"device"},
			nil),
	})
}

// vim:ts=4:sw=4:noexpandtab
