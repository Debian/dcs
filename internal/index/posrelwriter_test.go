package index

import (
	"bytes"
	"testing"
)

func TestPosrelWriter(t *testing.T) {
	type input struct {
		bytes   []byte
		entries int
	}
	tests := []struct {
		name   string
		inputs []input
		want   []byte
	}{
		{
			name: "build",
			inputs: []input{
				{[]byte{0x1}, 1},
				{[]byte{0x0}, 1},
				{[]byte{0x1}, 1},
				{[]byte{0x1}, 1},
			},
			want: []byte{0xd},
		},

		{
			name: "build full",
			inputs: []input{
				{[]byte{0x1}, 1},
				{[]byte{0x0}, 1},
				{[]byte{0x1}, 1},
				{[]byte{0x1}, 1},
				{[]byte{0x0}, 1},
				{[]byte{0x0}, 1},
				{[]byte{0x0}, 1},
				{[]byte{0x1}, 1},
			},
			want: []byte{0x8d},
		},

		{
			name: "build exceed",
			inputs: []input{
				{[]byte{0x1}, 1},
				{[]byte{0x0}, 1},
				{[]byte{0x1}, 1},
				{[]byte{0x1}, 1},
				{[]byte{0x0}, 1},
				{[]byte{0x0}, 1},
				{[]byte{0x0}, 1},
				{[]byte{0x1}, 1},
				{[]byte{0x1}, 1},
			},
			want: []byte{0x8d, 0x1},
		},

		{
			name: "passthrough",
			inputs: []input{
				{[]byte{0xaa, 0xbb}, 16},
			},
			want: []byte{0xaa, 0xbb},
		},

		{
			name: "merge full",
			inputs: []input{
				{[]byte{0xaa}, 8},
				{[]byte{0xbb}, 8},
			},
			want: []byte{0xaa, 0xbb},
		},

		{
			name: "merge half",
			inputs: []input{
				{[]byte{0xee, 0xa}, 8 + 4},
				{[]byte{0xb}, 4},
			},
			want: []byte{0xee, 0xba},
		},

		{
			name: "merge partial",
			inputs: []input{
				{
					// ee = 11101110
					// 2a = 00101010
					[]byte{0xee, 0x2a},
					8 + 7,
				},
				{[]byte{0xbb}, 8},
			},
			// ee = 11101110
			// 55 = 01010101
			// bb = 10111011
			want: []byte{0xee, 0xaa, 0x5d},
		},

		{
			// Verifies that posrelWriter does not make any assumptions about
			// slice length.
			name: "merge partial with longer slice",
			inputs: []input{
				{[]byte{0x03, 0x01}, 2}, // 00000011b
				{[]byte{0xc7}, 8},       // 11000111b
			},
			want: []byte{0x1f, 0x03},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			w := newPosrelWriter(&out)
			w.Debug = true
			for _, input := range tt.inputs {
				if err := w.Write(input.bytes, input.entries); err != nil {
					t.Fatal(err)
				}
			}
			if err := w.Flush(); err != nil {
				t.Fatal(err)
			}
			if got := out.Bytes(); !bytes.Equal(got, tt.want) {
				t.Fatalf("unexpected output: got %x, want %x", got, tt.want)
			}
		})
	}
}
