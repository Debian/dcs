package index

import (
	"encoding/binary"
	"io"
)

// index is persisted to disk in little endian

type Trigram uint32

// A MetaEntry defines the position within the index of the data associated with
// a trigram.
type MetaEntry struct {
	Trigram    Trigram
	Entries    uint32 // number of entries (excluding padding)
	OffsetData int64  // delta offset within the corresponding .data or .turbopfor file
}

const (
	// metaEntrySize is (encoding/binary).Size(&MetaEntry{}), which Go 1.11 does
	// not turn into a compile-time constant yet.
	metaEntrySize = 16
)

var meBuf [metaEntrySize]byte // TODO: make concurrency safe
func writeMetaEntry(w io.Writer, me *MetaEntry) error {
	n := 0
	binary.LittleEndian.PutUint32(meBuf[n:], uint32(me.Trigram))
	n += 4
	binary.LittleEndian.PutUint32(meBuf[n:], (me.Entries))
	n += 4
	binary.LittleEndian.PutUint64(meBuf[n:], uint64(me.OffsetData))
	_, err := w.Write(meBuf[:])
	return err
}

func readMetaEntry(r io.Reader, me *MetaEntry) error {
	var meBuf [metaEntrySize]byte
	if _, err := io.ReadFull(r, meBuf[:]); err != nil {
		return err
	}

	n := 0
	me.Trigram = Trigram(binary.LittleEndian.Uint32(meBuf[n:]))
	n += 4
	me.Entries = binary.LittleEndian.Uint32(meBuf[n:])
	n += 4
	me.OffsetData = int64(binary.LittleEndian.Uint64(meBuf[n:]))

	return nil
}

func (me *MetaEntry) Unmarshal(b []byte) {
	n := 0
	me.Trigram = Trigram(binary.LittleEndian.Uint32(b[n:]))
	n += 4
	me.Entries = binary.LittleEndian.Uint32(b[n:])
	n += 4
	me.OffsetData = int64(binary.LittleEndian.Uint64(b[n:]))
}
