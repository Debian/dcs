# Creating a Debian package from DCS

The canonical way of deploying DCS is creating a Debian package and installing that on the machine to run DCS on. This document describes how to set up all the dependencies and create the Debian package.

## Installing Go

```bash
apt-get install golang-go
```

Go requires you to setup a directory in which all source code is stored. The location of that directory is stored in an environment variable called `GOPATH`:
```bash
mkdir ~/gocode
mkdir ~/gocode/src
export GOPATH=/home/michael/gocode
```

Now, check out the source of dcs:

```bash
cd ~/gocode/src
git clone git://github.com/debiancodesearch/dcs.git
```

Then, install all the dependencies:
```bash
go get github.com/jbarham/gopgsqldriver
go get github.com/mstap/godebiancontrol
go get code.google.com/p/codesearch
```

Note that gopgsqldriver currently does not compile cleanly due to [libpq-fe.h being in a different location on Debian](https://github.com/jbarham/gopgsqldriver/issues/4) than on the author’s machine apparently. Let’s fix that:
```bash
cd github.com/jbarham/gopgsqldriver
sed -i 's,#include <libpq-fe.h>,#include <postgresql/libpq-fe.h>,g' pgdriver.go
```

We now create an upstream tarball from the git source, extract it and create a package via dpkg-buildpackage:
```bash
cd $GOPATH/src/dcs
git archive --format=tar.gz --prefix=dcs-0.1/src/dcs/ -o /tmp/dcs_0.1.orig.tar.gz HEAD
cd /tmp
tar xf dcs_0.1.orig.tar.gz
cd dcs-0.1
mv src/dcs/debian .
dpkg-buildpackage -b
```

You should now have a Debian package in `../dcs_0.1-1_amd64.deb` which you can deploy on your server.
