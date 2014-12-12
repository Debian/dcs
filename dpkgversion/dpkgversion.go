// dpkgversion is a pure-go implementation of dpkg version string functions
// (parsing, comparison) which is compatible with dpkg(1).
package dpkgversion

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

type Version struct {
	Epoch    uint
	Version  string
	Revision string
}

func (v Version) String() string {
	var result string
	if v.Epoch > 0 {
		result = strconv.Itoa(int(v.Epoch)) + ":" + v.Version
	} else {
		result = v.Version
	}
	if len(v.Revision) > 0 {
		result += "-" + v.Revision
	}
	return result
}

func cisdigit(r rune) bool {
	return r >= '0' && r <= '9'
}

func cisalpha(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
}

func order(r rune) int {
	if cisdigit(r) {
		return 0
	}
	if cisalpha(r) {
		return int(r)
	}
	if r == '~' {
		return -1
	}
	if int(r) != 0 {
		return int(r) + 256
	}
	return 0
}

func verrevcmp(a string, b string) int {
	i := 0
	j := 0
	for i < len(a) || j < len(b) {
		var first_diff int
		for (i < len(a) && !cisdigit(rune(a[i]))) ||
			(j < len(b) && !cisdigit(rune(b[j]))) {
			ac := 0
			if i < len(a) {
				ac = order(rune(a[i]))
			}
			bc := 0
			if j < len(b) {
				bc = order(rune(b[j]))
			}
			if ac != bc {
				return ac - bc
			}
			i++
			j++
		}

		for i < len(a) && a[i] == '0' {
			i++
		}
		for j < len(b) && b[j] == '0' {
			j++
		}

		for i < len(a) && cisdigit(rune(a[i])) && j < len(b) && cisdigit(rune(b[j])) {
			if first_diff == 0 {
				first_diff = int(rune(a[i]) - rune(b[j]))
			}
			i++
			j++
		}

		if i < len(a) && cisdigit(rune(a[i])) {
			return 1
		}
		if j < len(b) && cisdigit(rune(b[j])) {
			return -1
		}
		if first_diff != 0 {
			return first_diff
		}
	}
	return 0
}

// Compare compares the two provided Debian versions. It returns 0 if a and b
// are equal, a value < 0 if a is smaller than b and a value > 0 if a is
// greater than b.
func Compare(a Version, b Version) int {
	if a.Epoch > b.Epoch {
		return 1
	}
	if a.Epoch < b.Epoch {
		return -1
	}

	rc := verrevcmp(a.Version, b.Version)
	if rc != 0 {
		return rc
	}

	return verrevcmp(a.Revision, b.Revision)
}

// Parse returns a Version struct filled with the epoch, version and revision
// specified in input. It verifies the version string as a whole, just like
// dpkg(1), and even returns roughly the same error messages.
func Parse(input string) (Version, error) {
	result := Version{}
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return result, fmt.Errorf("version string is empty")
	}

	if strings.IndexFunc(trimmed, unicode.IsSpace) != -1 {
		return result, fmt.Errorf("version string has embedded spaces")
	}

	colon := strings.Index(trimmed, ":")
	if colon != -1 {
		epoch, err := strconv.ParseInt(trimmed[:colon], 10, 64)
		if err != nil {
			return result, fmt.Errorf("epoch: %v", err)
		}
		if epoch < 0 {
			return result, fmt.Errorf("epoch in version is negative")
		}
		result.Epoch = uint(epoch)
	}

	result.Version = trimmed[colon+1:]
	if len(result.Version) == 0 {
		return result, fmt.Errorf("nothing after colon in version number")
	}
	if hyphen := strings.LastIndex(result.Version, "-"); hyphen != -1 {
		result.Revision = result.Version[hyphen+1:]
		result.Version = result.Version[:hyphen]
	}

	if len(result.Version) > 0 && !unicode.IsDigit(rune(result.Version[0])) {
		return result, fmt.Errorf("version number does not start with digit")
	}

	if strings.IndexFunc(result.Version, func(c rune) bool {
		return !cisdigit(c) && !cisalpha(c) && c != '.' && c != '-' && c != '+' && c != '~' && c != ':'
	}) != -1 {
		return result, fmt.Errorf("invalid character in version number")
	}

	if strings.IndexFunc(result.Revision, func(c rune) bool {
		return !cisdigit(c) && !cisalpha(c) && c != '.' && c != '+' && c != '~'
	}) != -1 {
		return result, fmt.Errorf("invalid character in revision number")
	}

	return result, nil
}

// vim:ts=4:sw=4:noexpandtab
