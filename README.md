[![GitHub Actions CI](https://github.com/Debian/dcs/actions/workflows/go.yml/badge.svg)](https://github.com/Debian/dcs/actions/workflows/go.yml)

Please read http://codesearch.debian.net/research/bsc-thesis.pdf first!

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
