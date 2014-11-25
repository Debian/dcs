all: instant.min.js reconnecting-websocket.min.js Roboto-Regular.woff Roboto-Bold.woff Inconsolata.woff debcodesearch.min.css

%.woff: %.?tf
	echo "WOFF $<"
	sfnt2woff $<

%.min.js: %.js
	echo "MINIFY $<"
	slimit -m $< > $@ || yui-compressor -o $@ $<

debcodesearch.min.css: debian.css debcodesearch.css
	echo "MINIFY $^"
	(cat $^) | yui-compressor --type css > $@ || (cat $^) | perl -MCSS::Minifier -E 'open(IN, "<", "/dev/stdin"); say CSS::Minifier::minify(input => *IN)' > $@
