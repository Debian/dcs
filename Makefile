install:
	go install github.com/Debian/dcs/cmd/...

push:
	./push-ex62.zsh

test:
	go test -count=1 -v -race github.com/Debian/dcs/...

docs: contrib/ksy/meta.dot contrib/ksy/docidmap.dot
	dot -Tsvg contrib/ksy/meta.dot > howto/meta.svg
	dot -Tsvg contrib/ksy/docidmap.dot > howto/docidmap.svg
