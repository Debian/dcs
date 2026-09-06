# Setting up a small instance of DCS for hacking

This document describes how to set up a small instance of DCS running on one
single machine with a very small index, so that you can quickly get it running
and start testing your changes to the source code.

## Prepare your environment

If you don’t already have Go installed, use:

```bash
sudo apt install golang-go
```

## Download/update the source code

```bash
git clone https://github.com/Debian/dcs
```

## Launch DCS

The `dcs-localdcs` entrypoint runs all of DCS in a single process, indexes the
packages in `testdata/` and prints the URL at which you can access this local
DCS instance in your browser:

```bash
go run ./cmd/dcs-localdcs
```
