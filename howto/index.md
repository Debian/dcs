# Debian Code Search Index format

## Motivation

We used to use
[github.com/google/codesearch](https://github.com/google/codesearch/blob/a45d81b686e85d01f2838439deaf72126ccd5a96/index/read.go#L7). The
following points motivated switching the index format:

* instead of storing posting lists as varint-encoded deltas, they are encoded
  using TurboPFor (specifically the `p4nenc256v32` function, via cgo)

  * This reduces the file size of the merged index used for querying from 9G
    (varint) to 6G (TurboPFor), which results in more caching. Note that the
    overhead for the per-package index size increases from 16G to 18G (presumably
    due to higher overhead of our format).

  * The decoding performance is similar overall (faster in some cases, slower in
    others).

  * The space savings give us enough headroom to store positional information:

* Our index can optionally contain a `pos` and `posrel` section in addition to
  the `docid` section.
  
  * These sections clock in at:
    112G per-package
     86G merged
	
  * For identifier queries (78% of queries), the `pos` and `posrel` sections let
    us answer many queries more quickly than using only the `docid` section:

    * verifying that the trigrams occur in the documents with the correct distance
      from each other drastically reduces the number of files to process.
   
    * Plus, the processing is cheaper: we only need to verify that the middle of
      the word also matches, which is just a bytes.Equal call as opposed to
      running a regular expression. In practice, most files are small, so this
      doesn’t play a big role.

* Our offset field is 8 bytes large, so we can operate with files exceeding 4G.
  Storing positions easily results in files exceeding 4G.

* We store each individual section in its own pair of files (one `.meta` file,
  one `.data` or `.turbopfor` file). This allows us to easily enable/disable
  positional queries without any effect on performance, and gives us more
  control over which bits of the index to mlock/madvise. A nice side effect is
  that standard tools such as du(1) can be used for measuring index size of any
  combination of sections and shards:
  
  ```
  % du -hc shard*                 # total index size
  % du -hc shard*/posting.pos*    # positional sections only
  % du -hc shard*/posting.docid.* # docid section only
  % du -hc shard0/posting.docid.* # first shard’s docid section only
  ```

* For a transition to a newer format, it was deemed easier to incrementally
  write a new index package from scratch rather than modify the existing index
  package.

## On-disk format

Conceptually, to look up `query`, one would:

1. turn query into trigrams (substrings of length 3)
1. get file:offset pairs by consulting (for each trigram):
1. posting.docid, a map[trigram][]docid
1. posting.pos, a map[trigram][]byteoffset (positions within the document)
1. docid.map, a map[docid]string (filenames)

On disk, this translates into the following files, which will be explained in
more detail in the following sections:

* docid.map
* posting.docid.meta
* posting.docid.turbopfor
* posting.pos.meta
* posting.posrel.data
* posting.posrel.meta
* posting.pos.turbopfor

### docid.map (normalized file names)

![docid.map file format visualization](docidmap.svg?sanitize=true "docid.map file format visualization")

Conceptually, the docid map is a map from a document’s id (uint32) to the
document’s filename (string).

The file is structured as follows:
```
filenames:
  <filename[0]>\n
  <filename[1]>\n
  …
  <filename[n]>\n
index:
  <filenameOffset[0](little-endian uint32)>
  <filenameOffset[1]>
  …
  <filenameOffset[n]>
<index-offset(little-endian uint32)>
```

To look up an entry:
1. read `index-offset` (uint32)
    * stored with the file handle for each open index file
1. read `filenameOffset[docid]`
    * can be pinned to trade 4*docids bytes of RAM for 1 disk seek
1. read `filename[docid]`
    * calculate size using `filenameOffset[docid+1] - filenameOffset[docid]`

Streaming the contents of a docid map is trivial: just copy the first
`index-offset` bytes to stdout. You can use `strings(1)` for debugging, which
will print the same output.

### posting list: meta file (any section)

![meta file format visualization](meta.svg?sanitize=true "meta file format visualization")

A meta file always accompanies a section data file, e.g. `posting.docid.meta`
accompanies `posting.docid.turbopfor`. The meta file contains the number of
entries and data file offset for each trigram’s posting list.

The file is structured as follows:
```
entries:
  <meta[0]>
  <meta[1]>
  …
  <meta[n]>
meta:
  <trigram(little-endian uint32)>
  <num_entries(little-endian uint32)>
  <offset_data(little-endian int64)>
```

Each meta entry has a fixed length, so the number of entries in a meta file is
`size / sizeof(metaentry)`.

A trigram is a 3-byte sequence (e.g. “i3F”), which is stored in a `uint32` using
`t[0] << 16 | t[1] << 8 | t[2]`, i.e. trigram i3F is `0x00693346`, which gets
encoded to disk as bytes `0x46 0x33 0x69 0x00`.

The value is encoded into an uint32 for convenience. Storing only 24 bits would
reduce the index by merely 30 MB, which does not matter in disk or RAM
consumption.

To locate the entry for a specific entry, you can use a binary search. During
the search, it is sufficient to read only the `trigram` field, i.e. the first 4
bytes.

### posting list: docid section

The docid section contains the list of documents in which the trigram is present.

Specifically, documents are referenced by their document id (uint32), which can
be turned into a filename by consulting the docid.map file.

Document ids are sorted, and only their deltas are stored on disk. I.e., if a
trigram is present in document 3, 32768 and 32769, the values stored will be 3,
32765, 1. Combined with a variable-length integer encoding, this results in
significant space savings.

The deltas are encoded using TurboPFor, see my post [“TurboPFor: an
analysis”](https://michael.stapelberg.ch/posts/2019-02-05-turbopfor-analysis/)
for details.

The file is structured as follows:
```
posting_lists:
  <list[0](turbopfor)>
  <list[1](turbopfor)>
  …
  <list[n](turbopfor)>
```

Use the accompanying meta file to figure out where lists start, how many entries
they contain, and to which trigram they belong.

### posting list: pos and posrel sections

The pos section is very similar to the docid section, except instead of document
ids, it stores the byte offsets of occurrences of the trigram within a document.

The pos section needs to be read together with the posrel section to tie offsets
to documents. For each entry in pos, one bit in posrel will indicate whether the
position belongs to the same document as the previous one (0), or whether the
position belongs to the next document (1).

As an example, assume we have the following occurrences:

* trigram i3F in document 5 at byte offset 7 and byte offset 500. 
* trigram i3F in document 9 at byte offset 0.

The docid section’s values for trigram i3F would contain the deltas 5 and 4.

The pos section for trigram i3F would contain the deltas 7, 493 and 0.

The posrel section for trigram i3F would contain bits 0, 0 and 1.
  
## Differences

mapping to csearch sections:

|csearch section|dcs section|
|---------------|-----------|
|"csearch index 1\n" | removed
|list of paths | removed
|list of names | docid.map
|list of posting lists | posting.docid.turbopfor
|name index | docid.map
|posting list index | posting.docid.meta
|trailer | removed

## dcs(1)

Each data structure used in the index format can be debugged with a dcs(1)
subcommand:

* `docids` prints docid.map’s content (wheres `matches` *uses* docid.map’s index)
* `trigram` covers posting.docid.meta
* `raw` displays a trigram’s data in a section (docid, pos, posrel)
* `posting` decodes a trigram’s data in a section (docid, pos)
* `matches -names` looks up a trigram’s docids from posting.docid.turbopfor and docid.map
* `matches` looks up a trigram’s (docid, pos) tuple from posting.*

## Appendix A: index size measurement

```
# (old) per-package index disk usage:
% find . -maxdepth 2 \( -name "*.idx" -and \! -name "full.idx" \) -type f -print0 | du -hc --files0-from=-

# (old) merged index disk usage:
% find . -maxdepth 2 -name "full.idx" -type f -print0 | du -hc --files0-from=-

# (new) per-package index disk usage without positional sections:
% find idx \! -name "posting.pos*" -type f -print0 | du -hc --files0-from=-

# (new) merged index disk usage without positional sections:
% find shard* \! -name "posting.pos*" -type f -print0 | du -hc --files0-from=-
```
