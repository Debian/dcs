[![GitHub Actions CI](https://github.com/Debian/dcs/actions/workflows/go.yml/badge.svg)](https://github.com/Debian/dcs/actions/workflows/go.yml)

Context:

* [Debian Code Search: Bachelor Thesis (2012)](https://codesearch.debian.net/research/bsc-thesis.pdf)
* [Announcing the new Debian Code Search Instant (2014)](https://michael.stapelberg.ch/posts/2014-12-03-debian-code-search-instant/)
  * [Debian Code Search: taming the latency tail (2014)](https://michael.stapelberg.ch/posts/2014-12-23-code-search-taming-the-latency-tail/)
* [Debian Code Search: improving client-side latency (2016)](https://michael.stapelberg.ch/posts/2016-08-08-debian-codesearch-latency/)
* [Debian Code Search: positional index, TurboPFor-compressed (2019)](https://michael.stapelberg.ch/posts/2019-09-29-dcs-positional-turbopfor-index/)
  * [TurboPFor: an analysis (2019)](https://michael.stapelberg.ch/posts/2019-02-05-turbopfor-analysis/)
* [Debian Code Search: OpenAPI now available (2021)](https://michael.stapelberg.ch/posts/2021-03-06-debian-code-search-openapi/)

Documentation:

* [Setting up a small instance of DCS for hacking](https://github.com/Debian/dcs/blob/main/howto/hacking.md)
* [Debian Code Search Index format](https://github.com/Debian/dcs/blob/main/howto/index.md)

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
