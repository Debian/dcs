package index

import (
	"bufio"
	"encoding/binary"
	"io"
	"log"
	"os"
	"path/filepath"
	"slices"
)

type indexMeta struct {
	docidBase uint32
	rd        *PForReader
}

type posrelMeta struct {
	rd *PosrelReader
}

func readMeta(dir, typ string, idx map[Trigram][]uint32, idxid uint32) error {
	f, err := os.Open(filepath.Join(dir, "posting."+typ+".meta"))
	if err != nil {
		return err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return err
	}
	bufr := bufio.NewReader(f)

	buf := make([]byte, metaEntrySize)
	for i := 0; i < (int(st.Size()) / metaEntrySize); i++ {
		if _, err := io.ReadFull(bufr, buf); err != nil {
			return err
		}
		t := Trigram(binary.LittleEndian.Uint32(buf))
		idx[t] = append(idx[t], idxid)
	}
	return nil
}

const debug = false

var debugTrigram = func(trigram string) Trigram {
	t := []byte(trigram)
	return Trigram(uint32(t[0])<<16 | uint32(t[1])<<8 | uint32(t[2]))
}("_op")

type docidMapMerge struct {
	bufr    *bufio.Reader // reused between merge() calls
	dest    *countingWriter
	offsets []uint32
}

func (m *docidMapMerge) merge(srcdir string) (uint32, error) {
	f, err := os.Open(filepath.Join(srcdir, "docid.map"))
	if err != nil {
		return 0, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return 0, err
	}
	if _, err := f.Seek(-4, io.SeekEnd); err != nil {
		return 0, err
	}
	// Locate index offset:
	var indexOffset uint32
	if err := binary.Read(f, binary.LittleEndian, &indexOffset); err != nil {
		return 0, err
	}

	// TODO: detect |base| overflows
	n := (uint32(st.Size()) - indexOffset - 4) / 4

	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return 0, err
	}
	m.bufr.Reset(f)
	// TODO(performance): measure whether using the index and incrementing
	// the offsets is any faster than this method:
	scanner := bufio.NewScanner(&io.LimitedReader{
		R: m.bufr,
		N: int64(indexOffset)})
	for scanner.Scan() {
		m.offsets = append(m.offsets, uint32(m.dest.offset))
		m.dest.Write(scanner.Bytes())
		m.dest.Write([]byte{'\n'})
	}
	if err := scanner.Err(); err != nil {
		return 0, err
	}
	return n, nil
}

func mergeDocidMaps(destdir string, srcdirs []string) ([]uint32, error) {
	fDocidMap, err := os.Create(filepath.Join(destdir, "docid.map"))
	if err != nil {
		return nil, err
	}
	defer fDocidMap.Close()
	cw := newCountingWriter(fDocidMap)

	m := &docidMapMerge{
		bufr: bufio.NewReader(nil),
		dest: &cw,
	}

	bases := make([]uint32, len(srcdirs))
	var base uint32
	for idx, srcdir := range srcdirs {
		bases[idx] = base
		n, err := m.merge(srcdir)
		if err != nil {
			return nil, err
		}
		log.Printf("%s (idx %d) contains %d docids", srcdir, idx, n)
		base += n
	}
	indexStart := uint32(cw.offset)
	if err := binary.Write(&cw, binary.LittleEndian, m.offsets); err != nil {
		return nil, err
	}
	if err := binary.Write(&cw, binary.LittleEndian, indexStart); err != nil {
		return nil, err
	}

	if err := cw.Close(); err != nil {
		return nil, err
	}

	return bases, nil
}

func ConcatN(destdir string, srcdirs []string) error {
	if err := os.MkdirAll(destdir, 0755); err != nil {
		return err
	}

	bases, err := mergeDocidMaps(destdir, srcdirs)
	if err != nil {
		return err
	}

	log.Printf("reading fileMetaEntries")

	idxMetaPos := make([]indexMeta, len(srcdirs))
	idxMetaPosrel := make([]posrelMeta, len(srcdirs))

	idxDocid := make(map[Trigram][]uint32)
	for idx, dir := range srcdirs {
		base := bases[idx]

		if err := readMeta(dir, "docid", idxDocid, uint32(idx)); err != nil {
			return err
		}

		{
			rd, err := newPForReader(dir, "pos")
			if err != nil {
				return err
			}
			defer rd.Close()
			idxMetaPos[idx] = indexMeta{docidBase: base, rd: rd}
		}

		{
			rd, err := newPosrelReader(dir)
			if err != nil {
				return err
			}
			defer rd.Close()

			idxMetaPosrel[idx] = posrelMeta{rd: rd}
		}
	}

	trigrams := make([]Trigram, 0, len(idxDocid))
	for t := range idxDocid {
		trigrams = append(trigrams, t)
	}
	slices.Sort(trigrams)

	{
		idxMetaDocid := make([]indexMeta, len(srcdirs))

		for idx, dir := range srcdirs {
			base := bases[idx]
			rd, err := newPForReader(dir, "docid")
			if err != nil {
				return err
			}
			idxMetaDocid[idx] = indexMeta{docidBase: base, rd: rd}
		}

		if err := writeDocids(destdir, trigrams, idxDocid, idxMetaDocid); err != nil {
			return err
		}
		for _, meta := range idxMetaDocid {
			meta.rd.Close()
		}
	}

	if err := writePosrel(destdir, trigrams, idxDocid, idxMetaPos, idxMetaPosrel); err != nil {
		return err
	}

	if err := writePos(destdir, trigrams, idxDocid, idxMetaPos); err != nil {
		return err
	}

	return nil
}

func writeDocids(destdir string, trigrams []Trigram, idxDocid map[Trigram][]uint32, idxMetaDocid []indexMeta) error {
	log.Printf("writing merged docids")
	dw, err := newPForWriter(destdir, "docid")
	if err != nil {
		return err
	}

	fDocidMeta, err := os.Create(filepath.Join(destdir, "posting.docid.meta"))
	if err != nil {
		return err
	}
	defer fDocidMeta.Close()
	bufwDocidMeta := bufio.NewWriter(fDocidMeta)

	meBuf := make([]byte, metaEntrySize)
	dr := NewDeltaReader()
	var meta MetaEntry
	for _, t := range trigrams {
		if debug {
			if t != debugTrigram {
				continue
			}
		}

		//for _, t := range []trigram{trigram(6650227), trigram(7959906)} {
		//ctrl, data := dw.Offsets()
		me := MetaEntry{
			Trigram: t,
			//OffsetCtrl: ctrl,
			//OffsetEnc:  data,
			OffsetData: dw.Offset(),
		}
		var last uint32
		for _, idxid := range idxDocid[t] {
			idx := idxMetaDocid[idxid]
			if err := idx.rd.metaEntry1(&meta, t); err != nil {
				if err == errNotFound {
					continue
				}
				return err
			}
			me.Entries += meta.Entries
			dr.Reset(&meta, idx.rd.data.Data)
			docids := dr.Read() // returns non-nil at least once
			// Bump the first docid: it needs to be mapped from the old
			// docid range [0, n) to the new docid range [base, base+n).
			//
			// Since we are building a single docid list for this trigram,
			// the new value needs to be a delta, hence, subtract last.
			docids[0] += (idx.docidBase - last)
			for docids != nil {
				for _, d := range docids {
					if err := dw.PutUint32(d); err != nil {
						return err
					}
					last += d
				}
				docids = dr.Read()
			}
		}
		if err := dw.Flush(); err != nil {
			return err
		}
		me.Marshal(meBuf)
		if _, err := bufwDocidMeta.Write(meBuf); err != nil {
			//if err := binary.Write(bufwDocidMeta, binary.LittleEndian, &me); err != nil {
			return err
		}
	}

	if err := bufwDocidMeta.Flush(); err != nil {
		return err
	}

	if err := fDocidMeta.Close(); err != nil {
		return err
	}

	if err := dw.Close(); err != nil {
		return err
	}

	return nil
}

func writePosrel(destdir string, trigrams []Trigram, idxDocid map[Trigram][]uint32, idxMetaPos []indexMeta, idxMetaPosrel []posrelMeta) error {
	log.Printf("writing merged posrel")
	fmetaf, err := os.Create(filepath.Join(destdir, "posting.posrel.meta"))
	if err != nil {
		return err
	}
	defer fmetaf.Close()
	bufwmeta := bufio.NewWriter(fmetaf)

	fposrel, err := os.Create(filepath.Join(destdir, "posting.posrel.data"))
	if err != nil {
		return err
	}
	defer fposrel.Close()
	cw := newCountingWriter(fposrel)
	pw := newPosrelWriter(&cw)
	var fmeta, pmeta MetaEntry
	for _, t := range trigrams {
		if debug {
			if t != debugTrigram {
				continue
			}
		}
		if t == 2105376 { // TODO: document: "   "?
			continue
		}

		me := MetaEntry{
			Trigram:    t,
			OffsetData: int64(cw.offset),
		}
		if err := binary.Write(bufwmeta, binary.LittleEndian, &me); err != nil {
			return err
		}
		for _, idxid := range idxDocid[t] {
			// TODO: refactor all metaEntry1 to use ,ok idiom, they only ever return errNotFound
			if err := idxMetaPos[idxid].rd.metaEntry1(&fmeta, t); err != nil {
				if err == errNotFound {
					continue
				}
				return err
			}

			if err := idxMetaPosrel[idxid].rd.metaEntry1(&pmeta, t); err != nil {
				if err == errNotFound {
					continue
				}
				return err
			}
			b := idxMetaPosrel[idxid].rd.data.Data[pmeta.OffsetData:]
			if err := pw.Write(b, int(fmeta.Entries)); err != nil {
				return err
			}

		}
		if err := pw.Flush(); err != nil {
			return err
		}
	}
	if err := bufwmeta.Flush(); err != nil {
		return err
	}
	if err := fmetaf.Close(); err != nil {
		return err
	}
	if err := cw.Close(); err != nil {
		return err
	}
	return nil
}

func writePos(destdir string, trigrams []Trigram, idxDocid map[Trigram][]uint32, idxMetaPos []indexMeta) error {
	log.Printf("writing merged pos")
	dw, err := newPForWriter(destdir, "pos")
	if err != nil {
		return err
	}

	fDocidMeta, err := os.Create(filepath.Join(destdir, "posting.pos.meta"))
	if err != nil {
		return err
	}
	defer fDocidMeta.Close()
	bufwDocidMeta := bufio.NewWriter(fDocidMeta)

	meBuf := make([]byte, metaEntrySize)
	dr := NewDeltaReader()
	var meta MetaEntry
	//for _, t := range []trigram{trigram(6650227), trigram(7959906)} {
	for _, t := range trigrams {
		if debug {
			if t != debugTrigram {
				continue
			}
		}

		if t == 2105376 { // TODO: document: "   "?
			continue
		}

		//ctrl, data := dw.Offsets()
		me := MetaEntry{
			Trigram: t,
			// OffsetCtrl: ctrl,
			// OffsetEnc:  data,
			OffsetData: dw.Offset(),
		}

		for _, idxid := range idxDocid[t] {
			idx := idxMetaPos[idxid]
			if err := idx.rd.metaEntry1(&meta, t); err != nil {
				if err == errNotFound {
					continue
				}
				return err
			}
			me.Entries += meta.Entries
			dr.Reset(&meta, idx.rd.data.Data)

			for docids := dr.Read(); docids != nil; docids = dr.Read() {
				for _, d := range docids {
					if err := dw.PutUint32(d); err != nil {
						return err
					}
				}
			}
		}

		if err := dw.Flush(); err != nil {
			return err
		}

		me.Marshal(meBuf)
		if _, err := bufwDocidMeta.Write(meBuf); err != nil {
			//if err := binary.Write(bufwDocidMeta, binary.LittleEndian, &me); err != nil {
			return err
		}
	}

	if err := bufwDocidMeta.Flush(); err != nil {
		return err
	}

	if err := fDocidMeta.Close(); err != nil {
		return err
	}

	if err := dw.Close(); err != nil {
		return err
	}

	return nil
}
