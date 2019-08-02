install:
	go install github.com/Debian/dcs/cmd/...

push:
	./push-ex62.zsh

test:
	go test -count=1 -v -race github.com/Debian/dcs/...
