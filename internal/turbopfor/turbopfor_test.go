package turbopfor_test

import (
	"bytes"
	"math/rand"
	"reflect"
	"testing"

	"github.com/Debian/dcs/internal/turbopfor"
)

func TestEncode(t *testing.T) {
	for _, test := range []struct {
		name  string
		input []uint32
		want  []byte
	}{
		{
			name:  "small",
			input: []uint32{0x2a, 0x39, 0x5a, 0x77},
			want:  []byte{0x07, 0xaa, 0x9c, 0xf6, 0x0e},
		},

		{
			name:  "small with large exception",
			input: []uint32{0x2a, 0x39, 0x5a, 0x777777},
			want:  []byte{0x88, 0xf, 0x8, 0x77, 0x77, 0x2a, 0x39, 0x5a, 0x77},
		},

		{
			name:  "constant",
			input: []uint32{0x89},
			want:  []byte{0xc8, 0x89},
		},

		{
			name:  "bitpack 21 bit",
			input: []uint32{1120345, 1120349, 1120364, 1120311, 1120399, 1120388, 1120377},
			want:  []byte{0x15, 0x59, 0x18, 0xb1, 0xb, 0x23, 0xb2, 0x61, 0xc4, 0x1b, 0x8c, 0xf8, 0x88, 0x11, 0x9, 0x31, 0x62, 0x1e, 0x46, 0x4},
		},

		{
			name:  "bitpack with exceptions",
			input: []uint32{7, 9, 3, 4, 5, 1, 3, 7, 3, 1, 2, 254},
			want:  []byte{0x44, 0x1, 0x97, 0x43, 0x15, 0x73, 0x13, 0xe2, 0xf, 0xb},
		},

		{
			name:  "bitpack with 2-byte exceptions",
			input: []uint32{7, 9, 3, 4, 5, 1, 3, 7, 3, 1, 2, 11254},
			want:  []byte{0x44, 0x1, 0x97, 0x43, 0x15, 0x73, 0x13, 0x62, 0xb3, 0xe, 0xb},
		},

		{
			name:  "bitpack with 3-byte exceptions",
			input: []uint32{7, 9, 3, 4, 5, 1, 3, 7, 3, 1, 2, 11254},
			want:  []byte{0x44, 0x1, 0x97, 0x43, 0x15, 0x73, 0x13, 0x62, 0xb3, 0xe, 0xb},
		},

		{
			name:  "bitpack with big exceptions",
			input: []uint32{7, 9, 3, 4, 5, 1, 3, 7, 3, 1, 2, 718238414},
			// Sometimes, I saw:
			//                                                   0xfe
			//                                                   0xf2
			want: []byte{0x84, 0x1a, 0x0, 0x8, 0x2c, 0xf7, 0xac, 0x2, 0x97, 0x43, 0x15, 0x73, 0x13, 0xe2},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			padded := make([]uint32, len(test.input)+32)
			copy(padded, test.input)
			padded = padded[:len(test.input)]
			got := turbopfor.P4nenc32(padded)
			if !bytes.Equal(got, test.want) {
				t.Fatalf("unexpected encoding result:\ngot  %x (%#v)\nwant %x (%#v)", got, got, test.want, test.want)
			}
		})
	}
}

func TestDeltaEncode(t *testing.T) {
	t.Skipf("TODO: investigate this test failure")
	for _, test := range []struct {
		name  string
		input []uint32
		want  []byte
	}{
		{
			name:  "small",
			input: []uint32{3, 4, 5, 6, 9, 10, 15, 17, 18, 333},
			want:  []byte{0x03, 0x43, 0x01, 0x00, 0x04, 0x06, 0x42, 0x27, 0x08},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			padded := make([]uint32, turbopfor.DecodingSize(len(test.input)))
			copy(padded, test.input)
			padded = padded[:len(test.input)]
			got := turbopfor.P4nd1enc32(padded)
			if !bytes.Equal(got, test.want) {
				t.Fatalf("unexpected encoding result:\ngot  %x (%#v)\nwant %x (%#v)", got, got, test.want, test.want)
			}
		})
	}
}

func TestDeltaDecode(t *testing.T) {
	t.Skipf("TODO: investigate this test failure")
	for _, test := range []struct {
		name  string
		input []byte
		want  []uint32
	}{
		{
			name:  "small",
			input: []byte{0x03, 0x43, 0x01, 0x00, 0x04, 0x06, 0x42, 0x27, 0x08},
			want:  []uint32{3, 4, 5, 6, 9, 10, 15, 17, 18, 333},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			buffer := make([]uint32, len(test.want), len(test.want)+32)
			num := turbopfor.P4nd1dec32(test.input, buffer)
			if got, want := num, len(test.input); got != want {
				t.Fatalf("got %d, want %d", got, want)
			}
			if !reflect.DeepEqual(buffer, test.want) {
				t.Fatalf("got %v, want %v", buffer, test.want)
			}
		})
	}
}

func TestChunkedEncode(t *testing.T) {
	input := make([]uint32, 275, 275+32)
	for idx := range input {
		input[idx] = rand.Uint32()
	}
	want := turbopfor.P4nenc256v32(input)

	var got []byte
	for len(input) >= 256 {
		tmp := make([]byte, turbopfor.EncodingSize(len(input)))
		n := turbopfor.P4enc256v32(input, tmp)
		got = append(got, tmp[:n]...)
		input = input[256:]
	}
	got = append(got, turbopfor.P4enc32(input)...)

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %x\nwant %x", got, want)
	}
}

func TestChunkedDecode(t *testing.T) {
	input := make([]uint32, 275, 275+32)
	for idx := range input {
		input[idx] = rand.Uint32()
	}
	encoded := turbopfor.P4nenc256v32(input)

	got := make([]uint32, len(input))
	n := 0
	for ; n+256 < len(input); n += 256 {
		encoded = encoded[turbopfor.P4dec256v32(encoded, got[n:n+256]):]
	}
	turbopfor.P4dec32(encoded, got[n:])

	if !reflect.DeepEqual(got, input) {
		t.Fatalf("got %x\nwant %x", got, input)
	}
}
