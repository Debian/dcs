#!/bin/zsh
# vim:ts=4:sw=4:expandtab
# © 2012 Michael Stapelberg
#
# Run this script in /dcs, it unpacks every source package from
# /dcs/source-mirror to /dcs/unpacked using dpkg-source.
#
#     cd /dcs && dcs-unpack

[ -d source-mirror ] || {
    echo 'Directory "source-mirror" not found, are you running this script in /dcs?' >&2
    exit 1
}

mkdir -p unpacked
for dsc in source-mirror/pool/main/**/*.dsc; do
    # Ensure this isn’t a -data package
    dir=$(dirname $dsc)
    [ "${dir%-data}" = "$dir" ] || continue

    echo "dsc file: $dsc"
    base=$(basename $dsc .dsc)
    dpkg-source --no-copy --no-check -x $dsc unpacked/$base
done
