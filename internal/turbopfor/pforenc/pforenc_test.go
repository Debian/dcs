package pforenc

import (
	"fmt"
	"math"
	"math/rand/v2"
	"slices"
	"testing"

	"github.com/Debian/dcs/internal/turbopfor/pfordec"
	"github.com/google/go-cmp/cmp"
)

func TestEncodeBlock(t *testing.T) {
	var be BlockEncoder
	var bd pfordec.BlockDecoder
	var out [LargestP4]byte
	vals := []uint32{0x2342, 1111, 19, math.MaxUint32}
	decoded := make([]uint32, len(vals))
	encoded := be.EncodeBlock(out[:0], vals)
	n := bd.DecodeBlock(encoded, decoded)
	if n != len(encoded) {
		t.Fatalf("DecodeBlock() = %d, want len(encoded)=%d", n, len(encoded))
	}
	if diff := cmp.Diff(vals, decoded); diff != "" {
		t.Fatalf("EncodeBlock() does not round-trip through DecodeBlock: diff (-decoded +vals):\n%s", diff)
	}
}

func TestEncodeN(t *testing.T) {
	var be BlockEncoder
	var bd pfordec.BlockDecoder
	var out [10 * LargestP4]byte
	vals := []uint32{0x2342, 1111, 19, math.MaxUint32}
	vals1Block := slices.Repeat(vals, 256/len(vals))
	vals5Blocks := slices.Repeat(vals1Block, 5)
	decoded := make([]uint32, len(vals5Blocks))
	encoded := be.EncodeN(out[:0], vals5Blocks)
	n := bd.DecodeN(encoded, decoded)
	if n != len(encoded) {
		t.Fatalf("DecodeN() = %d, want len(encoded)=%d", n, len(encoded))
	}
	if diff := cmp.Diff(vals5Blocks, decoded); diff != "" {
		t.Fatalf("EncodeBlock() does not round-trip through DecodeN: diff (-decoded +vals):\n%s", diff)
	}
}

// testCaseGen is a generator that produces n uint32 values,
// typically in such a way that a certain TurboPFor block type is chosen.
type testCaseGen struct {
	name string
	gen  func(idxInBlock int) uint32 // idxInBlock is [0..255]
}

func genAllConstant(int) uint32 { return 42 }

func genBitpackingBW(bitWidth int) func(int) uint32 {
	rng := rand.New(rand.NewPCG(uint64(bitWidth) /* seed1 */, 0 /* seed2 */))
	return func(idxInBlock int) uint32 {
		if idxInBlock == 0 {
			// Ensure that we don’t end up with a smaller bitWidth
			// in case the rng returns smaller values.
			return 1<<bitWidth - 1
		}
		return rng.Uint32N(1 << bitWidth)
	}
}

func genBitpackingExcBW(bitWidth int) func(int) uint32 {
	rng := rand.New(rand.NewPCG(uint64(bitWidth) /* seed1 */, 0 /* seed2 */))
	return func(idxInBlock int) uint32 {
		// Insert 64 exceptions that need the full 32 bits each.
		// At 64 exceptions, the fixed-width exception bitmap (always 32 bytes)
		// clearly wins over bitpacking-vb-exc (stores each index, i.e. 64 bytes).
		if idxInBlock%4 == 0 {
			return math.MaxUint32
		}
		return rng.Uint32N(1 << bitWidth)
	}
}

func genBitpackingVBExc(idxInBlock int) uint32 {
	// Insert only 1 exception, so that vb-exc becomes clearly the best choice.
	if idxInBlock == 128 {
		return math.MaxUint32
	}
	return uint32(idxInBlock) * 16 // [0..4080]; bitWidth=12
}

func genSparseBitmap(idxInBlock int) uint32 {
	// 64 nonzero values per block: bit width 0 with bitmap exceptions.
	if idxInBlock%4 == 0 {
		return 1000 + uint32(idxInBlock)
	}
	return 0
}

func genSparseVB(idxInBlock int) uint32 {
	// Insert only 8 non-zero values per block (vb-exc is best).
	if idxInBlock%32 == 0 {
		return 1000 + uint32(idxInBlock)
	}
	return 0
}

var testCaseGenerators = []testCaseGen{
	{name: "all-zero", gen: func(int) uint32 { return 0 }},
	{name: "all-constant", gen: genAllConstant},
	{name: "bitpacking-bw1", gen: genBitpackingBW(1)},
	{name: "bitpacking-bw2", gen: genBitpackingBW(2)},
	{name: "bitpacking-bw7", gen: genBitpackingBW(7)},
	{name: "bitpacking-bw1-exc", gen: genBitpackingExcBW(1)},
	{name: "bitpacking-bw2-exc", gen: genBitpackingExcBW(2)},
	{name: "bitpacking-bw7-exc", gen: genBitpackingExcBW(7)},
	{name: "bitpacking-vb-exc", gen: genBitpackingVBExc},
	{name: "sparse-exc", gen: genSparseBitmap},
	{name: "sparse-vb-exc", gen: genSparseVB},
}

type testCase struct {
	name string
	vals []uint32
}

func testCases(nvals int) []testCase {
	var cases []testCase
	for _, sb := range testCaseGenerators {
		vals := make([]uint32, 0, nvals)
		for val := range nvals {
			vals = append(vals, sb.gen(val%256))
		}
		cases = append(cases, testCase{
			name: sb.name,
			vals: vals,
		})
	}
	return cases
}

func debianWeightedMix(nvals int) testCase {
	// Debian Code Search weighted value mix:
	debianMixGens := []func(int) uint32{
		genBitpackingExcBW(2),
		genBitpackingExcBW(8),
		genBitpackingExcBW(2),
		genBitpackingVBExc,
		genBitpackingExcBW(8),
		genBitpackingVBExc,
		genAllConstant,
		genBitpackingExcBW(8),
	}
	vals := make([]uint32, 0, nvals)
	for val := range nvals {
		gen := debianMixGens[(val/256)%len(debianMixGens)]
		vals = append(vals, gen(val%256))
	}
	return testCase{
		name: "debian-mix",
		vals: vals,
	}
}

func allTestCases() []testCase {
	var cases []testCase

	// Boundary cases: no values at all, one constant value.
	cases = append(cases, testCase{
		name: "empty",
		vals: nil,
	})
	cases = append(cases, testCase{
		name: "one-constant",
		vals: []uint32{42},
	})

	cases = append(cases, allBenchCases()...)

	return cases
}

func allBenchCases() []testCase {
	var cases []testCase

	// Exercise different block types, for full blocks and remainders.
	for _, n := range []int{
		8 * 256,     // 2048; i.e. 8 full TurboPFor blocks
		7*256 + 247, // 247%16 == 7 (causes a tail in scan)
		160,         // remainder block (not full)
	} {
		cases = append(cases, testCases(n)...)
		cases = append(cases, debianWeightedMix(n))
	}

	return cases
}

func TestRoundTrip(t *testing.T) {
	var be BlockEncoder
	var bd pfordec.BlockDecoder
	for _, tc := range allTestCases() {
		n := len(tc.vals)
		t.Run(fmt.Sprintf("n=%d/vals=%s", n, tc.name), func(t *testing.T) {
			if tc.vals == nil {
				t.Skipf("C does not like empty inputs")
			}
			out := make([]byte, 0, (n/256+1)*LargestP4)
			encoded := be.EncodeN(out, tc.vals)
			decoded := make([]uint32, n)
			read := bd.DecodeN(encoded, decoded)
			if read != len(encoded) {
				t.Fatalf("DecodeN() = %d, want len(encoded)=%d", read, len(encoded))
			}
			if diff := cmp.Diff(tc.vals, decoded); diff != "" {
				t.Fatalf("Go encoder does not round-trip through C decoder: diff (-decoded +vals):\n%s", diff)
			}
		})
	}
}

// reportMetrics adds Mval/s and encoded-bytes metrics to all benchmarks.
func reportMetrics(b *testing.B, n int, nencoded int) {
	b.ReportMetric(float64(nencoded), "encoded-bytes")
	b.ReportMetric(float64(b.N*n)/1e6/b.Elapsed().Seconds(), "Mval/s")
}

// BenchmarkEncode/n=<N>/vals=<testcase>/impl=<c|go|go-stream>
//
// e.g. BenchmarkEncode/n=2048/vals=one-constant/impl=go-stream
func BenchmarkEncode(b *testing.B) {
	for _, tc := range allBenchCases() {
		n := len(tc.vals)
		b.Run(fmt.Sprintf("n=%d/vals=%s", n, tc.name), func(b *testing.B) {
			b.Run("impl=go", func(b *testing.B) {
				b.ReportAllocs()
				var be BlockEncoder
				var encoded []byte
				buf := make([]byte, 0, (n/256+1)*LargestP4)
				for b.Loop() {
					encoded = be.EncodeN(buf, tc.vals)
				}
				reportMetrics(b, n, len(encoded))
			})
			b.Run("impl=go-stream", func(b *testing.B) {
				b.ReportAllocs()
				var se StreamEncoder
				var encoded int
				for b.Loop() {
					encoded = 0
					for _, val := range tc.vals {
						if se.Add(val) {
							encoded += len(se.EncodeBlock())
						}
					}
					encoded += len(se.EncodeBlock())
				}
				reportMetrics(b, n, encoded)
			})
		})
	}
}
