package dpkgversion

import (
	"testing"
)

// Abbreviation for creating a new Version object.
func v(epoch uint, version string, revision string) Version {
	return Version{Epoch: epoch, Version: version, Revision: revision}
}

func TestEpoch(t *testing.T) {
	if Compare(Version{Epoch: 1}, Version{Epoch: 2}) == 0 {
		t.Errorf("epoch=1, epoch=2")
	}

	if a, b := v(0, "1", "1"), v(0, "2", "1"); Compare(a, b) == 0 {
		t.Errorf("a, b")
	}

	if a, b := v(0, "1", "1"), v(0, "1", "2"); Compare(a, b) == 0 {
		t.Errorf("a, b")
	}
}

func TestEquality(t *testing.T) {
	if a, b := v(0, "0", "0"), v(0, "0", "0"); Compare(a, b) != 0 {
		t.Errorf("a, b")
	}

	if a, b := v(0, "0", "00"), v(0, "00", "0"); Compare(a, b) != 0 {
		t.Errorf("a, b")
	}

	if a, b := v(1, "2", "3"), v(1, "2", "3"); Compare(a, b) != 0 {
		t.Errorf("a, b")
	}
}

func TestEpochDifference(t *testing.T) {
	a := v(0, "0", "0")
	b := v(1, "0", "0")
	if Compare(a, b) >= 0 {
		t.Errorf("a, b")
	}
	if Compare(b, a) <= 0 {
		t.Errorf("a, b")
	}
}

func TestVersionDifference(t *testing.T) {
	a := v(0, "a", "0")
	b := v(0, "b", "0")
	if Compare(a, b) >= 0 {
		t.Errorf("a, b")
	}
	if Compare(b, a) <= 0 {
		t.Errorf("a, b")
	}
}

func TestRevisionDifference(t *testing.T) {
	a := v(0, "0", "a")
	b := v(0, "0", "b")
	if Compare(a, b) >= 0 {
		t.Errorf("a, b")
	}
	if Compare(b, a) <= 0 {
		t.Errorf("a, b")
	}
}

func TestCompareCodesearch(t *testing.T) {
	a := v(0, "1.8.6", "2")
	b := v(0, "1.8.6", "2.1")
	if Compare(a, b) >= 0 {
		t.Errorf("a, b")
	}
}

func TestParseZeroVersions(t *testing.T) {
	var a Version
	var err error
	b := v(0, "0", "")

	if a, err = Parse("0"); err != nil {
		t.Errorf("Parsing %q failed: %v", "0", err)
	}
	if Compare(a, b) != 0 {
		t.Errorf("Compare(%v, %v), got %d, want 0", a, b, Compare(a, b))
	}

	if a, err = Parse("0:0"); err != nil {
		t.Errorf("Parsing %q failed: %v", "0:0", err)
	}
	if Compare(a, b) != 0 {
		t.Errorf("Compare(%v, %v), got %d, want 0", a, b, Compare(a, b))
	}

	if a, err = Parse("0:0-"); err != nil {
		t.Errorf("Parsing %q failed: %v", "0:0-", err)
	}
	if Compare(a, b) != 0 {
		t.Errorf("Compare(%v, %v), got %d, want 0", a, b, Compare(a, b))
	}

	b = v(0, "0", "0")
	if a, err = Parse("0:0-0"); err != nil {
		t.Errorf("Parsing %q failed: %v", "0:0-0", err)
	}
	if Compare(a, b) != 0 {
		t.Errorf("Compare(%v, %v), got %d, want 0", a, b, Compare(a, b))
	}

	b = v(0, "0.0", "0.0")
	if a, err = Parse("0:0.0-0.0"); err != nil {
		t.Errorf("Parsing %q failed: %v", "0:0.0-0.0", err)
	}
	if Compare(a, b) != 0 {
		t.Errorf("Compare(%v, %v), got %d, want 0", a, b, Compare(a, b))
	}
}

func TestParseEpochedVersions(t *testing.T) {
	var a Version
	var err error
	b := v(1, "0", "")

	if a, err = Parse("1:0"); err != nil {
		t.Errorf("Parsing %q failed: %v", "1:0", err)
	}
	if Compare(a, b) != 0 {
		t.Errorf("Compare(%v, %v), got %d, want 0", a, b, Compare(a, b))
	}

	b = v(5, "1", "")
	if a, err = Parse("5:1"); err != nil {
		t.Errorf("Parsing %q failed: %v", "5:1", err)
	}
	if Compare(a, b) != 0 {
		t.Errorf("Compare(%v, %v), got %d, want 0", a, b, Compare(a, b))
	}
}

func TestParseMultipleHyphens(t *testing.T) {
	var a Version
	var err error
	b := v(0, "0-0", "0")

	if a, err = Parse("0:0-0-0"); err != nil {
		t.Errorf("Parsing %q failed: %v", "0:0-0-0", err)
	}
	if Compare(a, b) != 0 {
		t.Errorf("Compare(%v, %v), got %d, want 0", a, b, Compare(a, b))
	}

	b = v(0, "0-0-0", "0")
	if a, err = Parse("0:0-0-0-0"); err != nil {
		t.Errorf("Parsing %q failed: %v", "0:0-0-0-0", err)
	}
	if Compare(a, b) != 0 {
		t.Errorf("Compare(%v, %v), got %d, want 0", a, b, Compare(a, b))
	}
}

func TestParseMultipleColons(t *testing.T) {
	var a Version
	var err error
	b := v(0, "0:0", "0")

	if a, err = Parse("0:0:0-0"); err != nil {
		t.Errorf("Parsing %q failed: %v", "0:0:0-0", err)
	}
	if Compare(a, b) != 0 {
		t.Errorf("Compare(%v, %v), got %d, want 0", a, b, Compare(a, b))
	}

	b = v(0, "0:0:0", "0")
	if a, err = Parse("0:0:0:0-0"); err != nil {
		t.Errorf("Parsing %q failed: %v", "0:0:0:0-0", err)
	}
	if Compare(a, b) != 0 {
		t.Errorf("Compare(%v, %v), got %d, want 0", a, b, Compare(a, b))
	}
}

func TestParseMultipleHyphensAndColons(t *testing.T) {
	var a Version
	var err error
	b := v(0, "0:0-0", "0")

	if a, err = Parse("0:0:0-0-0"); err != nil {
		t.Errorf("Parsing %q failed: %v", "0:0:0-0-0", err)
	}
	if Compare(a, b) != 0 {
		t.Errorf("Compare(%v, %v), got %d, want 0", a, b, Compare(a, b))
	}

	b = v(0, "0-0:0", "0")
	if a, err = Parse("0:0-0:0-0"); err != nil {
		t.Errorf("Parsing %q failed: %v", "0:0-0:0-0", err)
	}
	if Compare(a, b) != 0 {
		t.Errorf("Compare(%v, %v), got %d, want 0", a, b, Compare(a, b))
	}
}

func TestParseValidUpstreamVersionCharacters(t *testing.T) {
	var a Version
	var err error
	b := v(0, "09azAZ.-+~:", "0")

	if a, err = Parse("0:09azAZ.-+~:-0"); err != nil {
		t.Errorf("Parsing %q failed: %v", "0:09azAZ.-+~:-0", err)
	}
	if Compare(a, b) != 0 {
		t.Errorf("Compare(%v, %v), got %d, want 0", a, b, Compare(a, b))
	}
}

func TestParseValidRevisionCharacters(t *testing.T) {
	var a Version
	var err error
	b := v(0, "0", "azAZ09.+~")

	if a, err = Parse("0:0-azAZ09.+~"); err != nil {
		t.Errorf("Parsing %q failed: %v", "0:0-azAZ09.+~", err)
	}
	if Compare(a, b) != 0 {
		t.Errorf("Compare(%v, %v), got %d, want 0", a, b, Compare(a, b))
	}
}

func TestParseLeadingTrailingSpaces(t *testing.T) {
	var a Version
	var err error
	b := v(0, "0", "1")

	if a, err = Parse("    0:0-1"); err != nil {
		t.Errorf("Parsing %q failed: %v", "    0:0-1", err)
	}
	if Compare(a, b) != 0 {
		t.Errorf("Compare(%v, %v), got %d, want 0", a, b, Compare(a, b))
	}

	if a, err = Parse("0:0-1     "); err != nil {
		t.Errorf("Parsing %q failed: %v", "0:0-1     ", err)
	}
	if Compare(a, b) != 0 {
		t.Errorf("Compare(%v, %v), got %d, want 0", a, b, Compare(a, b))
	}

	if a, err = Parse("      0:0-1     "); err != nil {
		t.Errorf("Parsing %q failed: %v", "      0:0-1     ", err)
	}
	if Compare(a, b) != 0 {
		t.Errorf("Compare(%v, %v), got %d, want 0", a, b, Compare(a, b))
	}
}

func TestParseEmptyVersion(t *testing.T) {
	if _, err := Parse(""); err == nil {
		t.Errorf("Expected an error, but %q was parsed without an error", "")
	}
	if _, err := Parse("  "); err == nil {
		t.Errorf("Expected an error, but %q was parsed without an error", "  ")
	}
}

func TestParseEmptyUpstreamVersionAfterEpoch(t *testing.T) {
	if _, err := Parse("0:"); err == nil {
		t.Errorf("Expected an error, but %q was parsed without an error", "0:")
	}
}

func TestParseVersionWithEmbeddedSpaces(t *testing.T) {
	if _, err := Parse("0:0 0-1"); err == nil {
		t.Errorf("Expected an error, but %q was parsed without an error", "0:0 0-1")
	}
}

func TestParseVersionWithNegativeEpoch(t *testing.T) {
	if _, err := Parse("-1:0-1"); err == nil {
		t.Errorf("Expected an error, but %q was parsed without an error", "-1:0-1")
	}
}

func TestParseVersionWithHugeEpoch(t *testing.T) {
	if _, err := Parse("999999999999999999999999:0-1"); err == nil {
		t.Errorf("Expected an error, but %q was parsed without an error", "999999999999999999999999:0-1")
	}
}

func TestParseInvalidCharactersInEpoch(t *testing.T) {
	if _, err := Parse("a:0-0"); err == nil {
		t.Errorf("Expected an error, but %q was parsed without an error", "a:0-0")
	}
	if _, err := Parse("A:0-0"); err == nil {
		t.Errorf("Expected an error, but %q was parsed without an error", "A:0-0")
	}
}

func TestParseUpstreamVersionNotStartingWithADigit(t *testing.T) {
	if _, err := Parse("0:abc3-0"); err == nil {
		t.Errorf("Expected an error, but %q was parsed without an error", "0:abc3-0")
	}
}

func TestParseInvalidCharactersInUpstreamVersion(t *testing.T) {
	chars := "!#@$%&/|\\<>()[]{};,_=*^'"
	for i := 0; i < len(chars); i++ {
		verstr := "0:0" + chars[i:i+1] + "-0"
		if _, err := Parse(verstr); err == nil {
			t.Errorf("Expected an error, but %q was parsed without an error", verstr)
		}
	}
}

func TestParseInvalidCharactersInRevision(t *testing.T) {
	if _, err := Parse("0:0-0:0"); err == nil {
		t.Errorf("Expected an error, but %q was parsed without an error", "0:0-0:0")
	}
	chars := "!#@$%&/|\\<>()[]{}:;,_=*^'"
	for i := 0; i < len(chars); i++ {
		verstr := "0:0-" + chars[i:i+1]
		if _, err := Parse(verstr); err == nil {
			t.Errorf("Expected an error, but %q was parsed without an error", verstr)
		}
	}
}
