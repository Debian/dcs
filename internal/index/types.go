package index

// index is persisted to disk in little endian

type Trigram uint32

// A MetaEntry defines the position within the index of the data associated with
// a trigram.
type MetaEntry struct {
	Trigram    Trigram
	Entries    uint32 // number of entries (excluding padding)
	OffsetCtrl int64  // control bytes offset within the corresponding .ctrl file
	OffsetData int64  // streaming vbyte deltas offset within the corresponding .data file
}

const metaEntrySize = 24 // (encoding/binary).Size(MetaEntry)
