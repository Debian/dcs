package static

import (
	"embed"
	"fmt"
	"hash"
	"hash/fnv"
	"io"
)

//go:embed critical.css critical.min.css
//go:embed non-critical.css non-critical.min.css
//go:embed debcodesearch.css debcodesearch.min.css
//go:embed instant.js instant.min.js
//go:embed highlightjs.min.js highlightjs-default.min.css
//go:embed about.html contact.html faq.html thirdparty.html
//go:embed favicon.ico opensearch.xml robots.txt
//go:embed openapi.json openapi.yaml openapi2.json openapi2.yaml
//go:embed Inconsolata.woff Inconsolata.woff2 Roboto-Regular.woff Roboto-Regular.woff2 Roboto-Bold.woff Roboto-Bold.woff2
//go:embed Pics/gradient.png Pics/openlogo-50.svg
var FS embed.FS

func Hash(path string) string {
	f, err := FS.Open(path)
	if err != nil {
		return fmt.Sprintf("BUG: %v", err)
	}
	defer f.Close()
	h := quickhash()
	if _, err := io.Copy(h, f); err != nil {
		return fmt.Sprintf("Copy: %v", err)
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

func quickhash() hash.Hash {
	return fnv.New128()
}
