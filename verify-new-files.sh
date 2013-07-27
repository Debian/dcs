#!/bin/zsh

set -e

# Bring up a new batch stack. We overwrite the -max_stacks flag: by default it
# is 5. This way, we can always bring up a batch stack, even if there are 5
# batch queries running currently.
STACK=$(dcs-batch-helper -max_stacks=6 create)
if [ $? -ne 0 ]
then
	echo "Could not create a new batch stack" >&2
	exit 1
fi

# Now point the source-backend to /dcs/unpacked-new instead of /dcs/unpacked.
EXECSTART=$(perl -nlE 'say if /^ExecStart=.*[^\\]$/ || /^ExecStart=/ .. /[^\\]$/' < /run/systemd/system/dcs-batch${STACK}-source-backend.service | sed 's,/dcs/unpacked,/dcs/unpacked-new,g')

mkdir /run/systemd/system/dcs-batch${STACK}-source-backend.service.d
cat >/run/systemd/system/dcs-batch${STACK}-source-backend.service.d/args.conf <<EOT
[Service]
ExecStart=
${EXECSTART}
EOT

systemctl daemon-reload
systemctl restart dcs-batch${STACK}-source-backend.service

# And point the index-backends to /dcs/NEW/*.idx instead of /dcs/*.idx
EXECSTART=$(perl -nlE 'say if /^ExecStart=.*[^\\]$/ || /^ExecStart=/ .. /[^\\]$/' < /run/systemd/system/dcs-batch${STACK}-index-backend@.service | sed 's,/dcs/index,/dcs/NEW/index,g')
for backend in $(systemctl list-units --full | grep "^dcs-batch${STACK}-index-backend" | cut -f 1 -d ' ')
do
	mkdir /run/systemd/system/${backend}.d
	cat >/run/systemd/system/${backend}.d/args.conf <<EOT
[Service]
ExecStart=
${EXECSTART}
EOT
	systemctl daemon-reload
	systemctl restart $backend
done

# TODO: remove this once dcs-web is socket-activated.
sleep 10

# Verify the new files are okay.
# As we discover problems, more checks could be added here, but this one seems fine for now.
if ! wget -qO- "http://localhost:30${STACK}20/search?q=i3Font" | grep -q 'i3Font load_font'
then
	echo "Stack not serving correctly, /search?q=i3Font did not contain 'i3Font load_font'" >&2
	exit 1
fi

# Delete the drop-ins and destroy the batch stack.
rm -rf /run/systemd/system/dcs-batch${STACK}-source-backend.service.d
for backend in $(systemctl list-units --full | grep "^dcs-batch${STACK}-index-backend" | cut -f 1 -d ' ')
do
	rm -rf /run/systemd/system/${backend}.d
done

dcs-batch-helper destroy $STACK
