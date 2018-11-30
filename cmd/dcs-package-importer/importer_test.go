package main

import "testing"

func TestIsTar(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		filename string
		want     bool
	}{
		{"test.tar.gz", true},
		{"test.tar.bz2", true},
		{"test.tar.xz", true},
		{"test.tar.lz", true},
		{"test.tar.gz.sig", false},
	} {
		t.Run(tt.filename, func(t *testing.T) {
			got := isTar(tt.filename)
			if got != tt.want {
				t.Errorf("isTar(%v) = %v, want %v", tt.filename, got, tt.want)
			}
		})
	}
}
