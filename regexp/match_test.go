package regexp

import (
	"strings"
	"testing"
)

func TestMatchContextAfter(t *testing.T) {
	bufferSize := 1 << 20
	// The context data which is placed "after the fold", that is after one
	// buffer’s worth of data.
	contextAfterFold := "ctx1\nctx2\n"
	buffer := make([]byte, bufferSize+len(contextAfterFold))

	max := bufferSize - len("\nfnord\n")
	for i := 0; i < max; i++ {
		buffer[i] = 'x'
	}
	buffer[max] = '\n'
	buffer[max+1] = 'f'
	buffer[max+2] = 'n'
	buffer[max+3] = 'o'
	buffer[max+4] = 'r'
	buffer[max+5] = 'd'
	buffer[max+6] = '\n'
	buffer[max+7] = 'c'
	buffer[max+8] = 't'
	buffer[max+9] = 'x'
	buffer[max+10] = '1'
	buffer[max+11] = '\n'
	buffer[max+12] = 'b'
	buffer[max+13] = 'a'
	buffer[max+14] = '\n'
	re, err := Compile("fnord")
	if err != nil {
		t.Fatalf("Compile(%#q): %v", "fnord", err)
	}

	g := Grep{}
	g.Regexp = re
	matches := g.Reader(strings.NewReader(string(buffer)), "input")
	if len(matches) != 1 {
		t.Fatalf("Expected precisely one match, got %d", len(matches))
	}
	if matches[0].Ctxn1 != "ctx1" {
		t.Errorf("Context +1 wrong: %s", matches[0].Ctxn1)
	}
	if matches[0].Ctxn2 != "ba" {
		t.Errorf("Context +2 wrong: %s", matches[0].Ctxn2)
	}

	re, err = Compile("ba")
	if err != nil {
		t.Fatalf("Compile(%#q): %v", "ba", err)
	}
	g.Regexp = re
	matches = g.Reader(strings.NewReader(string(buffer)), "input")
	if len(matches) != 1 {
		t.Fatalf("Expected precisely one match, got %d", len(matches))
	}
	if matches[0].Ctxp1 != "ctx1" {
		t.Errorf("Context -1 wrong: %s", matches[0].Ctxp1)
	}
	if matches[0].Ctxp2 != "fnord" {
		t.Errorf("Context -2 wrong: %s", matches[0].Ctxp2)
	}
}

func TestMatchContextBefore(t *testing.T) {
	bufferSize := 1 << 20
	// The context data which is placed "after the fold", that is after one
	// buffer’s worth of data.
	contextAfterFold := "ctx1\nctx2\n"
	buffer := make([]byte, bufferSize+len(contextAfterFold))

	max := bufferSize - len("\nfnord\nctx1\n")
	for i := 0; i < max; i++ {
		buffer[i] = 'x'
	}
	buffer[max] = '\n'
	buffer[max+1] = 'f'
	buffer[max+2] = 'n'
	buffer[max+3] = 'o'
	buffer[max+4] = 'r'
	buffer[max+5] = 'd'
	buffer[max+6] = '\n'
	buffer[max+7] = 'c'
	buffer[max+8] = 't'
	buffer[max+9] = 'x'
	buffer[max+10] = '1'
	buffer[max+11] = '\n'
	buffer[max+12] = 'b'
	buffer[max+13] = 'a'
	buffer[max+14] = '\n'

	re, err := Compile("ba")
	if err != nil {
		t.Fatalf("Compile(%#q): %v", "ba", err)
	}
	g := Grep{}
	g.Regexp = re
	matches := g.Reader(strings.NewReader(string(buffer)), "input")
	if len(matches) != 1 {
		t.Fatalf("Expected precisely one match, got %d", len(matches))
	}
	if matches[0].Ctxp1 != "ctx1" {
		t.Errorf("Context -1 wrong: %s", matches[0].Ctxp1)
	}
	if matches[0].Ctxp2 != "fnord" {
		t.Errorf("Context -2 wrong: %s", matches[0].Ctxp2)
	}
}
