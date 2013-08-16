# Setting up a small instance of DCS for hacking

This document describes how to set up a small instance of DCS running on one
single machine with a very small index, so that you can quickly get it running
and start testing your changes to the source code.

## Creating the PostgreSQL tables

DCS needs two databases: udd (Ultimate Debian Database) and dcs. Only the
popcon information is used from udd, so we don’t need a full mirror (in case
you already have one, you can use it).

```bash
apt-get install postgresql
su - postgres
createuser udd
createuser dcs
createdb -T template0 -E SQL_ASCII udd
createdb -E utf8 -O dcs dcs
```

Afterwards, verify that you, under your own user account, can access that
database and create the table schema:

```bash
psql < $GOPATH/github.com/Debian/dcs/schema.sql
```

## Filling the PostgreSQL tables

The best way is to donwload the udd-popcon dump and import it. In case you
cannot do that, an alternative is to just insert the following rows manually:

```sql
INSERT INTO popcon (package, insts, vote, olde, recent, nofiles)
VALUES ('i3', 728, 0, 0, 0, 728);
INSERT INTO popcon (package, insts, vote, olde, recent, nofiles)
VALUES ('i3-wm', 1245, 401, 388, 456, 0);
INSERT INTO popcon (package, insts, vote, olde, recent, nofiles)
VALUES ('i3-wm-dbg', 17, 0, 11, 6, 0);
```

Be aware that when working on anything ranking-related, you need the full
import. With only the rows above, the numbers will not reflect the actual
numbers used in production.

## Creating the mirror directories

```bash
export MIRRORPATH=/tmp/mini
export DCSPATH=/tmp/dcs-mini

mkdir -p $MIRRORPATH
mkdir -p $DCSPATH
cd $GOPATH/src/github.com/Debian/dcs/
cp -r testdata/* $MIRRORPATH
```

## Indexing the mirror

```bash
$GOPATH/bin/dcs-unpack \
    -mirror_path=$MIRRORPATH \
    -new_unpacked_path=$DCSPATH/unpacked \
    -old_unpacked_path=

$GOPATH/bin/compute-ranking \
    -mirror_path=$MIRRORPATH \
    -verbose=true

$GOPATH/bin/dcs-index \
    -index_shard_path=$DCSPATH \
    -unpacked_path=$DCSPATH/unpacked/
```

## Running the service

Normally you’d use the provided .service files, but all the paths are wrong.
Hence, let’s run the necessary processes directly:

```bash
$GOPATH/bin/index-backend \
    -index_path=$DCSPATH/index.0.idx

$GOPATH/bin/source-backend \
    -unpacked_path=$DCSPATH/unpacked/

$GOPATH/bin/dcs-web \
    -template_pattern=$GOPATH/src/github.com/Debian/dcs/cmd/dcs-web/templates/* \
    -static_path=$GOPATH/src/github.com/Debian/dcs/static/
```
