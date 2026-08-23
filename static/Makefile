all: instant.min.js debcodesearch.min.css non-critical.min.css critical.min.css loadCSS.min.js cssrelpreload.min.js service-worker.min.js

%.min.js: %.js
	go tool esbuild --minify $< > $@

%.min.css: %.css
	go tool esbuild --minify --loader=css < $< > $@

debcodesearch.min.css: debian.css debcodesearch.css
	cat $^ | go tool esbuild --minify --loader=css > $@

.DELETE_ON_ERROR:
