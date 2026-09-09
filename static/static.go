package static

import "embed"

//go:embed critical*.css non-critical.css debian.css debcodesearch.css
//go:embed instant.js service-worker.js
//go:embed jquery.min.js highlightjs.min.js highlightjs-default.min.css
//go:embed about.html contact.html faq.html thirdparty.html
//go:embed favicon.ico opensearch.xml robots.txt
//go:embed openapi.json openapi.yaml openapi2.json openapi2.yaml
//go:embed Inconsolata.woff Inconsolata.woff2 Roboto-Regular.woff Roboto-Regular.woff2 Roboto-Bold.woff Roboto-Bold.woff2
//go:embed Pics/gradient.png Pics/openlogo-50.svg
var FS embed.FS
