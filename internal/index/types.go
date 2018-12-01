package index

import "encoding/binary"

var encoding = binary.LittleEndian

type Trigram uint32

// A MetaEntry specifies the offset (in the corresponding data file) and number
// of entries for each trigram.
type MetaEntry struct {
	Trigram    Trigram
	Entries    uint32 // number of entries (excluding padding)
	OffsetData int64  // delta offset within the corresponding .data or .turbopfor file
}

// metaEntrySize is (encoding/binary).Size(&MetaEntry{}), which Go 1.11 does not
// turn into a compile-time constant yet.
const metaEntrySize = 16

func (me *MetaEntry) Unmarshal(b []byte) {
	me.Trigram = Trigram(encoding.Uint32(b))
	me.Entries = encoding.Uint32(b[4:])
	me.OffsetData = int64(encoding.Uint64(b[8:]))
}

func (me *MetaEntry) Marshal(b []byte) {
	encoding.PutUint32(b, uint32(me.Trigram))
	encoding.PutUint32(b[4:], me.Entries)
	encoding.PutUint64(b[8:], uint64(me.OffsetData))
}
