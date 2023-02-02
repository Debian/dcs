package index

import (
	"bufio"
	"encoding/binary"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
)

type fileMetaEntry struct {
	idxid   uint32
	entries uint32
	offset  int64
}

type indexMeta struct {
	docidBase uint32
	rd        *PForReader
}

type posrelMetaEntry struct {
	idxid  uint32
	offset int64
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

func readPosrelMeta(dir string, idx map[Trigram][]posrelMetaEntry, idxid uint32) error {
	f, err := os.Open(filepath.Join(dir, "posting.posrel.meta"))
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
	var entry MetaEntry
	for i := 0; i < (int(st.Size()) / metaEntrySize); i++ {
		if _, err := io.ReadFull(bufr, buf); err != nil {
			return err
		}
		entry.Unmarshal(buf)
		idx[entry.Trigram] = append(idx[entry.Trigram], posrelMetaEntry{
			idxid:  idxid,
			offset: entry.OffsetData,
		})
	}
	return nil
}

const debug = false

var debugTrigram = func(trigram string) Trigram {
	t := []byte(trigram)
	return Trigram(uint32(t[0])<<16 | uint32(t[1])<<8 | uint32(t[2]))
}("_op")

func ConcatN(destdir string, srcdirs []string) error {
	fDocidMap, err := os.Create(filepath.Join(destdir, "docid.map"))
	if err != nil {
		return err
	}
	defer fDocidMap.Close()
	cw := newCountingWriter(fDocidMap)

	var (
		base    uint32
		offsets []uint32
	)
	bufr := bufio.NewReader(nil)
	bases := make([]uint32, len(srcdirs))
	for idx, dir := range srcdirs {
		bases[idx] = base
		f, err := os.Open(filepath.Join(dir, "docid.map"))
		if err != nil {
			return err
		}
		defer f.Close()
		st, err := f.Stat()
		if err != nil {
			return err
		}
		if _, err := f.Seek(-4, io.SeekEnd); err != nil {
			return err
		}
		// Locate index offset:
		var indexOffset uint32
		if err := binary.Read(f, binary.LittleEndian, &indexOffset); err != nil {
			return err
		}

		// TODO: detect |base| overflows
		n := (uint32(st.Size()) - indexOffset - 4) / 4
		log.Printf("%s (idx %d) contains %d docids", dir, idx, n)
		base += n

		if _, err := f.Seek(0, io.SeekStart); err != nil {
			return err
		}
		bufr.Reset(f)
		// TODO(performance): measure whether using the index and incrementing
		// the offsets is any faster than this method:
		scanner := bufio.NewScanner(&io.LimitedReader{
			R: bufr,
			N: int64(indexOffset)})
		for scanner.Scan() {
			offsets = append(offsets, uint32(cw.offset))
			cw.Write(scanner.Bytes())
			cw.Write([]byte{'\n'})
		}
		if err := scanner.Err(); err != nil {
			return err
		}
	}
	indexStart := uint32(cw.offset)
	if err := binary.Write(&cw, binary.LittleEndian, offsets); err != nil {
		return err
	}
	if err := binary.Write(&cw, binary.LittleEndian, indexStart); err != nil {
		return err
	}

	if err := cw.Close(); err != nil {
		return err
	}

	log.Printf("reading fileMetaEntries")

	idxMetaDocid := make([]indexMeta, len(srcdirs))
	idxMetaPos := make([]indexMeta, len(srcdirs))
	idxMetaPosrel := make([]posrelMeta, len(srcdirs))

	idxDocid := make(map[Trigram][]uint32)
	for idx, dir := range srcdirs {
		base := bases[idx]

		{
			rd, err := newPForReader(dir, "docid")
			if err != nil {
				return err
			}
			defer rd.Close()
			idxMetaDocid[idx] = indexMeta{docidBase: base, rd: rd}
		}

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
	sort.Slice(trigrams, func(i, j int) bool { return trigrams[i] < trigrams[j] })

	{
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
				meta, err := idx.rd.metaEntry1(t)
				if err != nil {
					if err == errNotFound {
						continue
					}
					return err
				}
				me.Entries += meta.Entries
				dr.Reset(meta, idx.rd.data.Data)
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
	}

	{
		log.Printf("writing merged posrel")
		fmeta, err := os.Create(filepath.Join(destdir, "posting.posrel.meta"))
		if err != nil {
			return err
		}
		defer fmeta.Close()
		bufwmeta := bufio.NewWriter(fmeta)

		fposrel, err := os.Create(filepath.Join(destdir, "posting.posrel.data"))
		if err != nil {
			return err
		}
		defer fposrel.Close()
		cw := newCountingWriter(fposrel)
		pw := newPosrelWriter(&cw)
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
				fmeta, err := idxMetaPos[idxid].rd.metaEntry1(t)
				if err != nil {
					if err == errNotFound {
						continue
					}
					return err
				}

				pmeta, err := idxMetaPosrel[idxid].rd.metaEntry1(t)
				if err != nil {
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
		if err := fmeta.Close(); err != nil {
			return err
		}
		if err := cw.Close(); err != nil {
			return err
		}
	}

	{
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
				meta, err := idx.rd.metaEntry1(t)
				if err != nil {
					if err == errNotFound {
						continue
					}
					return err
				}
				me.Entries += meta.Entries
				dr.Reset(meta, idx.rd.data.Data)

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
	}

	return nil
}
