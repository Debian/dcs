package web

import (
	"fmt"
	"os"
	"time"

	"github.com/BurntSushi/toml"
)

// Policy makes all limits configurable that dcs-web applies
// to accepting queries, running queries, keeping results, etc.
//
// These used to be configured via command-line flags,
// but the number of related flags grew too large,
// so now we have a Policy, expressed in TOML.
type Policy struct {
	Search SearchPolicy `toml:"search"`
	// AdmissionPolicy
	// TODO: -max_concurrent_queries
	// TODO: -max_concurrent_queries_by_owner
}

// SearchPolicy configures how a search works:
// How long is it allowed to run (overall deadline),
// after how many seconds does the speedbump trigger, etc.
type SearchPolicy struct {
	// Timeout sets the overall deadline of a query (e.g. 10 minutes).
	Timeout time.Duration `toml:"timeout"`

	// Speedbump: if >0, stops queries and requires confirmation:
	// https://codesearch.debian.net/faq#speedbump (e.g. 10 seconds).
	Speedbump time.Duration `toml:"speedbump"`
}

func defaultPolicy() *Policy {
	return &Policy{
		Search: SearchPolicy{
			Timeout:   10 * time.Minute,
			Speedbump: 10 * time.Second,
		},
	}
}

// readPolicy reads the policy from path.
func readPolicy(path string) (*Policy, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	pol := defaultPolicy()
	dec, err := toml.Decode(string(b), pol)
	if err != nil {
		return nil, err
	}
	if undecoded := dec.Undecoded(); len(undecoded) > 0 {
		return nil, fmt.Errorf("%s: unknown TOML keys: %v", path, undecoded)
	}
	// TODO: validate policy for plausibility / value ranges
	return pol, nil
}
