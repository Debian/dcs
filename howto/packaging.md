# Creating a Debian package from DCS

The canonical way of deploying DCS is creating a Debian package and installing that on the machine to run DCS on. This document describes how to set up all the dependencies and create the Debian package.

## Installing and setting up Go

```bash
apt-get install -t experimental golang-go
apt-get install golang-pq-dev golang-godebiancontrol-dev golang-codesearch-dev
```

Go requires you to setup a directory in which all source code is stored. The location of that directory is stored in an environment variable called `GOPATH`:
```bash
mkdir ~/gocode
mkdir ~/gocode/src
export GOPATH=/home/michael/gocode
```

Now, check out the source of dcs:

```bash
go get github.com/Debian/dcs
```

## Creating the Debian package

We now create an upstream tarball from the git source, extract it and create a package via dpkg-buildpackage:
```bash
cd $GOPATH/src/dcs
git archive --format=tar.gz --prefix=dcs-0.1/ -o /tmp/dcs_0.1.orig.tar.gz HEAD
cd /tmp
tar xf dcs_0.1.orig.tar.gz
cd dcs-0.1
dpkg-buildpackage -b
```

You should now have a Debian package in `../dcs_0.1-1_amd64.deb` which you can deploy on your server.
