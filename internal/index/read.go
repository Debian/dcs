package index

import (
	"avxdecode"
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"math"
	"os"
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

type SectionReader struct {
	meta *mmap.ReaderAt
	ctrl *mmap.ReaderAt
	data *mmap.ReaderAt
}

func newSectionReader(dir, section string) (*SectionReader, error) {
	var sr SectionReader
	var err error
	if sr.meta, err = mmap.Open(filepath.Join(dir, "posting."+section+".meta")); err != nil {
		return nil, err
	}
	if sr.ctrl, err = mmap.Open(filepath.Join(dir, "posting."+section+".ctrl")); err != nil {
		return nil, err
	}
	if sr.data, err = mmap.Open(filepath.Join(dir, "posting."+section+".data")); err != nil {
		return nil, err
	}
	return &sr, nil
}

func (sr *SectionReader) Close() error {
	if err := sr.meta.Close(); err != nil {
		return err
	}
	if err := sr.ctrl.Close(); err != nil {
		return err
	}
	if err := sr.data.Close(); err != nil {
		return err
	}
	return nil
}

// TODO: remove once count0 is gone
func (sr *SectionReader) Trigrams() ([]Trigram, error) {
	l := sr.meta.Len() / metaEntrySize
	result := make([]Trigram, l)
	var entry MetaEntry
	var buf [metaEntrySize]byte
	for i := 0; i < l; i++ {
		if _, err := sr.meta.ReadAt(buf[:], int64(i*metaEntrySize)); err != nil {
			return nil, err
		}
		if err := binary.Read(bytes.NewReader(buf[:]), binary.LittleEndian, &entry); err != nil {
			return nil, err
		}
		result[i] = entry.Trigram
	}
	return result, nil
}

func (sr *SectionReader) metaEntry(trigram Trigram) (*MetaEntry, *MetaEntry, error) {
	// TODO: copy better code from benchmark.go<as>
	entries := make([]MetaEntry, sr.meta.Len()/metaEntrySize)
	var buf [metaEntrySize]byte
	n := sort.Search(len(entries), func(i int) bool {
		if entries[i].Trigram == 0 {
			if _, err := sr.meta.ReadAt(buf[:], int64(i*metaEntrySize)); err != nil {
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
			if _, err := sr.meta.ReadAt(buf[:], int64(i*metaEntrySize)); err != nil {
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

func (sr *SectionReader) MetaEntry(trigram Trigram) (*MetaEntry, error) {
	e, _, err := sr.metaEntry(trigram)
	return e, err
}

// Streams returns a ctrl stream and data stream reader for the specified
// trigram.
func (sr *SectionReader) Streams(t Trigram) (ctrl io.Reader, data io.Reader, entries int, _ error) {
	meta, next, err := sr.metaEntry(t)
	if err != nil {
		return nil, nil, 0, err
	}
	ctrlBytes := (int64(meta.Entries) + 3) / 4
	dataBytes := next.OffsetData - meta.OffsetData
	// TODO: benchmark whether an *os.File with Seek is measurably worse
	return &io.LimitedReader{
			R: &mmapReader{r: sr.ctrl, off: meta.OffsetCtrl},
			N: ctrlBytes,
		},
		&io.LimitedReader{
			R: &mmapReader{r: sr.data, off: meta.OffsetData},
			N: dataBytes,
		},
		int(meta.Entries),
		nil
}

func (sr *SectionReader) Deltas(t Trigram) ([]uint32, error) {
	ctrl, data, entries, err := sr.Streams(t)
	if err != nil {
		return nil, err
	}
	var bctrl, bdata bytes.Buffer
	// TODO: parallelize?
	if _, err := io.Copy(&bctrl, ctrl); err != nil {
		return nil, err
	}
	if _, err := io.Copy(&bdata, data); err != nil {
		return nil, err
	}
	deltas := make([]uint32, bctrl.Len()*4)
	// TODO: move avxdecode into dcs dir
	avxdecode.DecodeAVX(deltas, bctrl.Bytes(), bdata.Bytes(), entries)
	deltas = deltas[:entries]
	return deltas, nil
}

type cachedLookup struct {
	docid uint32 // 0xFFFFFFFF is invalid, because 0 is a valid docid
	fn    string
}

type DocidReader struct {
	f           *mmap.ReaderAt
	indexOffset uint32
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
	DocidMap *DocidReader   // docid → filename mapping
	Docid    *SectionReader // docids for all trigrams
	Pos      *SectionReader // positions for all trigrams
	Posrel   *PosrelReader  // position relationships for all trigrams

	pfdocid *PForReader
	pfpos   *PForReader
}

func Open(dir string) (*Index, error) {
	var i Index
	var err error
	if i.DocidMap, err = newDocidReader(dir); err != nil {
		return nil, err
	}

	// if i.Docid, err = newSectionReader(dir, "docid"); err != nil {
	// 	return nil, err
	// }
	// if i.Pos, err = newSectionReader(dir, "pos"); err != nil {
	// 	return nil, err
	// }

	if i.Posrel, err = newPosrelReader(dir); err != nil {
		return nil, err
	}

	if i.pfdocid, err = newPForReader(dir, "docid"); err != nil {
		return nil, err
	}
	if i.pfpos, err = newPForReader(dir, "pos"); err != nil {
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
		if _, err := posrel.ReadAt(pr[:], int64(i/8)); err != nil {
			return nil, err
		}
		// 205G ~/as/idx before
		// 158G ~/as/idx after \o/
		chg := int((pr[0] >> (uint(i) % 8)) & 1)
		docidIdx += chg
		prevP *= uint32(1 ^ chg)

		// if docids[i] != 0 && i > 0 && chg != 1 {
		// 	log.Printf("BUG: idx %d, docid changed but chg = %d", i, chg)
		// } else if docids[i] == 0 && chg != 0 {
		// 	log.Printf("BUG: idx %d, docid same but chg = %d", i, chg)
		// } else {
		// 	log.Printf("VERIFIED: idx %d", i)
		// }
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

func (i *Index) PForMatches(t Trigram) ([]Match, error) {
	docids, err := i.pfdocid.Deltas(t)
	if err != nil {
		return nil, err
	}
	pos, err := i.pfpos.Deltas(t)
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
		if _, err := posrel.ReadAt(pr[:], int64(i/8)); err != nil {
			return nil, err
		}
		// 205G ~/as/idx before
		// 158G ~/as/idx after \o/
		chg := int((pr[0] >> (uint(i) % 8)) & 1)
		docidIdx += chg
		prevP *= uint32(1 ^ chg)

		// if docids[i] != 0 && i > 0 && chg != 1 {
		// 	log.Printf("BUG: idx %d, docid changed but chg = %d", i, chg)
		// } else if docids[i] == 0 && chg != 0 {
		// 	log.Printf("BUG: idx %d, docid same but chg = %d", i, chg)
		// } else {
		// 	log.Printf("VERIFIED: idx %d", i)
		// }
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

type PForReader struct {
	meta *mmap.ReaderAt
	data *mmap.ReaderAt
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
	// TODO: copy better code from benchmark.go<as>
	entries := make([]MetaEntry, sr.meta.Len()/metaEntrySize)
	var buf [metaEntrySize]byte
	n := sort.Search(len(entries), func(i int) bool {
		if entries[i].Trigram == 0 {
			if _, err := sr.meta.ReadAt(buf[:], int64(i*metaEntrySize)); err != nil {
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
			if _, err := sr.meta.ReadAt(buf[:], int64(i*metaEntrySize)); err != nil {
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

func (sr *PForReader) MetaEntry(trigram Trigram) (*MetaEntry, error) {
	e, _, err := sr.metaEntry(trigram)
	return e, err
}

// Streams returns a ctrl stream and data stream reader for the specified
// trigram.
func (sr *PForReader) Streams(t Trigram) (data io.Reader, entries int, _ error) {
	meta, next, err := sr.metaEntry(t)
	if err != nil {
		return nil, 0, err
	}
	dataBytes := next.OffsetData - meta.OffsetData
	log.Printf("offset: %d, bytes: %d", meta.OffsetData, dataBytes)
	// TODO: benchmark whether an *os.File with Seek is measurably worse
	return &io.LimitedReader{
			R: &mmapReader{r: sr.data, off: meta.OffsetData},
			N: dataBytes,
		},
		int(meta.Entries),
		nil
}

func (sr *PForReader) Deltas(t Trigram) ([]uint32, error) {
	t = 0
	data, entries, err := sr.Streams(t)
	if err != nil {
		return nil, err
	}
	var bdata bytes.Buffer
	if _, err := io.Copy(&bdata, data); err != nil {
		return nil, err
	}
	padded := make([]byte, bdata.Len()+32)
	copy(padded, bdata.Bytes())
	padded = padded[:bdata.Len()]

	deltas := make([]uint32, entries, entries+128*1024)
	log.Printf("trigram %d decoding %d entries from %d (cap %d) to %d (cap %d) ints", t, entries, len(padded), cap(padded), len(deltas), cap(deltas))

	log.Printf("data: %#v", bdata.Bytes())
	//turbopfor.P4ndec32(padded, deltas)
	turbopfor.P4ndec256v32(padded, deltas)
	log.Printf("deltas: %#v", deltas)
	os.Exit(0)
	return deltas, nil
}
