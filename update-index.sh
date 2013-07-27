#!/bin/sh
# Updates the source mirror, generates a new index, verifies it is serving and
# then swaps the old index with the new one.
#
# In case anything goes wrong, you can manually swap back the old index, see
# swap-index.sh

set -e

/bin/rm -rf /dcs/NEW /dcs/OLD
/bin/mkdir /dcs/NEW /dcs/OLD

/usr/bin/debmirror --diff=none -a none --source -s main -h deb-mirror.de -r /debian /dcs/source-mirror >/dev/null
/usr/bin/debmirror --diff=none --exclude-deb-section=.* --include golang-mode --nocleanup -a none --arch amd64 -s main -h deb-mirror.de -r /debian /dcs/source-mirror >/dev/null

POPCONDUMP=$(mktemp)
if ! wget -q http://udd.debian.org/udd-popcon.sql.xz -O $POPCONDUMP
then
	wget -q http://public-udd-mirror.xvm.mit.edu/snapshots/udd-popcon.sql.xz -O $POPCONDUMP
fi
echo 'DROP TABLE popcon; DROP TABLE popcon_src;' | psql udd
xz -d -c $POPCONDUMP | psql udd
rm $POPCONDUMP

/usr/bin/compute-ranking \
	-mirror_path=/dcs/source-mirror

/usr/bin/dcs-unpack \
	-mirror_path=/dcs/source-mirror \
	-new_unpacked_path=/dcs/unpacked-new \
	-old_unpacked_path=/dcs/unpacked >/dev/null

/usr/bin/dcs-index \
	-index_shard_path=/dcs/NEW/ \
	-unpacked_path=/dcs/unpacked-new/ \
	-shards 6 >/dev/null

# cat > /etc/sudoers.d/dcs-verify-new-files <<EOT
# dcs ALL=(ALL:ALL) NOPASSWD: /dcs/verify-new-files.sh
# EOT
# cat > /etc/sudoers.d/dcs-swap-index <<EOT
# dcs ALL=(ALL:ALL) NOPASSWD: /dcs/swap-index.sh
# EOT
sudo /dcs/verify-new-files.sh && sudo /dcs/swap-index.sh
