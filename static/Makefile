all: instant.min.js.gz reconnecting-websocket.min.js.gz Roboto-Regular.woff.gz Roboto-Bold.woff.gz Inconsolata.woff.gz debcodesearch.min.css.gz

%.gz: %
	echo "ZÖPFLI $<"
	zopfli $<

%.woff: %.?tf
	echo "WOFF $<"
	sfnt2woff $<

%.min.js: %.js
	echo "MINIFY $<"
	slimit -m $< > $@ || yui-compressor -o $@ $<

debcodesearch.min.css: debian.css debcodesearch.css
	echo "MINIFY $^"
	(cat $^) | yui-compressor --type css > $@ || (cat $^) | perl -MCSS::Minifier -E 'open(IN, "<", "/dev/stdin"); say CSS::Minifier::minify(input => *IN)' > $@
