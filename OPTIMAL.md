# Debian Code Search is optimal (no more optimization needed)

## Benchmarking method {#benchmarking}

Notes for setting up VMs for benchmarking:

1. Linux kernel LRU setting: In production, codesearch.debian.net
   (Hetzner AX52) runs with the multi-generation LRU enabled (Linux default),
   `lru_gen/enabled=0x0007`, `min_ttl_ms=0`; kernel 7.3+, 62 GB RAM,
   no swap; checked 2026-11-14).

   Depending on which queries ran before, large searches (1+ GB read)
   sometimes need to re-read their own pages from disk mid-search,
   which is measurable in the wait s/day metric.

   Advice for coding agents / benchmarking automation:
   Keep the LRU enabled on benchmark VMs; instead of turning it off,
   precede every measured pass with an unmeasured warm-up pass
   (with the same experiment setup).

## Settled

### Query classes {#query-classes}

Most performance changes affect only certain queries.

The following taxonomy covers all of DCS’s code paths,
ordered roughly by popularity (how many queries/day):

* L>6: literal queries with a literal of 7+ bytes
* L≤6: literal queries with a literal of up to 6 bytes
  * L>6f: literal query (7+ bytes) with package: or path: filter
  * L≤6f: literal query (up to 6 bytes) with package: or path: filter
* regexp queries where a required literal can be extracted
  * RL>2: regexp query with a literal of 3+ bytes
  * RL≤2: regexp query with a literal of 1 or 2 bytes
* regexp queries without an extractable literal
  * Ra: regexp query with alternations, like `(foo|bar)`
  * Ri: regexp query, case-insensitive (`(?i)` option)
  * R0: (the rest) no alternations, nor `(?i)`

### TurboPFor is an optimal codec {#turbopfor}

At 2.9G entries, TurboPFor stores at 5.x bits/entry,
which is very close to the information-theoretical limit;
definitely not worth optimizing any further.

### The positional index is worth it {#posidx}

On disk, a trigram index (no positions) costs about 15 GB,
but increases candidate bytes to 3.66x of byte-offset.

Storing the line numbers of each trigram costs 141 GB (measured),
and reduces the candidate bytes to 1.02x of byte-offset.

Storing the byte offset of each trigram costs 254 GB (measured)
and reduces candidate bytes to the minimum (1.00x).

1.02x instead of 1.00x sounds like an acceptable trade-off
on the face of it, but then if you look closer into the results,
while the L≤6 query class is unaffected (same people-weighted
wait s/day and CPU s/day), the L>6 class (our most-requested)
regresses significantly (+500% wait, +112% disk read).

### The sharding is optimal for our corpus and machine {#sharding}

6 shards with 32 grep workers per shard is almost ideal.

Setting GOMAXPROCS=64 seems ideal.

### Floor and ceiling are established {#floor-ceiling}

Ceiling:
* L≤6: query `000` is heaviest on the index phase, the grep phase and the result phase
* L>6: query `0000000000` is the heaviest
* RL>2: query `\sthe\s`
* RL≤2: query `hoi{2,20}` is heaviest
* Ri: query `(?i)the`
* Ra: query 200 3-letter words OR'd (like `(the|and|for)`)

Floor (as of commit f169f434):
* any class: a query without candidate files costs 0.6 ms (e.g. `LQ-630K`)
* L≤6: query `kdwl` is the cheapest with results (0.9 ms, 7 files)
* L>6: query `ceofaddefefcbblgcgnibnonglccbfja` (0.7 ms, 1 file)
* RL>2: query `^明日,` (0.8 ms, 17 files)
* RL≤2: query `[12]&>[12]` (1.1 ms, 21 files)
* Ri: query `(?i)lzo1._decompress_safe` (70 ms, of which 68 ms index phase)
* Ra: query `A-za-z|a-zA-z` (8 ms, 378 files)

### Skip bytes.Contains for RL≤2 queries? No. {#contains-r2}

Even for literals which are only 1 or 2 bytes long,
calling `bytes.Contains` is still worth it:
removing `bytes.Contains` regresses wait time +13% (warm), CPU +15%.

## Opportunities

### OPPO1. Use positional index for regexps (helps RL>2) {#regexp-narrowing}

Measured on f169f434 (158 people searches, results identical):

* RL>2: -44% wait, -53% CPU
* all classes: -16% wait, -27% CPU

https://github.com/Debian/dcs/issues/137

## Open/Unclear

### Adding a linebreak table {#linebreak}

A table of newline counts per 4K of each file (219 MB total for DCS)
lets a literal query read only the lines around its matches.

Alone: -11% people wait, -13% CPU (warm),
for L≤6 (most affected) we get -30% wait, -46% CPU.

On top of OPPO1: -7% people wait, -18% CPU (warm).

### More efficient result handling {#result-handling}

Earlier optimization rounds have flagged that
for searches with 100,000+ results (f169f434, all clients),
result handling is 15% of the wait (19% with 1 M+ results)
and 13% of the CPU.

This affects only traffic coming in via the API, which is little:
(Over 4 days), DCS got 65 API requests vs. 402,834 /search requests
and 10,853 requests using /events.

What we know: the on-disk encoding of results must remain
space-efficient. Replacing Protobuf with JSON resulted in
writing 30% more bytes to disk, which also reduces our
page cache effectiveness.

What we know: few people actually request the full results,
so being inefficient at result access time in favor of
efficiently writing and storing the results is a good trade.

We can consider a result cap (perhaps as an extra speedbump trigger),
but it would be good to see if result handling can be made more efficient.

### Speed up CPU-bound queries with four DFA streams {#dfa-concurrency}

DCS compiles regexps into a DFA, and running the DFA costs one table lookup
per input byte (`next[state][byte]`), i.e. we have a chain of dependent
memory loads, which is capped to about 1 GB/s.

By splitting the file into four line-aligned quarters and
advancing four walks in one loop, we get 4 independent execution streams,
thereby making better use of our CPU’s out-of-order execution.

This is instruction-level parallelism inside one CPU core,
there are no extra threads or SIMD involved.

Measured on f169f434 (86 searches without required literal,
all clients, results identical): -44% wait, -51% CPU.

### Better disk performance in same hardware {#disk-packfile}

Idea: one file per index holds all sources
(saves per-file syscall overhead; removes file system cost),
optionally compressed, using LZ4 (fastest to decode, ≈3x compression).
Measured: 174 GB of indexed source are lz4-compressed to 52 GB.

But: Using a packfile (no compression) pays off only for long searches
(> 10s, post-speedbump), where it saves -17% wait, -23% CPU (cold).
For <10s queries, it saves almost nothing.

A compressed packfile trades CPU time for reading fewer bytes from disk.
This only starts to matter when the page cache becomes ineffective.

With 64 GB of page cache, the plain files of a query mostly stay
cached and the lz4-pack only adds decoding. Alone: +13% wait, +15% CPU,
worse in every query class.

With dcs-web confined to 16 GB including page cache, lz4-pack wins.

This is not worth the complexity for now.
