// vim:ts=4:sw=4:noexpandtab
// © 2012-2014 Michael Stapelberg (see also: LICENSE)

package godebiancontrol

import (
	"bufio"
	"io"
	"strings"
)

const (
	beginPgp       = `-----BEGIN PGP SIGNED MESSAGE-----`
	beginSignature = `-----BEGIN PGP SIGNATURE-----`
)

type signatureStripper struct {
	reader    io.Reader
	bufreader *bufio.Reader
}

func (s *signatureStripper) Read(p []byte) (n int, err error) {
	line, err := s.bufreader.ReadString('\n')
	if err != nil {
		return 0, err
	}
	trimmed := strings.TrimSpace(line)
	if trimmed == beginPgp {
		// Read until we find an empty line, there may be key value pairs in
		// the header of this message.
		for trimmed != "" {
			line, err = s.bufreader.ReadString('\n')
			if err != nil {
				return 0, err
			}
			trimmed = strings.TrimSpace(line)
		}
	} else if trimmed == beginSignature {
		return 0, io.EOF
	}

	return copy(p, line), nil
}

// PGPSignatureStripper returns a reader that strips the PGP signature (if any)
// from the input data without verifying it.
func PGPSignatureStripper(input io.Reader) io.Reader {
	return &signatureStripper{input, bufio.NewReader(input)}
}
