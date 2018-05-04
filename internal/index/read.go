package index

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"math"
	"path/filepath"
	"sort"
	"turbopfor"

	"golang.org/x/exp/mmap"
)

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

type PForReader struct {
	meta     *mmap.ReaderAt
	data     *mmap.ReaderAt
	padbuf   []byte
	deltabuf []uint32
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
		return nil, nil, fmt.Errorf("not found")
	}
	var result MetaEntry
	result.Unmarshal(d[n*metaEntrySize:])
	if result.Trigram != trigram {
		return nil, nil, fmt.Errorf("not found")
	}

	var next MetaEntry
	if n < num-1 {
		next.Unmarshal(d[(n+1)*metaEntrySize:])
	} else {
		next.OffsetData = int64(sr.data.Len())
	}
	return &result, &next, nil
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
		dr.n += 256
		return dr.buf
	}
	if remaining := dr.entries - dr.n; remaining > 0 {
		turbopfor.P4dec32(dr.data, dr.buf[:remaining])
		dr.n += remaining
		return dr.buf[:remaining]
	}
	return nil
}

func (sr *PForReader) deltas(meta *MetaEntry) ([]uint32, error) {
	entries := int(meta.Entries)
	d := sr.data.Data()

	var deltas []uint32
	// TODO: figure out overhead. 128*1024 is wrong. might be 0, actually
	if n := entries + 128*1024; n < len(sr.deltabuf) {
		deltas = sr.deltabuf[:n-(128*1024)]
	} else {
		sr.deltabuf = make([]uint32, n)
		deltas = sr.deltabuf[:n-(128*1024)]
	}
	turbopfor.P4ndec256v32(d[meta.OffsetData:], deltas)
	return deltas, nil
}

func (sr *PForReader) Deltas(t Trigram) ([]uint32, error) {
	meta, _, err := sr.metaEntry(t)
	if err != nil {
		return nil, err
	}
	return sr.deltas(meta)
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
	// TODO: maybe de-duplicate with SectionReader.metaEntry?
	// TODO: copy better code from benchmark.go<as>
	entries := make([]MetaEntry, pr.meta.Len()/metaEntrySize)
	var buf [metaEntrySize]byte
	n := sort.Search(len(entries), func(i int) bool {
		if entries[i].Trigram == 0 {
			if _, err := pr.meta.ReadAt(buf[:], int64(i*metaEntrySize)); err != nil {
				log.Fatal(err) // TODO
			}
			if err := binary.Read(bytes.NewReader(buf[:]), binary.LittleEndian, &entries[i]); err != nil {
				log.Fatal(err) // TODO
			}
		}
		return entries[i].Trigram >= trigram
	})
	if n >= len(entries) || entries[n].Trigram != trigram {
		return nil, nil, fmt.Errorf("not found") // TODO
	}
	result := entries[n]
	var next MetaEntry
	if n < len(entries)-1 {
		i := n + 1
		if entries[i].Trigram == 0 {
			if _, err := pr.meta.ReadAt(buf[:], int64(i*metaEntrySize)); err != nil {
				log.Fatal(err) // TODO
			}
			if err := binary.Read(bytes.NewReader(buf[:]), binary.LittleEndian, &entries[i]); err != nil {
				log.Fatal(err) // TODO
			}
		}

		next = entries[n+1]
	} else {
		next.OffsetData = math.MaxInt64
	}
	return &result, &next, nil
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

func (pr *PosrelReader) Data(t Trigram) (io.ReaderAt, error) {
	meta, _, err := pr.metaEntry(t)
	if err != nil {
		return nil, err
	}
	return &mmapOffsetReader{r: pr.data, off: meta.OffsetData}, nil
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
}

func Open(dir string) (*Index, error) {
	var i Index
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

func (i *Index) Matches(t Trigram) ([]Match, error) {
	docids, err := i.Docid.Deltas(t)
	if err != nil {
		return nil, err
	}
	pos, err := i.Pos.Deltas(t)
	if err != nil {
		return nil, err
	}
	posrel, err := i.Posrel.Data(t)
	if err != nil {
		return nil, err
	}
	//log.Printf("%d docid, %d pos", len(docids), len(pos))
	matches := make([]Match, 0, len(pos))
	docidIdx := -1
	var prevD, prevP uint32
	var pr [1]byte
	for i := 0; i < len(pos); i++ {
		// should be 1 if the docid changes, 0 otherwise
		// TODO: access .data directly instead?
		// TODO: micro-benchmark the “read uint64s, use bits.TrailingZeros64(), mask u &= u-1” trick
		if _, err := posrel.ReadAt(pr[:], int64(i/8)); err != nil {
			return nil, err
		}
		// 205G ~/as/idx before
		// 158G ~/as/idx after \o/
		chg := int((pr[0] >> (uint(i) % 8)) & 1)
		docidIdx += chg
		prevP *= uint32(1 ^ chg)

		log.Printf("docidIdx=%d, chg=%d, pr = %x", docidIdx, chg, pr[0])
		prevD += docids[docidIdx] * uint32(chg)
		prevP += pos[i]
		matches = append(matches, Match{
			Docid:    prevD,
			Position: prevP,
		})
	}
	return matches, nil
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
