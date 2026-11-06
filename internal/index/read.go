package index

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math/bits"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"

	"github.com/Debian/dcs/internal/mmap"
	"github.com/Debian/dcs/internal/turbopfor/pfordec"
	"golang.org/x/sync/errgroup"
	"golang.org/x/sys/unix"
)

var errNotFound = errors.New("not found")

type DocidReader struct {
	f           *mmap.File
	indexOffset uint32
	Count       int
}

func newDocidReader(dir string) (*DocidReader, error) {
	f, err := mmap.Open(filepath.Join(dir, "docid.map"))
	if err != nil {
		return nil, err
	}
	indexOffset := binary.LittleEndian.Uint32(f.Data[len(f.Data)-4:])
	return &DocidReader{
		f:           f,
		indexOffset: indexOffset,
		Count:       int(uint32(len(f.Data))-indexOffset-4) / 4,
	}, nil
}

func (dr *DocidReader) Close() error {
	return dr.f.Close()
}

func (dr *DocidReader) All() io.Reader {
	return bytes.NewReader(dr.f.Data[:dr.indexOffset])
}

func (dr *DocidReader) Lookup(docid uint32) (string, error) {
	offset := int64(dr.indexOffset + (docid * 4))
	if offset >= int64(len(dr.f.Data)-4) {
		return "", fmt.Errorf("docid %d outside of docid map [0, %d)", docid, (int64(len(dr.f.Data)-4)-int64(dr.indexOffset))/4)
	}
	// Locate docid file name offset:
	offsets := struct {
		String uint32
		Next   uint32 // next string location or (for the last entry) index location
	}{
		String: binary.LittleEndian.Uint32(dr.f.Data[offset:]),
		Next:   binary.LittleEndian.Uint32(dr.f.Data[offset+4:]),
	}

	// Read docid file name:
	l := int(offsets.Next - offsets.String - 1)
	fn := string(dr.f.Data[int(offsets.String) : int(offsets.String)+l])
	return fn, nil
}

type reusableBuffer struct {
	u  []uint32
	bd pfordec.BlockDecoder
}

type bufferPair struct {
	docid *reusableBuffer
	pos   *reusableBuffer
}

func newBufferPair() *bufferPair {
	return &bufferPair{
		docid: &reusableBuffer{},
		pos:   &reusableBuffer{},
	}
}

type PForReader struct {
	// The dir field is unused in the code, but incredibly useful to keep around
	// when looking at crash dumps in a debugger.
	dir string

	meta *mmap.File
	data *mmap.File

	//deltabuf []uint32
}

func newPForReader(dir, section string) (*PForReader, error) {
	sr := PForReader{
		dir: dir,
	}
	var err error
	if sr.meta, err = mmap.Open(filepath.Join(dir, "posting."+section+".meta")); err != nil {
		return nil, err
	}

	if sr.data, err = mmap.Open(filepath.Join(dir, "posting."+section+".turbopfor")); err != nil {
		sr.meta.Close()
		return nil, err
	}
	return &sr, nil
}

// Merging doesn’t call this, search does.
// Calls madvise(2) for search’s disk access pattern.
func (sr *PForReader) adviseForSearch() {
	if len(sr.meta.Data) > 0 {
		unix.Madvise(sr.meta.Data, unix.MADV_RANDOM)
	}
	if len(sr.data.Data) > 0 {
		unix.Madvise(sr.data.Data, unix.MADV_SEQUENTIAL)
	}
}

func (sr *PForReader) Close() error {
	if err := sr.meta.Close(); err != nil {
		return err
	}
	if err := sr.data.Close(); err != nil {
		return err
	}
	return nil
}

func (sr *PForReader) metaEntry(trigram Trigram) (*MetaEntry, *MetaEntry, error) {
	num := len(sr.meta.Data) / metaEntrySize
	d := sr.meta.Data
	n := sort.Search(num, func(i int) bool {
		// MetaEntry.Trigram is the first member
		return Trigram(binary.LittleEndian.Uint32(d[i*metaEntrySize:])) >= trigram
	})
	if n >= num {
		return nil, nil, errNotFound
	}
	var result MetaEntry
	result.Unmarshal(d[n*metaEntrySize:])
	if result.Trigram != trigram {
		return nil, nil, errNotFound
	}

	var next MetaEntry
	if n < num-1 {
		next.Unmarshal(d[(n+1)*metaEntrySize:])
	} else {
		next.OffsetData = int64(len(sr.data.Data))
	}
	return &result, &next, nil
}

func (sr *PForReader) metaEntry1(dest *MetaEntry, trigram Trigram) (found bool) {
	num := len(sr.meta.Data) / metaEntrySize
	d := sr.meta.Data
	n := sort.Search(num, func(i int) bool {
		// MetaEntry.Trigram is the first member
		return Trigram(binary.LittleEndian.Uint32(d[i*metaEntrySize:])) >= trigram
	})
	if n >= num {
		return false
	}
	dest.Unmarshal(d[n*metaEntrySize:])
	if dest.Trigram != trigram {
		return false
	}
	return true
}

func (sr *PForReader) metaEntryAt(dest *MetaEntry, i int) (found bool) {
	d := sr.meta.Data
	if num := len(d) / metaEntrySize; i >= num {
		return false
	}
	dest.Unmarshal(d[i*metaEntrySize:])
	return true
}

func (sr *PForReader) MetaEntry(trigram Trigram) (*MetaEntry, error) {
	e, _, err := sr.metaEntry(trigram)
	return e, err
}

// Streams returns a reader for the specified trigram data.
func (sr *PForReader) Data(t Trigram) (data io.Reader, entries int, _ error) {
	meta, next, err := sr.metaEntry(t)
	if err != nil {
		return nil, 0, err
	}
	dataBytes := next.OffsetData - meta.OffsetData
	//log.Printf("offset: %d, bytes: %d", meta.OffsetData, dataBytes)
	// TODO: benchmark whether an *os.File with Seek is measurably worse
	return bytes.NewReader(sr.data.Data[meta.OffsetData : meta.OffsetData+dataBytes]),
		int(meta.Entries),
		nil
}

func (sr *PForReader) deltas(meta *MetaEntry, buffer *reusableBuffer) ([]uint32, error) {
	entries := int(meta.Entries)
	d := sr.data.Data

	if entries > cap(buffer.u) {
		buffer.u = make([]uint32, 0, entries)
	}
	buffer.bd.DecodeN(d[meta.OffsetData:], buffer.u[:entries])
	return buffer.u[:entries], nil
}

func (sr *PForReader) deltasWithBuffer(t Trigram, buffer *reusableBuffer) ([]uint32, error) {
	var meta MetaEntry
	if found := sr.metaEntry1(&meta, t); !found {
		return nil, errNotFound
	}
	return sr.deltas(&meta, buffer)
}

func (sr *PForReader) Deltas(t Trigram) ([]uint32, error) {
	meta, _, err := sr.metaEntry(t)
	if err != nil {
		return nil, err
	}
	return sr.deltas(meta, &reusableBuffer{})
}

// A DeltaReader reads up to 256 deltas at a time (i.e. one TurboPFor block),
// which is useful for copying index data in a windowed fashion (for merging).
type DeltaReader struct {
	entries int
	n       int
	data    []byte
	sd      pfordec.StreamDecoder
}

func NewDeltaReader() *DeltaReader {
	return &DeltaReader{}
}

// Reset positions the reader on a posting list.
func (dr *DeltaReader) Reset(meta *MetaEntry, data []byte) {
	dr.entries = int(meta.Entries)
	dr.n = 0
	dr.data = data[meta.OffsetData:]
}

// Read returns up to 256 uint32 deltas.
//
// When all deltas have been read, Read returns nil.
//
// The first Read call after Reset returns a non-nil result.
func (dr *DeltaReader) Read() []uint32 {
	if dr.n+256 <= dr.entries {
		vals, read := dr.sd.DecodeBlock(dr.data, 256)
		dr.data = dr.data[read:]
		dr.n += 256
		return vals
	}
	if remaining := dr.entries - dr.n; remaining > 0 {
		vals, read := dr.sd.DecodeBlock(dr.data, remaining)
		dr.data = dr.data[read:]
		dr.n += remaining
		return vals
	}
	return nil
}

type PosrelReader struct {
	meta *mmap.File
	data *mmap.File
}

func newPosrelReader(dir string) (*PosrelReader, error) {
	var pr PosrelReader
	var err error
	if pr.meta, err = mmap.Open(filepath.Join(dir, "posting.posrel.meta")); err != nil {
		return nil, err
	}
	if pr.data, err = mmap.Open(filepath.Join(dir, "posting.posrel.data")); err != nil {
		return nil, err
	}
	return &pr, nil
}

func (pr *PosrelReader) metaEntry1(dest *MetaEntry, trigram Trigram) (found bool) {
	// TODO: maybe de-duplicate with PForReader.metaEntry?

	num := len(pr.meta.Data) / metaEntrySize
	d := pr.meta.Data
	n := sort.Search(num, func(i int) bool {
		// MetaEntry.Trigram is the first member
		return Trigram(binary.LittleEndian.Uint32(d[i*metaEntrySize:])) >= trigram
	})
	if n >= num {
		return false
	}
	dest.Unmarshal(d[n*metaEntrySize:])
	if dest.Trigram != trigram {
		return false
	}

	return true
}

func (pr *PosrelReader) MetaEntry(trigram Trigram) (*MetaEntry, error) {
	var meta MetaEntry
	found := pr.metaEntry1(&meta, trigram)
	if !found {
		return nil, errNotFound
	}
	return &meta, nil
}

func (pr *PosrelReader) DataBytes(t Trigram) ([]byte, error) {
	var meta MetaEntry
	if found := pr.metaEntry1(&meta, t); !found {
		return nil, errNotFound
	}
	return pr.data.Data[meta.OffsetData:], nil
}

func (pr *PosrelReader) Close() error {
	if err := pr.meta.Close(); err != nil {
		return err
	}
	if err := pr.data.Close(); err != nil {
		return err
	}
	return nil
}

type Index struct {
	dir string

	DocidMap *DocidReader  // docid → filename mapping
	Docid    *PForReader   // docids for all trigrams
	Pos      *PForReader   // positions for all trigrams
	Posrel   *PosrelReader // position relationships for all trigrams

	// refs counts how many queries are currently using the index.
	// refs starts at 1 and calling Delete() decreases it,
	// which will either immediately delete the index (no queries)
	// or delete the index once all queries on the index are done.
	refs atomic.Int32
}

func Open(dir string) (*Index, error) {
	// Evaluate symlinks so that the directory path we store remains valid
	// (symlinks are replaced as the index files are replaced).
	var err error
	dir, err = filepath.EvalSymlinks(dir)
	if err != nil {
		return nil, err
	}

	i := Index{
		dir: dir,
	}
	i.refs.Store(1)
	if i.DocidMap, err = newDocidReader(dir); err != nil {
		return nil, err
	}

	// posrel reduces the index size by about ≈ 1/4!
	if i.Posrel, err = newPosrelReader(dir); err != nil {
		return nil, err
	}

	if i.Docid, err = newPForReader(dir, "docid"); err != nil {
		return nil, err
	}
	if i.Pos, err = newPForReader(dir, "pos"); err != nil {
		return nil, err
	}
	i.Docid.adviseForSearch()
	i.Pos.adviseForSearch()

	return &i, nil
}

func (i *Index) CloseWhenUnused() {
	if i.refs.Add(-1) > 0 {
		return // queries still in progress
	}
	i.Close()
}

func (i *Index) Use() (release func()) {
	i.refs.Add(1)
	return func() {
		if i.refs.Add(-1) > 0 {
			return // CloseWhenUnused() was not yet called
		}
		i.Close()
	}
}

type Match struct {
	Docid    uint32
	Position uint32 // byte offset of the trigram within the document
}

func (i *Index) Matches(t Trigram) ([]Match, error) {
	return i.matchesWithBuffer(t, newBufferPair())
}

func (i *Index) matchesWithBufferDirect(t Trigram, buffers *bufferPair) (docids []uint32, pos []uint32, posrel []byte, _ error) {
	// mu.Lock()
	// defer mu.Unlock()
	var eg errgroup.Group

	eg.Go(func() error {
		var err error
		docids, err = i.Docid.deltasWithBuffer(t, buffers.docid)
		return err
	})
	eg.Go(func() error {
		var err error
		pos, err = i.Pos.deltasWithBuffer(t, buffers.pos)
		return err
	})
	eg.Go(func() error {
		var err error
		posrel, err = i.Posrel.DataBytes(t)
		return err
	})

	if err := eg.Wait(); err != nil {
		return nil, nil, nil, err
	}
	return docids, pos, posrel, nil
}

// positional decodes the docids and returns the posrel stream
// for the specified trigram, and resets the pos DeltaReader
// such that it reads (= decodes) positions for the trigram.
func (i *Index) positional(t Trigram, docidBuf *reusableBuffer, pos *DeltaReader) (docids []uint32, posrel []byte, _ error) {
	var meta MetaEntry
	if found := i.Pos.metaEntry1(&meta, t); !found {
		return nil, nil, errNotFound
	}

	pos.Reset(&meta, i.Pos.data.Data)

	docids, err := i.Docid.deltasWithBuffer(t, docidBuf)
	if err != nil {
		return nil, nil, err
	}
	posrel, err = i.Posrel.DataBytes(t)
	if err != nil {
		return nil, nil, err
	}
	return docids, posrel, nil
}

func (i *Index) matchesWithBuffer(t Trigram, buffers *bufferPair) ([]Match, error) {
	docids, pos, posrel, err := i.matchesWithBufferDirect(t, buffers)
	if err != nil {
		return nil, err
	}
	matches := make([]Match, 0, len(pos))
	docidIdx := -1
	var prevD, prevP uint32
	for i := 0; i < len(pos); {
		// should be 1 if the docid changes, 0 otherwise
		// TODO: micro-benchmark the “read uint64s, use bits.TrailingZeros64(), mask u &= u-1” trick
		pr := posrel[i/8]
		rest := min(len(pos)-i, 8)
		for range rest {
			// TODO: exchange (uint(i) % 8) with j?
			chg := int((pr >> (uint(i) % 8)) & 1)
			docidIdx += chg
			prevP *= uint32(1 ^ chg)

			prevD += docids[docidIdx] * uint32(chg)
			prevP += pos[i]
			matches = append(matches, Match{
				Docid:    prevD,
				Position: prevP,
			})
			i++
		}
	}
	return matches, nil
}

type planEntry struct {
	offset  int
	t       Trigram
	entries uint32
}

func (i *Index) bestTrigrams(query string) (*planEntry, *planEntry, error) {
	qb := []byte(query)
	readMeta := func(j int) (planEntry, error) {
		t := Trigram(uint32(qb[j])<<16 |
			uint32(qb[j+1])<<8 |
			uint32(qb[j+2]))
		meta, _, err := i.Pos.metaEntry(t)
		if err != nil {
			return planEntry{}, err
		}
		return planEntry{
			offset:  j,
			t:       t,
			entries: meta.Entries,
		}, nil
	}

	if len(query) <= 6 {
		// Fast path: For literal queries shorter than 7 bytes,
		// there are only two non-overlapping trigrams:
		// Selecting the first and last trigram is optimal.
		first, err := readMeta(0)
		if err != nil {
			return nil, nil, err
		}

		// For a query that consists of 3 bytes only,
		// last will be the same trigram as first,
		// at distance 0 (harmless, yields correct results).
		last, err := readMeta(len(query) - 3)
		if err != nil {
			return nil, nil, err
		}

		return &first, &last, nil
	}

	// Find the pair of trigrams which yield the highest selectivity
	// (i.e. discards most false-positive candidates),
	// yet have the fewest docs/positions (entries)
	// (to minimize decoding cost).
	//
	// Selectivity can be maximized by choosing trigrams
	// which are far apart in the query. The closer they are,
	// the more often they appear together in candidate files.
	//
	// The upper bound on the distance is choosing the first
	// and last trigram in the search query,
	// but that is not sufficient to guarantee selectivity:
	// Many search queries start with terms that have common
	// beginnings and ends, and differ only in the middle.
	//
	// The lower bound on the distance is 1 or 2,
	// which results in overlapping trigrams:
	// Two overlapping 3-grams constrain about as much as one 4-gram.
	// In practice, a distance of 3 is the minimum,
	// and a distance of 1/3rd the query’s length benchmarks best.
	plan := make([]planEntry, len(query)-2)
	for j := range plan {
		var err error
		// readMeta does a binary search, i.e. random reads
		// which can take a few milliseconds for long queries.
		// We could do all readMeta concurrently.
		plan[j], err = readMeta(j)
		if err != nil {
			// err is errNotFound. The query cannot match
			// if any of its trigrams are entirely absent.
			return nil, nil, err
		}
	}

	gap := max(3, len(query)/3) // 1/3rd the query length, but at least 3.
	first, last := &plan[0], &plan[len(plan)-1]
	cheapestLeft := &plan[0]
	// To find the best pair (left + right),
	// this loop varies right (plan[k])
	// and considers every left (plan[k-gap]).
	for k := gap; k < len(plan); k++ {
		// See if this left trigram is cheaper.
		if left := plan[k-gap]; left.entries < cheapestLeft.entries {
			cheapestLeft = &plan[k-gap]
		}
		// Independently, see if this pair is cheapest overall.
		if cheapestLeft.entries+plan[k].entries < first.entries+last.entries {
			first, last = cheapestLeft, &plan[k]
		}
	}
	return first, last, nil
}

func (i *Index) QueryPositional(query string) ([]Match, error) {
	if len(query) < 3 {
		return nil, nil // too short: a trigram needs 3 characters
	}

	first, last, err := i.bestTrigrams(query)
	if err != nil {
		if errors.Is(err, errNotFound) {
			return nil, nil // no matches, not an error
		}
		return nil, err
	}

	var eg errgroup.Group

	var (
		fdocids []uint32
		fpos    = NewDeltaReader()
		fposrel []byte

		ldocids []uint32
		lpos    = NewDeltaReader()
		lposrel []byte
	)

	eg.Go(func() error {
		var err error
		fdocids, fposrel, err = i.positional(first.t, &reusableBuffer{}, fpos)
		return err
	})

	eg.Go(func() error {
		var err error
		ldocids, lposrel, err = i.positional(last.t, &reusableBuffer{}, lpos)
		return err
	})

	if err := eg.Wait(); err != nil {
		return nil, err
	}

	// A match of the query at byte offset off
	// has first’s trigram at off+first.offset
	// and last’s trigram at off+last.offset,
	// i.e. last comes exactly delta bytes after first.
	delta := last.offset - first.offset
	var entries []Match

	var (
		fdocidIdx = -1
		fprevD    uint32
		fprevP    uint32

		ldocidIdx = -1
		lprevD    uint32
		lprevP    uint32
	)

	var (
		fblock, lblock []uint32
		lblocks        int // number of blocks read from lpos
	)

	var j int // not reset to skip already-inspected parts of last
	llpos := lpos.entries
	jInc := func(add int) {
		j += add
		if j >= llpos {
			return
		}
		if ((lposrel[j/8] >> (uint(j) % 8)) & 1) == 1 {
			ldocidIdx++
			lprevD += ldocids[ldocidIdx]
			lprevP = 0
		}

		// Seek to the right TurboPFor block if needed.
		for ; lblocks <= j/256; lblocks++ {
			lblock = lpos.Read()
		}
		lprevP += lblock[j%256]
	}
	jInc(0)
	for i := 0; i < fpos.entries; i++ {
		if i%256 == 0 {
			fblock = fpos.Read() // decode another block
		}
		if ((fposrel[i/8] >> (uint(i) % 8)) & 1) == 1 {
			fdocidIdx++
			fprevD += fdocids[fdocidIdx]
			fprevP = 0
		}
		fprevP += fblock[i%256]

		docid := fprevD
		pos := uint32(int(fprevP) + delta)
		for j+1 < llpos && lprevD < docid {
			// Skip pos entries until posrel contains a 1 (i.e. docid change):
			jInc(1 + bits.TrailingZeros16((uint16(lposrel[(j+1)/8])|0xFF00)>>(uint((j+1))%8)))
		}

		for ; j < llpos && lprevD == docid; jInc(1) {
			// TODO: support regexp queries by using greater-than comparison instead of equals
			if lprevP < pos {
				continue
			}
			if lprevP == pos {
				if start := int(fprevP) - first.offset; start >= 0 {
					entries = append(entries, Match{
						Docid:    fprevD,
						Position: uint32(start),
					})
				}
			}
			break
		}
	}
	//log.Printf("len(entries) = %d", len(entries))
	return entries, nil
}

func (i *Index) Close() error {
	if i.DocidMap != nil {
		if err := i.DocidMap.Close(); err != nil {
			return err
		}
	}
	if i.Docid != nil {
		if err := i.Docid.Close(); err != nil {
			return err
		}
	}
	if i.Pos != nil {
		if err := i.Pos.Close(); err != nil {
			return err
		}
	}
	if err := i.Posrel.Close(); err != nil {
		return err
	}
	return nil
}

// ReadPackageNames returns the names (pkg_ver)
// of all packages which are indexed in dir.
func ReadPackageNames(dir string) (map[string]bool, error) {
	docidMap, err := newDocidReader(dir)
	if err != nil {
		return nil, err
	}
	defer docidMap.Close()
	scanner := bufio.NewScanner(docidMap.All())
	pkgs := make(map[string]bool)
	for scanner.Scan() {
		name, _, _ := strings.Cut(scanner.Text(), "/")
		pkgs[name] = true
	}
	return pkgs, scanner.Err()
}
