all: instant.min.js.gz reconnecting-websocket.min.js.gz debcodesearch.min.css.gz

%.gz: %
	echo "ZÖPFLI $<"
	zopfli $<

%.min.js: %.js
	echo "MINIFY $<"
	slimit -m $< > $@ || yui-compressor -o $@ $< || yuicompressor -o $@ $<

debcodesearch.min.css: debian.css debcodesearch.css
	echo "MINIFY $^"
	(cat $^) | yui-compressor --type css > $@ || (cat $^) | yuicompressor --type css > $@ || (cat $^) | perl -MCSS::Minifier -E 'open(IN, "<", "/dev/stdin"); say CSS::Minifier::minify(input => *IN)' > $@
