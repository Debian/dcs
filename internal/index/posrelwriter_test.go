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
