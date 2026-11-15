package web

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDefaultPolicy(t *testing.T) {
	policy := defaultPolicy()
	if got, want := policy.Search.Timeout, 10*time.Minute; got != want {
		t.Fatalf("Search.Timeout: got %v, want %v", got, want)
	}
}

func TestReadPolicy(t *testing.T) {
	const testPolicy = `
[search]
timeout = "5m"  # reduce from the default (10 minutes)
speedbump = 0 # run all queries to completion
`
	fn := filepath.Join(t.TempDir(), "policy.toml")
	if err := os.WriteFile(fn, []byte(testPolicy), 0600); err != nil {
		t.Fatal(err)
	}
	policy, err := readPolicy(fn)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := policy.Search.Timeout, 5*time.Minute; got != want {
		t.Errorf("Search.Timeout: got %v, want %v", got, want)
	}
	if got, want := policy.Search.Speedbump, 0*time.Minute; got != want {
		t.Errorf("Search.Speedbump: got %v, want %v", got, want)
	}
}

func TestPartialPolicy(t *testing.T) {
	const testPolicy = `
[search]
# Leave timeout at its default.
speedbump = 0 # run all queries to completion
`
	fn := filepath.Join(t.TempDir(), "policy.toml")
	if err := os.WriteFile(fn, []byte(testPolicy), 0600); err != nil {
		t.Fatal(err)
	}
	policy, err := readPolicy(fn)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := policy.Search.Timeout, 10*time.Minute; got != want {
		t.Errorf("Search.Timeout: got %v, want %v", got, want)
	}
	if got, want := policy.Search.Speedbump, 0*time.Minute; got != want {
		t.Errorf("Search.Speedbump: got %v, want %v", got, want)
	}
}
