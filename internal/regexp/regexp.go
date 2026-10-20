// Copyright 2011 The Go Authors.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package regexp implements regular expression search tuned for
// use in grep-like programs.
package regexp

import "regexp/syntax"

func bug() {
	panic("codesearch/regexp: internal error")
}

// Regexp is the representation of a compiled regular expression.
// A Regexp is NOT SAFE for concurrent use by multiple goroutines.
type Regexp struct {
	Syntax *syntax.Regexp
	expr   string // original expression
	m      matcher
}

// String returns the source text used to compile the regular expression.
func (re *Regexp) String() string {
	return re.expr
}

// Compile parses a regular expression and returns, if successful,
// a Regexp object that can be used to match against lines of text.
func Compile(expr string) (*Regexp, error) {
	re, err := syntax.Parse(expr, syntax.Perl)
	if err != nil {
		return nil, err
	}
	sre := re.Simplify()
	prog, err := syntax.Compile(sre)
	if err != nil {
		return nil, err
	}
	if err := toByteProg(prog); err != nil {
		return nil, err
	}
	return newFromProg(expr, re, prog), nil
}

func newFromProg(expr string, re *syntax.Regexp, prog *syntax.Prog) *Regexp {
	r := &Regexp{
		Syntax: re,
		expr:   expr,
	}
	r.m.init(prog)
	return r
}

// Clone returns a copy of r, which can be used in a different goroutine.
//
// prog, the immutable compiled program, is shared,
// but the matcher state is new.
func (r *Regexp) Clone() *Regexp {
	return newFromProg(r.expr, r.Syntax, r.m.prog)
}

func (r *Regexp) Match(b []byte, beginText, endText bool) (end int) {
	return r.m.match(b, beginText, endText)
}

func (r *Regexp) MatchString(s string, beginText, endText bool) (end int) {
	return r.m.matchString(s, beginText, endText)
}
