# Building Debian Code Search

This document describes two main use cases: deploying DCS to a server and
setting up an environment in which you can easily hack on DCS.

## Building a Debian package: git-buildpackage

Just run `git-buildpackage --git-pbuilder` in the checked out dcs repository
and you will not have to install any dependencies.

## Setting up a build environment

Install the Go compiler and toolchain, preferably from Debian testing:

```bash
apt-get install golang-go
```

Go requires you to setup a directory in which all source code is stored. The location of that directory is stored in an environment variable called `GOPATH`:
```bash
mkdir -p ~/gocode/src
export GOPATH=$HOME/gocode
```

Now, check out the source of dcs:

```bash
go get github.com/Debian/dcs/cmd/dcs-web
```

All dependencies will automatically be pulled in — the command above actually
not only checks out github.com/Debian/dcs, but tries to build cmd/dcs-web, the
web frontend.
