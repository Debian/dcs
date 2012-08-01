// vim:ts=4:sw=4:noexpandtab

// Post-ranking happens on the source backend (because it has the source files
// in the kernel’s page cache). In the post-ranking phase we can do (limited)
// source file level analysis, such as in which scope the query string was
// matched (comment, top-level, sub-level).
package ranking

import (
)

//var packageLocation *regexp.Regexp = regexp.MustCompile(`debian-source-mirror/unpacked/([^/]+)_`)

