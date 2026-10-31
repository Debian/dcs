package index

import (
	"regexp/syntax"
	"testing"
)

func TestLongestLiteral(t *testing.T) {
	for _, tt := range []struct {
		expr string
		want string
	}{
		{
			expr: "hash",
			want: "hash",
		},

		{
			expr: "(?i)hash",
			want: "",
		},

		{
			expr: "int main\\(\\)",
			want: "int main()",
		},

		{
			expr: "hash.*rational",
			want: "rational",
		},
	} {
		re, err := syntax.Parse(tt.expr, syntax.Perl)
		if err != nil {
			t.Fatal(err)
		}
		got := LongestLiteral(re)
		if got != tt.want {
			t.Errorf("LongestLiteral(%s) = %q, want %q", tt.expr, got, tt.want)
		}
	}
}
