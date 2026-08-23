package index

import (
	"os"
	"path/filepath"

	"github.com/Debian/dcs/internal/turbopfor/pforenc"
)

type pforWriter struct {
	f   countingWriter
	enc pforenc.StreamEncoder
}

func newPForWriter(dir, typ string) (*pforWriter, error) {
	f, err := os.Create(filepath.Join(dir, "posting."+typ+".turbopfor"))
	if err != nil {
		return nil, err
	}
	return &pforWriter{
		f: newCountingWriter(f),
	}, nil
}

func (pw *pforWriter) Offset() int64 {
	return int64(pw.f.offset)
}

func (pw *pforWriter) PutUint32(u uint32) error {
	if full := pw.enc.Add(u); full {
		if _, err := pw.f.Write(pw.enc.EncodeBlock()); err != nil {
			return err
		}
	}
	return nil
}

func (pw *pforWriter) Flush() error {
	b := pw.enc.EncodeBlock()
	if len(b) == 0 {
		return nil
	}
	if _, err := pw.f.Write(b); err != nil {
		return err
	}
	return nil
}

func (pw *pforWriter) Close() error {
	return pw.f.Close()
}
