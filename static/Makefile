all:
	yui-compressor -o instant.min.js instant.js
	yui-compressor -o reconnecting-websocket.min.js reconnecting-websocket.js
	(cat debian.css debcodesearch.css) | yui-compressor --type css > debcodesearch.min.css
	sfnt2woff Roboto-Regular.ttf
	sfnt2woff Roboto-Bold.ttf
	sfnt2woff Inconsolata.otf
