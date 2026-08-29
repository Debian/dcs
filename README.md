[![GitHub Actions CI](https://github.com/Debian/dcs/actions/workflows/go.yml/badge.svg)](https://github.com/Debian/dcs/actions/workflows/go.yml)

Context:

* [Debian Code Search: Bachelor Thesis (2012)](https://codesearch.debian.net/research/bsc-thesis.pdf)
* [Announcing the new Debian Code Search Instant (2014)](https://michael.stapelberg.ch/posts/2014-12-03-debian-code-search-instant/)
  * [Debian Code Search: taming the latency tail (2014)](https://michael.stapelberg.ch/posts/2014-12-23-code-search-taming-the-latency-tail/)
* [Debian Code Search: improving client-side latency (2016)](https://michael.stapelberg.ch/posts/2016-08-08-debian-codesearch-latency/)
* [Debian Code Search: positional index, TurboPFor-compressed (2019)](https://michael.stapelberg.ch/posts/2019-09-29-dcs-positional-turbopfor-index/)
  * [TurboPFor: an analysis (2019)](https://michael.stapelberg.ch/posts/2019-02-05-turbopfor-analysis/)
* [Debian Code Search: OpenAPI now available (2021)](https://michael.stapelberg.ch/posts/2021-03-06-debian-code-search-openapi/)

cmd/
    dcs-unpack - tool to unpack a debian source mirror
    dcs-index - tool to create an index from a debian source mirror
    compute-ranking - computes the ranking of each package/file
    dcs-web  - the code search web application itself
    index-backend - simple server which provides (a shard) of the index to dcs-web
    source-backend - simple server which provides the debian source to dcs-web

debian/
    The Debian packaging, which currently is very hacky due to Go packaging
    being hard in Debian currently. Patches welcome.

index/
    Copied from code.google.com/p/codesearch. Parts were re-written in
    hand-optimized C code (posting list decoding).

regexp/
    Copied from code.google.com/p/codesearch. Returns results in a data
    structure instead of printing them to stdout.

static/
    Static assets + HTML files (FAQ etc.)
