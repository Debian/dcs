# The DCS index

Debian Code Search requires an index in order to search through tons of source code efficiently. This index needs to be re-created whenever the source code changes — there is no incremental update due to the structure of the index. This document explains how to create a new index and how to update an existing index.

## Assumptions

1. There is a directory called `/dcs` in which all source and index files are stored.
2. DCS runs under a separate user account (called `dcs` in this document) which owns all files in `/dcs`.

## Creating a new index (first deployment)

Beware: These steps are untested. Please submit any corrections and/or ask if you have any questions.

First of all, we need to mirror the Debian archive:

```bash
$ cd /dcs
$ debmirror --diff=none --progress --verbose -a none --source -s main -h deb-mirror.de -r /debian source-mirror
```

Then, we use `dcs-unpack` to unpack every package in order to make its source code servable and indexable:

```bash
$ dcs-unpack \
    -mirrorPath=/dcs/source-mirror \
    -newUnpackPath=/dcs/unpacked \
    -oldUnpackPath=/invalid
```

Now that we have all source packages unpacked, we need to create the actual index. Depending on the size of your source code, you need to use more or less shards. Currently, I use 6 shards with about 1.2 GiB each. A shard cannot be larger than 2 GiB.

Also note that dcs-index creates a large amount of temporary data (many gigabytes, at least 7 GiB). If you have `/tmp` mounted as tmpfs, you might need to set `TMPDIR=/some/path` to place the temporary files in a different directory.

```bash
$ dcs-index \
    -mirrorPath=/dcs/ \
    -unpackedPath=/dcs/unpacked/ \
    -shards 6
```

Finally, start the index backend processes, the source backend process and dcs-web itself:

```bash
for i in $(seq 0 5); do
    systemctl start dcs-index-backend@$i.service
done
systemctl start dcs-source-backend.service
systemctl start dcs-web.service
```

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
1165,39s user 448,32s system 23% cpu 1:55:54,96 total
```

Now that we have all source packages unpacked, we need to create the actual index. Depending on the size of your source code, you need to use more or less shards. Currently, I use 6 shards with about 1.2 GiB each. A shard cannot be larger than 2 GiB.

Also note that dcs-index creates a large amount of temporary data (many gigabytes, at least 7 GiB). If you have `/tmp` mounted as tmpfs, you might need to set `TMPDIR=/some/path` to place the temporary files in a different directory.

```bash
$ dcs-index \
    -mirrorPath=/dcs/NEW/ \
    -unpackedPath=/dcs/unpacked-new/ \
    -shards 6
3418,80s user 1111,40s system 24% cpu 5:05:19,09 total
```

For the next step, it is recommended to create a simple shell script to automate the steps (and reduce downtime as much as possible). Note that the script hardcodes the amount of shards (the `seq 0 5` for 6 shards). Also, it is expected to run as root because it directly uses systemctl calls to restart the index backend processes.

```bash
#!/bin/zsh

mv /dcs/index.*.idx /dcs/OLD/
mv /dcs/NEW/index.*.idx /dcs/
mv /dcs/unpacked /dcs/OLD/unpacked
mv /dcs/unpacked-new /dcs/unpacked
for i in $(seq 0 5); do
    systemctl restart dcs-index-backend@$i.service
done
```

That’s it! Your index is now up to date. Verify that search still works and enjoy your new index.
