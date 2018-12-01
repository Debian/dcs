package index

import (
	"io"
	"log"
)

type posrelWriter struct {
	w       io.Writer
	numbits int
	current byte

	Debug bool
}

func newPosrelWriter(w io.Writer) *posrelWriter {
	return &posrelWriter{w: w}
}

func (pw *posrelWriter) WriteByte(bits byte, n int) error {
	if pw.Debug {
		log.Printf("WriteByte(%x, %d), pw.current=%x, pw.numbits=%d", bits, n, pw.current, pw.numbits)
	}
	if pw.numbits+n > 8 {
		// e.g. pw.numbits=6, n=3
		// pw.current = xx111111
		//       bits = 00000yyy
		avail := 8 - pw.numbits // 2
		pw.current |= (bits << uint(pw.numbits))
		pw.numbits = 8
		if err := pw.Flush(); err != nil {
			return err
		}
		bits >>= uint(avail)
		n -= avail
	}
	pw.current |= (bits << uint(pw.numbits))
	pw.numbits += n
	return nil
}

func (pw *posrelWriter) Write(b []byte, n int) error {
	for i := 0; i < (n / 8); i++ {
		if err := pw.WriteByte(b[i], 8); err != nil {
			return err
		}
	}
	if rest := n % 8; rest > 0 {
		return pw.WriteByte(b[n/8], rest)
	}

	return nil
}

func (pw *posrelWriter) Flush() error {
	if pw.numbits == 0 {
		return nil
	}
	if pw.Debug {
		log.Printf("flushing %x, pw.current=%x, pw.numbits=%d", pw.current, pw.current, pw.numbits)
	}
	if _, err := pw.w.Write([]byte{pw.current}); err != nil {
		return err
	}
	pw.current = 0
	pw.numbits = 0
	return nil
}
