# Setting up a small instance of DCS for hacking

This document describes how to set up a small instance of DCS running on one
single machine with a very small index, so that you can quickly get it running
and start testing your changes to the source code.

## Prepare your environment

If you don’t already have Go installed, use:

```bash
sudo apt-get install golang-go
export GOPATH=~/gocode
export PATH=$GOPATH/bin:$PATH
```

I recommend to put the `export` statements into your shell profile so that you
don’t have to remember to run them.

## Download/update the source code

```bash
go get -u github.com/Debian/dcs/cmd/...
```

## Launch DCS

The `dcs-localdcs` tool recompiles the code and static assets, then brings up a
local DCS:

```bash
dcs-localdcs
# play around with DCS
dcs-localdcs -stop
```

To quickly restart the stack, you can use `dcs-localdcs -stop && dcs-localdcs`
after saving your changes in your editor of choice.
