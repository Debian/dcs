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
	"sync"

	"github.com/Debian/dcs/internal/turbopfor"

	"golang.org/x/exp/mmap"
	"golang.org/x/sync/errgroup"
)

var errNotFound = errors.New("not found")

// mmapReader implements io.Reader for an mmap.ReaderAt.
type mmapReader struct {
	r   *mmap.ReaderAt
	off int64
}

// Read implements io.Reader.
func (mr *mmapReader) Read(p []byte) (n int, err error) {
	n, err = mr.r.ReadAt(p, mr.off)
	mr.off += int64(n)
	return n, err
}

type cachedLookup struct {
	docid uint32 // 0xFFFFFFFF is invalid, because 0 is a valid docid
	fn    string
}

type DocidReader struct {
	f           *mmap.ReaderAt
	indexOffset uint32
	Count       int
	last        cachedLookup
	buf         [4096]byte // TODO: document this is larger than PATH_MAX
}

func newDocidReader(dir string) (*DocidReader, error) {
	f, err := mmap.Open(filepath.Join(dir, "docid.map"))
	if err != nil {
		return nil, err
	}
	// Locate index offset:
	var b [4]byte
	if _, err := f.ReadAt(b[:], int64(f.Len()-4)); err != nil {
		return nil, err
	}
	indexOffset := binary.LittleEndian.Uint32(b[:])

	return &DocidReader{
		f:           f,
		indexOffset: indexOffset,
		Count:       int(uint32(f.Len())-indexOffset-4) / 4,
		last: cachedLookup{
			docid: 0xFFFFFFFF,
		},
	}, nil
}

func (dr *DocidReader) Close() error {
	return dr.f.Close()
}

func (dr *DocidReader) All() io.Reader {
	return &io.LimitedReader{
		R: &mmapReader{r: dr.f, off: 0},
		N: int64(dr.indexOffset),
	}
}

func (dr *DocidReader) Lookup(docid uint32) (string, error) {
	// memoizing the last entry suffices because posting lists are sorted by docid
	if dr.last.docid == docid {
		return dr.last.fn, nil
	}
	offset := int64(dr.indexOffset + (docid * 4))
	if offset >= int64(dr.f.Len()-4) {
		return "", fmt.Errorf("docid %d outside of docid map [0, %d)", docid, (int64(dr.f.Len()-4)-int64(dr.indexOffset))/4)
	}
	// Locate docid file name offset:
	if _, err := dr.f.ReadAt(dr.buf[:8], offset); err != nil {
		return "", err
	}
	offsets := struct {
		String uint32
		Next   uint32 // next string location or (for the last entry) index location
	}{
		String: binary.LittleEndian.Uint32(dr.buf[:4]),
		Next:   binary.LittleEndian.Uint32(dr.buf[4:8]),
	}

	// Read docid file name:
	l := int(offsets.Next - offsets.String - 1)
	if _, err := dr.f.ReadAt(dr.buf[:l], int64(offsets.String)); err != nil {
		return "", err
	}
	dr.last.docid = docid
	dr.last.fn = string(dr.buf[:l])
	return dr.last.fn, nil
}

type reusableBuffer struct {
	u []uint32
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
	meta *mmap.ReaderAt
	data *mmap.ReaderAt

	//deltabuf []uint32
}

func newPForReader(dir, section string) (*PForReader, error) {
	var sr PForReader
	var err error
	if sr.meta, err = mmap.Open(filepath.Join(dir, "posting."+section+".meta")); err != nil {
		return nil, err
	}
	if sr.data, err = mmap.Open(filepath.Join(dir, "posting."+section+".turbopfor")); err != nil {
		return nil, err
	}
	return &sr, nil
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
	num := sr.meta.Len() / metaEntrySize
	d := sr.meta.Data()
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
		next.OffsetData = int64(sr.data.Len())
	}
	return &result, &next, nil
}

func (sr *PForReader) metaEntry1(trigram Trigram) (*MetaEntry, error) {
	num := sr.meta.Len() / metaEntrySize
	d := sr.meta.Data()
	n := sort.Search(num, func(i int) bool {
		// MetaEntry.Trigram is the first member
		return Trigram(binary.LittleEndian.Uint32(d[i*metaEntrySize:])) >= trigram
	})
	if n >= num {
		return nil, errNotFound
	}
	var result MetaEntry
	result.Unmarshal(d[n*metaEntrySize:])
	if result.Trigram != trigram {
		return nil, errNotFound
	}
	return &result, nil
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
	return &io.LimitedReader{
			R: &mmapReader{r: sr.data, off: meta.OffsetData},
			N: dataBytes,
		},
		int(meta.Entries),
		nil
}

func (sr *PForReader) deltas(meta *MetaEntry, buffer *reusableBuffer) ([]uint32, error) {
	entries := int(meta.Entries)
	d := sr.data.Data()

	// DEBUG: pure-go verification
	// rd := NewDeltaReader()
	// rd.Reset(meta, d)
	// for rd.Read() != nil {
	// }

	//var deltas []uint32
	// TODO: figure out overhead. 128*1024 is wrong. might be 0, actually
	if n := entries + 128*1024; n > cap(buffer.u) {
		buffer.u = make([]uint32, 0, n)
	}
	//deltas := make([]uint32, entries, entries+128*1024)
	turbopfor.P4ndec256v32(d[meta.OffsetData:], buffer.u[:entries])
	return buffer.u[:entries], nil
}

func (sr *PForReader) deltasWithBuffer(t Trigram, buffer *reusableBuffer) ([]uint32, error) {
	meta, err := sr.metaEntry1(t)
	if err != nil {
		return nil, err
	}
	return sr.deltas(meta, buffer)
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
	buf     []uint32
}

func NewDeltaReader() *DeltaReader {
	return &DeltaReader{
		buf: make([]uint32, 256),
	}
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
		dr.data = dr.data[turbopfor.P4dec256v32(dr.data, dr.buf):]
		//dr.data = dr.data[goturbopfor.P4ndec256v32(dr.data, dr.buf):]
		dr.n += 256
		return dr.buf
	}
	if remaining := dr.entries - dr.n; remaining > 0 {
		turbopfor.P4dec32(dr.data, dr.buf[:remaining])
		//goturbopfor.P4dec32(dr.data, dr.buf[:remaining])
		dr.n += remaining
		return dr.buf[:remaining]
	}
	return nil
}

type PosrelReader struct {
	meta *mmap.ReaderAt
	data *mmap.ReaderAt
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

func (pr *PosrelReader) metaEntry(trigram Trigram) (*MetaEntry, *MetaEntry, error) {
	// TODO: maybe de-duplicate with PForReader.metaEntry?

	num := pr.meta.Len() / metaEntrySize
	d := pr.meta.Data()
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
		next.OffsetData = int64(pr.data.Len())
	}
	return &result, &next, nil
}

func (pr *PosrelReader) metaEntry1(trigram Trigram) (*MetaEntry, error) {
	// TODO: maybe de-duplicate with PForReader.metaEntry?

	num := pr.meta.Len() / metaEntrySize
	d := pr.meta.Data()
	n := sort.Search(num, func(i int) bool {
		// MetaEntry.Trigram is the first member
		return Trigram(binary.LittleEndian.Uint32(d[i*metaEntrySize:])) >= trigram
	})
	if n >= num {
		return nil, errNotFound
	}
	var result MetaEntry
	result.Unmarshal(d[n*metaEntrySize:])
	if result.Trigram != trigram {
		return nil, errNotFound
	}

	return &result, nil
}

func (pr *PosrelReader) MetaEntry(trigram Trigram) (*MetaEntry, error) {
	e, _, err := pr.metaEntry(trigram)
	return e, err
}

type mmapOffsetReader struct {
	r   *mmap.ReaderAt
	off int64
}

// Read implements io.ReaderAt.
func (mr *mmapOffsetReader) ReadAt(p []byte, off int64) (n int, err error) {
	return mr.r.ReadAt(p, mr.off+off)
}

// TODO: delete once raw.go uses DataBytes()
func (pr *PosrelReader) Data(t Trigram) (io.ReaderAt, error) {
	meta, _, err := pr.metaEntry(t)
	if err != nil {
		return nil, err
	}
	return &mmapOffsetReader{r: pr.data, off: meta.OffsetData}, nil
}

func (pr *PosrelReader) DataBytes(t Trigram) ([]byte, error) {
	meta, err := pr.metaEntry1(t)
	if err != nil {
		return nil, err
	}
	return pr.data.Data()[meta.OffsetData:], nil
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
	DocidMap *DocidReader  // docid → filename mapping
	Docid    *PForReader   // docids for all trigrams
	Pos      *PForReader   // positions for all trigrams
	Posrel   *PosrelReader // position relationships for all trigrams

	// buffers for both i.Matches() calls
	firstBuffer *bufferPair
	lastBuffer  *bufferPair
}

func Open(dir string) (*Index, error) {
	var i Index
	i.firstBuffer = newBufferPair()
	i.lastBuffer = newBufferPair()
	var err error
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

	return &i, nil
}

type Match struct {
	Docid    uint32
	Position uint32 // byte offset of the trigram within the document
}

var mu sync.Mutex

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
		rest := len(pos) - i
		if rest > 8 {
			rest = 8
		}
		for j := 0; j < rest; j++ {
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

// FiveLines returns five \n-separated lines surrounding pos. The first two
// lines are context above the line containing pos (which is always element
// [2]), the last two lines are context above that line.
func FiveLines(b []byte, pos int) [5]string {
	//fmt.Printf("FiveLines(%q, %d)", string(b), pos)
	var five [5]string
	prev := pos
	start := 2 // no before lines
	if prev > 0 {
		// start of line of match
		if idx := bytes.LastIndexByte(b[:prev], '\n'); idx != -1 {
			prev = idx + 1
		}
		// first context line
		if idx := bytes.LastIndexByte(b[:prev-1], '\n'); idx != -1 {
			prev = idx + 1
			start--
			// second context line (maybe start of file)
			if idx := bytes.LastIndexByte(b[:prev-1], '\n'); idx != -1 {
				prev = idx + 1
			} else {
				prev = 0
			}
			start--
		} else {
			prev = 0
			start--
		}
	}

	if prev == -1 {
		return five // TODO: BUG
	}
	//fmt.Printf("start=%d, prev=%d, window = %q\n", start, prev, b[prev:])
	scanner := bufio.NewScanner(bytes.NewReader(b[prev:]))
	for ; start < 5; start++ {
		if scanner.Scan() {
			five[start] = scanner.Text()
		}
	}
	return five
}

func (i *Index) QueryPositional(query string) ([]Match, error) {
	if len(query) < 4 {
		return nil, nil // not yet implemented
	}
	type planEntry struct {
		offset  int
		t       Trigram
		entries uint32
	}
	qb := []byte(query)
	plan := make([]planEntry, len(query)-2)
	// TODO: maybe parallelize building the plan?
	for j := 0; j < len(query)-2; j++ {
		t := Trigram(uint32(qb[j])<<16 |
			uint32(qb[j+1])<<8 |
			uint32(qb[j+2]))
		meta, _, err := i.Pos.metaEntry(t)
		if err != nil {
			return nil, err
		}
		plan[j] = planEntry{
			offset:  j,
			t:       t,
			entries: meta.Entries,
		}
	}
	plan[1] = plan[len(plan)-1]
	// TODO: figure out if query planning measurably decreases query latency in
	// production or not. For querylog-benchpos.txt, it’s a net loss: while the
	// posting lists for some queries are decoded much faster, the probability
	// of false positives is much lower whenthe first and last trigram
	// match. This results in more file/position tuples to verify, hence more
	// mmap() syscalls, hence a longer overall runtime.
	//
	// sort.Slice(plan, func(i, j int) bool { return plan[i].entries < plan[j].entries })
	first := plan[0]
	last := plan[1]

	// for _, p := range plan {
	// 	if math.Abs(float64(p.offset)-float64(first.offset)) < 3 {
	// 		continue
	// 	}
	// 	last = p
	// 	break
	// }

	var eg errgroup.Group

	var (
		fdocids []uint32
		fpos    []uint32
		fposrel []byte

		ldocids []uint32
		lpos    []uint32
		lposrel []byte
	)

	eg.Go(func() error {
		var err error
		fdocids, fpos, fposrel, err = i.matchesWithBufferDirect(first.t, i.firstBuffer)
		return err
	})

	eg.Go(func() error {
		var err error
		ldocids, lpos, lposrel, err = i.matchesWithBufferDirect(last.t, i.lastBuffer)
		return err
	})

	if err := eg.Wait(); err != nil {
		return nil, err
	}

	// filter matches based on position constraints
	//delta := len(query) - 3
	delta := last.offset - first.offset
	var entries []Match

	flipped := last.offset < first.offset //len(lpos) < len(fpos)
	if flipped {
		fdocids, ldocids = ldocids, fdocids
		fpos, lpos = lpos, fpos
		fposrel, lposrel = lposrel, fposrel
		delta *= -1
	}
	//log.Printf("plan[0] = %+v, plan[1] = %+v, delta = %d, flipped = %v", first, last, delta, flipped)

	var (
		fdocidIdx = -1
		fprevD    uint32
		fprevP    uint32

		ldocidIdx = -1
		lprevD    uint32
		lprevP    uint32
	)

	var j int // not reset to skip already-inspected parts of last
	llpos := len(lpos)
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

		lprevP += lpos[j]
	}
	jInc(0)
	for i := 0; i < len(fpos); i++ {
		if ((fposrel[i/8] >> (uint(i) % 8)) & 1) == 1 {
			fdocidIdx++
			fprevD += fdocids[fdocidIdx]
			fprevP = 0
		}
		fprevP += fpos[i]

		docid := fprevD
		pos := uint32(int(fprevP) + delta)
		for j < len(lpos) && lprevD < docid {
			// Skip pos entries until posrel contains a 1 (i.e. docid change):
			jInc(1 + bits.TrailingZeros16((uint16(lposrel[(j+1)/8])|0xFF00)>>(uint((j+1))%8)))
		}

		for ; j < len(lpos) && lprevD == docid; jInc(1) {
			// TODO: support regexp queries by using greater-than comparison instead of equals
			if lprevP < pos {
				continue
			}
			if lprevP == pos {
				if flipped {
					entries = append(entries, Match{Docid: fprevD, Position: lprevP})
				} else {
					entries = append(entries, Match{Docid: fprevD, Position: fprevP})
				}
			}
			break
		}
	}
	//log.Printf("len(entries) = %d", len(entries))
	return entries, nil
}

func (i *Index) Close() error {
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
