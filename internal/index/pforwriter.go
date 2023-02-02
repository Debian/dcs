package index

import (
	"log"
	"os"
	"path/filepath"

	"github.com/Debian/dcs/internal/turbopfor"
)

type pforWriter struct {
	f    countingWriter
	ints []uint32
	buf  []byte
}

func newPForWriter(dir, typ string) (*pforWriter, error) {
	f, err := os.Create(filepath.Join(dir, "posting."+typ+".turbopfor"))
	if err != nil {
		return nil, err
	}
	return &pforWriter{
		f:    newCountingWriter(f),
		ints: make([]uint32, 0, 256+32),
	}, nil
}

func (pw *pforWriter) Offset() int64 {
	return int64(pw.f.offset)
}

func (pw *pforWriter) PutUint32(u uint32) error {
	pw.ints = append(pw.ints, u)
	if len(pw.ints) == 256 {
		if sz := turbopfor.EncodingSize(len(pw.ints)); len(pw.buf) < sz {
			pw.buf = make([]byte, sz)
		}
		n := turbopfor.P4enc256v32(pw.ints, pw.buf)
		if _, err := pw.f.Write(pw.buf[:n]); err != nil {
			return err
		}
		pw.ints = pw.ints[:0]
	}
	return nil
}

func (pw *pforWriter) PrintFlush() error {
	log.Printf("encoding (256) %v", pw.ints)
	b := turbopfor.P4nenc256v32(pw.ints)
	log.Printf("b = %x (len %d)", b, len(b))
	if _, err := pw.f.Write(b); err != nil {
		return err
	}
	pw.ints = pw.ints[:0]
	return nil
}

func (pw *pforWriter) Flush() error {
	if len(pw.ints) == 0 {
		return nil
	}
	b := turbopfor.P4nenc256v32(pw.ints)
	if _, err := pw.f.Write(b); err != nil {
		return err
	}
	pw.ints = pw.ints[:0]
	return nil
}

func (pw *pforWriter) Close() error {
	return pw.f.Close()
}
