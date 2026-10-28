[![GitHub Actions CI](https://github.com/Debian/dcs/actions/workflows/go.yml/badge.svg)](https://github.com/Debian/dcs/actions/workflows/go.yml)

Context:

* [Debian Code Search: Bachelor Thesis (2012)](https://codesearch.debian.net/research/bsc-thesis.pdf)
* [Announcing the new Debian Code Search Instant (2014)](https://michael.stapelberg.ch/posts/2014-12-03-debian-code-search-instant/)
  * [Debian Code Search: taming the latency tail (2014)](https://michael.stapelberg.ch/posts/2014-12-23-code-search-taming-the-latency-tail/)
* [Debian Code Search: improving client-side latency (2016)](https://michael.stapelberg.ch/posts/2016-08-08-debian-codesearch-latency/)
* [Debian Code Search: positional index, TurboPFor-compressed (2019)](https://michael.stapelberg.ch/posts/2019-09-29-dcs-positional-turbopfor-index/)
  * [TurboPFor: an analysis (2019)](https://michael.stapelberg.ch/posts/2019-02-05-turbopfor-analysis/)
* [Debian Code Search: OpenAPI now available (2021)](https://michael.stapelberg.ch/posts/2021-03-06-debian-code-search-openapi/)
* [Debian Code Search: Fast TurboPFor with Go SIMD (2026)](https://michael.stapelberg.ch/posts/2026-09-06-dcs-fast-turbopfor-go-simd/)

Documentation:

* [Setting up a small instance of DCS for hacking](https://github.com/Debian/dcs/blob/main/howto/hacking.md)
* [Debian Code Search Index format](https://github.com/Debian/dcs/blob/main/howto/index.md)

Hardware requirements:

* At least 1 TB of SSD disk space.
* The more CPU, the better. Then, more IOPS (disks that saturate many reads).

Example: Google Cloud
* A Google Cloud N2 instance (`n2-standard-16`) has 64 GB of RAM and a Cascade Lake CPU (with AVX512), at least in my test.
  * Local NVMe SSDs are recommended. You need at least four 375 GB disks.
  * Performance ceiling (`fio`, 16K random reads): 180k IOPS, 2.95 GB/s
  * DCS needs 4.6 ns CPU time per byte searched, about 0.22 GB/s per vCPU.
  * 32 vCPUs are the recommendation if you want fastest queries. 16 vCPUs is fine, too.
  * 32 GB of RAM, or 64 GB of RAM (better) are recommended.
    * 128 GB of RAM holds the typical working set and can deliver 2x faster (warm) queries.

Example: Hetzner dedicated server
* A Hetzner AX52 (no longer available to order) has 64 GB of RAM, a Ryzen 7 7700 (8 cores / 16 threads) and two 1 TB Samsung PM9A1 NVMe disks.
  * Performance ceiling (`fio`, 16K random reads): 400k IOPS, 6.5 GB/s (RAID0)
  * DCS needs 2.7 ns CPU time per byte searched, about 0.37 GB/s per Zen 4 thread.
  * This machine is CPU-bound: To saturate the disk, we’d need a 16 or 32 core CPU.
  * With 64 GB of RAM, about 44% of searched bytes came from page cache.

Guide to this repository:

* `cmd/dcs` is the swiss-army knife tool for Debian Code Search, displaying index files in a variety of ways.
* `cmd/dcs-localdcs` runs a local development instance of Debian Code Search.

The service itself consists of the following services / jobs:

* `cmd/dcs-compute-ranking` generates `ranking.json` based on the Debian Popularity Contest (popcon).
* `cmd/dcs-feeder` feeds packages to multiple `dcs-package-importer` shards.
* `cmd/dcs-web` is the web server which runs searches on multiple `dcs-source-backend` shards.

Each shard (codesearch.debian.net uses 6 shards) runs:

* A `cmd/dcs-package-importer` to receive, unpack and index Debian packages for this shard.
* A `cmd/dcs-source-backend` to run actual searches over imported source code.
