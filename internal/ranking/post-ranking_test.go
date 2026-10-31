package ranking

import "testing"

func BenchmarkPostRank(b *testing.B) {
	querystr := NewQueryStr("done")
	opts := RankingOpts{Weighted: true}
	for b.Loop() {
		for i := range benchMatches {
			PostRank(opts, &benchMatches[i], &querystr)
		}
	}
}
