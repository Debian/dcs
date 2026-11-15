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
speedbump = 0  # run all queries to completion

[admission]
max_concurrent = 64  # for servers with lots of CPUs
max_concurrent_per_owner = 64  # no fairness

[results]
keep_max = 42
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
	if got, want := policy.Admission.MaxConcurrent, 64; got != want {
		t.Errorf("Admission.MaxConcurrent: got %v, want %v", got, want)
	}
	if got, want := policy.Results.KeepMax, 42; got != want {
		t.Errorf("Results.KeepMax: got %v, want %v", got, want)
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
