# The DCS index

Debian Code Search requires an index in order to search through tons of source code efficiently. This index needs to be re-created whenever the source code changes — there is no incremental update due to the structure of the index. This document explains how to create a new index and how to update an existing index.

## Assumptions

1. There is a directory called `/dcs` in which all source and index files are stored.
2. DCS runs under a separate user account (called `dcs` in this document) which owns all files in `/dcs`.

## Creating a new index (first deployment)



## Updating an existing index

First of all, change to `/dcs` and re-create the NEW/OLD folders. As explained in the previous section, I like to leave them around just in case something is wrong with the new index.

```bash
$ cd /dcs
$ rm -rf NEW OLD
20,69s user 184,01s system 12% cpu 27:20,75 total
$ mkdir NEW OLD
```

Then, update your copy of the Debian source mirror. This should not take much more than 15 minutes when using a high-speed mirror.

```bash
$ debmirror --progress --verbose -a none --source -s main -h deb-mirror.de -r /debian source-mirror
97,56s user 110,12s system 26% cpu 12:50,20 total
```

Now, `dcs-unpack` creates a new folder called `unpacked-new` which contains the unpacked source mirror. To save time and space, packages that have not changed from the last index will be hard-linked.

```bash
$ dcs-unpack \
    -mirrorPath=/dcs/source-mirror \
    -newUnpackPath=/dcs/unpacked-new \
    -oldUnpackPath=/dcs/unpacked
```
