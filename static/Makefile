all: instant.min.js instant.min.js.gz debcodesearch.min.css.gz non-critical.min.css non-critical.min.css.gz critical.min.css critical.min.css.gz loadCSS.min.js loadCSS.min.js.gz cssrelpreload.min.js cssrelpreload.min.js.gz url-search-params.min.js.gz service-worker.min.js service-worker.min.js.gz

%.gz: %
	echo "ZÖPFLI $<"
	zopfli $<

%.min.js: %.js
	echo "MINIFY $<"
	slimit -m $< > $@ || yui-compressor -o $@ $< || yuicompressor -o $@ $<

%.min.css: %.css
	echo "MINIFY $<"
	(cat $<) | yui-compressor --type css > $@ || (cat $<) | yuicompressor --type css > $@ || (cat $<) | perl -MCSS::Minifier -E 'open(IN, "<", "/dev/stdin"); say CSS::Minifier::minify(input => *IN)' > $@

debcodesearch.min.css: debian.css debcodesearch.css
	echo "MINIFY $^"
	(cat $^) | yui-compressor --type css > $@ || (cat $^) | yuicompressor --type css > $@ || (cat $^) | perl -MCSS::Minifier -E 'open(IN, "<", "/dev/stdin"); say CSS::Minifier::minify(input => *IN)' > $@

.DELETE_ON_ERROR:
