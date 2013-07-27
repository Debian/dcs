#!/bin/zsh

mv /dcs/index.*.idx /dcs/OLD/
mv /dcs/NEW/index.*.idx /dcs/
mv /dcs/unpacked /dcs/OLD/unpacked
mv /dcs/unpacked-new /dcs/unpacked
for i in $(seq 0 5); do
	systemctl restart dcs-index-backend@$i.service
done
