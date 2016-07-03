// vim:ts=4:sw=4:noexpandtab
// © 2012-2014 Michael Stapelberg (see also: LICENSE)

// Package debiancontrol implements a parser for Debian control files.
package godebiancontrol

import (
	"bufio"
	"io"
	"strings"
	"unicode"
)

type FieldType int

const (
	Simple FieldType = iota
	Folded
	Multiline
)

var fieldType = make(map[string]FieldType)

type Paragraph map[string]string

func init() {
	fieldType["Description"] = Multiline
	fieldType["Files"] = Multiline
	fieldType["Changes"] = Multiline
	fieldType["Checksums-Sha1"] = Multiline
	fieldType["Checksums-Sha256"] = Multiline
	fieldType["Package-List"] = Multiline
}

// Parses a Debian control file and returns a slice of Paragraphs.
//
// Implemented according to chapter 5.1 (Syntax of control files) of the Debian
// Policy Manual:
// http://www.debian.org/doc/debian-policy/ch-controlfields.html
func Parse(input io.Reader) (paragraphs []Paragraph, err error) {
	reader := bufio.NewReader(input)
	lastkey := ""
	var paragraph Paragraph = make(Paragraph)

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}

		trimmed := strings.TrimSpace(line)

		// Check if the line is empty (or consists only of whitespace). This
		// marks a new paragraph.
		if trimmed == "" {
			if len(paragraph) > 0 {
				paragraphs = append(paragraphs, paragraph)
			}
			paragraph = make(Paragraph)
			continue
		}

		// folded and multiline fields must start with a space or tab.
		if line[0] == ' ' || line[0] == '\t' {
			if fieldType[lastkey] == Multiline {
				// Whitespace, including newlines, is significant in the values
				// of multiline fields, therefore we just append the line
				// as-is.
				paragraph[lastkey] += line
			} else {
				// For folded lines we strip whitespace before appending.
				paragraph[lastkey] += trimmed
			}
		} else {
			split := strings.Split(trimmed, ":")
			key := split[0]
			value := strings.TrimLeftFunc(trimmed[len(key)+1:], unicode.IsSpace)
			paragraph[key] = value
			lastkey = key
		}
	}

	// Append last paragraph, but only if it is non-empty.
	// The case of an empty last paragraph happens when the file ends with a
	// blank line.
	if len(paragraph) > 0 {
		paragraphs = append(paragraphs, paragraph)
	}

	return
}
