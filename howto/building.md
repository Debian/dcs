# Building Debian Code Search

This document describes two main use cases: deploying DCS to a server and
setting up an environment in which you can easily hack on DCS.

## Building a Debian package: git-buildpackage

Just run `git-buildpackage --git-pbuilder` in the checked out dcs repository
and you will not have to install any dependencies.

In case you don’t want to use pbuilder, `git-buildpackage` will work, but
requires you to install the Debian packages (golang-codesearch-dev,
golang-pq-dev, …) on your machine. Note that the Debian packages are separate
from the build environment described below.

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

The `go(1)` tool will leave all the checked out git repositories in “detached
head” state, meaning a “git pull” will not pull new changes. To change this,
use:

```bash
cd $GOPATH/src/github.com/Debian/dcs
git checkout master
```
