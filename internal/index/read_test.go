package index

import (
	"bytes"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestFiveLines(t *testing.T) {
	golden := []byte(`first line
second line
third line
foo bar
baz 234
qux 567 890<no newline>`)

	binShGolden := []byte(`#!/bin/sh
set -e

if [ "$(uname -s)" = "Linux" ]; then
	update-alternatives --install /usr/bin/pager pager /bin/more 50 \
		--slave /usr/share/man/man1/pager.1.gz pager.1.gz \
		/usr/share/man/man1/more.1.gz
fi

#DEBHELPER#
`)

	for _, tt := range []struct {
		input []byte
		query string
		want  [5]string
	}{

		{
			input: golden,
			query: "foo bar",
			want: [5]string{
				"second line",
				"third line",
				"foo bar",
				"baz 234",
				"qux 567 890<no newline>",
			},
		},

		{
			input: golden,
			query: "third line",
			want: [5]string{
				"first line",
				"second line",
				"third line",
				"foo bar",
				"baz 234",
			},
		},

		{
			input: golden,
			query: "qux 567",
			want: [5]string{
				"foo bar",
				"baz 234",
				"qux 567 890<no newline>",
				"",
				"",
			},
		},

		{
			input: []byte("oneline"),
			query: "one",
			want: [5]string{
				"",
				"",
				"oneline",
				"",
				"",
			},
		},

		{
			input: []byte("oneline\ntwoline"),
			query: "one",
			want: [5]string{
				"",
				"",
				"oneline",
				"twoline",
				"",
			},
		},

		{
			input: []byte("oneline\ntwoline\nthreeline"),
			query: "one",
			want: [5]string{
				"",
				"",
				"oneline",
				"twoline",
				"threeline",
			},
		},

		{
			input: []byte("oneline\ntwoline\nthreeline\nfourline\n"),
			query: "one",
			want: [5]string{
				"",
				"",
				"oneline",
				"twoline",
				"threeline",
			},
		},

		{
			input: []byte(binShGolden),
			query: "bin/sh",
			want: [5]string{
				"",
				"",
				"#!/bin/sh",
				"set -e",
				"",
			},
		},

		{
			input: []byte(binShGolden),
			query: "set -e",
			want: [5]string{
				"",
				"#!/bin/sh",
				"set -e",
				"",
				`if [ "$(uname -s)" = "Linux" ]; then`,
			},
		},
	} {
		t.Run("", func(t *testing.T) {
			got := FiveLines(tt.input, bytes.Index(tt.input, []byte(tt.query)))
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("FiveLines(%s): unexpected diff (-want +got):\n%s", tt.query, diff)
			}
		})
	}
}
