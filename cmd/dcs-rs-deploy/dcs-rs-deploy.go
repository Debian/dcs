package main

import (
	"flag"
	"fmt"
	"github.com/Debian/dcs/rackspace"
	"github.com/Debian/dcs/sshutil"
	"io/ioutil"
	"log"
	"strings"
	"time"
)

var (
	dcs0ServerId = flag.String("dcs0_server_id",
		"",
		"Skip creating a new server and use the specified id as dcs0 vm.")
	dcsIndexServerIds = flag.String("index_server_ids",
		"",
		"comma-separated list of index server IDs.")
	dcs0VolumeId = flag.String("dcs0_volume_id_dcs",
		"",
		"Skip creating a new block storage volume and use the specified id.")
	dcs0VolumeMirrorId = flag.String("dcs0_volume_id_mirror",
		"",
		"Skip creating a new block storage volume and use the specified id.")

	rs *rackspace.RackspaceClient
)

// All functions defined here directly use log.Fatal() without an OrDie suffix.

func attachBlockStorageVolume(serverId, volumeId, deviceNode string) {
	volume, err := rs.GetVolume(volumeId)
	if err != nil {
		log.Fatal(err)
	}
	volume = volume.BecomeAvailableOrDie(2 * time.Hour)

	if volume.Status() == "IN-USE" {
		log.Printf("Volume %s already attached, not attaching.\n", volumeId)
		return
	}

	log.Printf("Attaching volume %s to %s\n", volumeId, serverId)
	attachId, err := rs.AttachBlockStorage(serverId,
		rackspace.VolumeAttachmentRequest{
			Device:   deviceNode,
			VolumeId: volumeId,
		})
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("attachment id = %s\n", attachId)

	volume = volume.BecomeInUseOrDie(30 * time.Minute)
}

// Returns an sshutil.Client because it reconnects on fdisk failures, creating
// a new client.
func mountBlockStorage(client *sshutil.Client, partition, mountpoint string) *sshutil.Client {
	// device is “/dev/xvdg” if partition is “/dev/xvdg1”
	device := partition[:len(partition)-1]

	// Unmount all mounts within this mountpoint, then the mountpoint itself.
	grep := `cut -d ' ' -f 2 /proc/mounts | grep '^` + mountpoint + `' | tac`
	client.RunOrDie(`for mnt in $(` + grep + `); do umount $mnt; done || true`)

	// Create a partition unless a partition already exists.
	fdisk := `[ -e ` + partition + ` ] || echo "n\np\n\n\n\nw\n" | fdisk -S 32 -H 32 ` + device

	// When re-partitioning the disk on which the root filesystem is stored,
	// the kernel will not re-read the partition table and thus fdisk will
	// fail.
	// We reboot the machine and wait until it comes back before continuing.
	session, err := client.NewSession()
	if err != nil {
		log.Fatalf("Failed to create session in SSH connection: %v\n", err)
	}
	defer session.Close()
	log.Printf(`[SSH] Running "%s"`, fdisk)
	err = session.Run(fdisk)
	if err != nil {
		log.Printf(`Could not execute SSH command (%v) rebooting\n`, err)

		client.RunOrDie(`reboot`)
		// Save host because client may be garbage collected in the loop
		host := client.Host
		start := time.Now()
		for time.Since(start) < 10*time.Minute {
			time.Sleep(5 * time.Second)
			log.Printf("Trying to SSH in again…\n")
			client, err = sshutil.Connect(host)
			if err != nil {
				log.Printf("SSH error (retrying): %v\n", err)
			} else {
				break
			}
		}

		// TODO: what has helped at least once is triggering a reboot via the rackspace web interface.

		if client == nil {
			log.Fatal("Machine did not come back within 10 minutes after rebooting.")
		}

		if !client.Successful(`[ -e ` + partition + ` ]`) {
			log.Fatal("%s not there, even after a reboot!\n", partition)
		}
	}

	// Create (or overwrite!) an ext4 file system without lazy initialization
	// (so that the volume is usable immediately) and with SSD-friendly
	// parameters.
	// TODO: ^has_journal, barrier=0
	client.RunOrDie(`mkfs.ext4 -b 4096 -m 0 -E stride=128,stripe-width=128,lazy_itable_init=0,lazy_journal_init=0 ` + partition)

	client.RunOrDie(fmt.Sprintf("echo %s %s ext4 defaults,nofail,noatime,nodiratime 0 0 >> /etc/fstab", partition, mountpoint))
	client.RunOrDie(`mkdir -p ` + mountpoint)
	client.RunOrDie(`mount ` + mountpoint)

	return client
}

func main() {
	flag.Parse()

	var err error
	rs, err = rackspace.NewClient()
	if err != nil {
		log.Fatal("Could not create new rackspace client: %v\n", err)
	}

	serverId := *dcs0ServerId
	if serverId == "" {
		serverId = rs.ServerFromSnapshot("dcs-20g-systemd+dcs+psql",
			rackspace.CreateServerRequest{
				Name: "NEW-dcs-0",
				// This is a 4 GB standard instance
				FlavorRef: "5",
			})
	}

	log.Printf("-dcs0_server_id=%s\n", serverId)

	server, err := rs.GetServer(serverId)
	if err != nil {
		log.Fatal(err)
	}
	server = server.BecomeActiveOrDie(2 * time.Hour)

	// Attach an SSD block storage volume (unless one was specified)
	volumeId := *dcs0VolumeId
	if volumeId == "" {
		volumeId, err = rs.CreateBlockStorage(
			rackspace.CreateBlockStorageRequest{
				DisplayName: "NEW-dcs-src-0",
				Size:        220,
				VolumeType:  "SSD",
			})
		if err != nil {
			log.Fatal(err)
		}
	}

	// Attach an SSD block storage volume (unless one was specified)
	volumeMirrorId := *dcs0VolumeMirrorId
	if volumeMirrorId == "" {
		volumeMirrorId, err = rs.CreateBlockStorage(
			rackspace.CreateBlockStorageRequest{
				DisplayName: "NEW-dcs-mirror-0",
				// 100 GB is the minimum size. Last time we used 43 GB.
				Size:       100,
				VolumeType: "SSD",
			})
		if err != nil {
			log.Fatal(err)
		}
	}

	log.Printf("-dcs0_volume_id_dcs=%s\n", volumeId)
	log.Printf("-dcs0_volume_id_mirror=%s\n", volumeMirrorId)

	// We chose xvd[gh] so that it does not clash with xvda, xvdb and
	// whichever new devices a Rackspace base image might use in the
	// future :). We use a predictable path because it is easier than
	// figuring out the path in the subsequent automation.
	attachBlockStorageVolume(serverId, volumeId, "/dev/xvdg")
	attachBlockStorageVolume(serverId, volumeMirrorId, "/dev/xvdh")

	client, err := sshutil.Connect(server.AccessIPv6)
	if err != nil {
		log.Fatal(err)
	}

	// The /dcs/NEW/index.*.idx files are the artifact of the first stage, so
	// if they are present, skip the first stage entirely.
	if !client.Successful(`[ -s /dcs/NEW/index.0.idx ]`) {
		log.Printf("/dcs/NEW/index.0.idx not present, creating new index.\n")

		// Partition the remaining capacity on the server (≈ 160G) as on-disk
		// temporary space, since the temporary files exceed the 18G available in
		// /tmp on our base image.
		client = mountBlockStorage(client, "/dev/xvda2", "/tmp-disk")

		// Attach the SSD Block Storage volumes.
		client = mountBlockStorage(client, "/dev/xvdg1", "/dcs")
		client = mountBlockStorage(client, "/dev/xvdh1", "/dcs/source-mirror")

		client.RunOrDie("chown -R dcs.dcs /dcs/source-mirror")
		client.RunOrDie("chown -R dcs.dcs /dcs/")
		client.RunOrDie(`chmod 1777 /tmp-disk`)

		// TODO: timestamps after each step in update-index.sh would be nice
		client.WriteToFileOrDie("/dcs/update-index.sh", []byte(`#!/bin/sh
# Updates the source mirror, generates a new index, verifies it is serving and
# then swaps the old index with the new one.
#
# In case anything goes wrong, you can manually swap back the old index, see
# swap-index.sh

set -e

/bin/rm -rf /dcs/NEW /dcs/OLD /dcs/unpacked-new
/bin/mkdir /dcs/NEW /dcs/OLD

[ -d ~/.gnupg ] || mkdir ~/.gnupg
[ -e ~/.gnupg/trustedkeys.gpg ] || cp /usr/share/keyrings/debian-archive-keyring.gpg ~/.gnupg/trustedkeys.gpg

GOMAXPROCS=2 /usr/bin/dcs-debmirror -tcp_conns=20 >/tmp/fdm.log 2>&1
/usr/bin/debmirror --diff=none --method=http --rsync-extra=none -a none --source -s main -h http.debian.net -r /debian /dcs/source-mirror >/dev/null
/usr/bin/debmirror --diff=none --method=http --rsync-extra=none --exclude-deb-section=.* --include golang-mode --nocleanup -a none --arch amd64 -s main -h http.debian.net -r /debian /dcs/source-mirror >/dev/null

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

[ -d /dcs/unpacked ] && mv /dcs/unpacked /dcs/unpacked-old || true
mv /dcs/unpacked-new /dcs/unpacked
`))
		client.RunOrDie(`chmod +x /dcs/update-index.sh`)
		client.RunOrDie(`TMPDIR=/tmp-disk nohup su dcs -c "/bin/sh -c 'sh -x /dcs/update-index.sh >/tmp/update.log 2>&1 &'"`)

		// TODO: i also think we need some sort of lock here. perhaps let systemd run the updater?
	}

	log.Printf("Now waiting until /dcs/NEW/index.*.idx appear and are > 0 bytes\n")
	start := time.Now()
	errors := 0
	for time.Since(start) < 24*time.Hour {
		pollclient, err := sshutil.Connect(server.AccessIPv6)
		if err != nil {
			log.Printf("Non-fatal polling connection error: %v\n", err)
			errors++
			if errors > 30 {
				log.Fatal("More than 30 connection errors connecting to %s, giving up.\n", server.AccessIPv6)
			}
			continue
		}

		// TODO: flag for the number of shards
		shardsFound := `[ $(find /dcs/NEW/ -iname "index.*.idx" -size +0 -mmin +15 | wc -l) -eq 6 ]`
		if pollclient.Successful(shardsFound) {
			log.Printf("All shards present.\n")
			break
		}

		time.Sleep(15 * time.Minute)
	}

	var indexServerIds []string
	indexServerIds = strings.Split(*dcsIndexServerIds, ",")
	if *dcsIndexServerIds == "" {
		// TODO: flag for the number of shards/servers
		indexServerIds = make([]string, 6)

		for i := 0; i < len(indexServerIds); i++ {
			indexServerIds[i] = rs.ServerFromSnapshot("dcs-20g-systemd+dcs",
				rackspace.CreateServerRequest{
					Name: fmt.Sprintf("NEW-dcs-index-%d", i),
					// This is a 2 GB standard instance
					FlavorRef: "4",
				})
		}
	}

	log.Printf("-index_server_ids=%s\n", strings.Join(indexServerIds, ","))

	done := make(chan bool)
	indexServers := make([]rackspace.Server, len(indexServerIds))
	for i, _ := range indexServers {
		go func(i int) {
			server, err := rs.GetServer(indexServerIds[i])
			if err != nil {
				log.Fatal(err)
			}
			indexServers[i] = server.BecomeActiveOrDie(2 * time.Hour)
			done <- true
		}(i)
	}

	log.Printf("Waiting for all %d index servers to be available…\n",
		len(indexServerIds))
	for _ = range indexServers {
		<-done
	}

	log.Printf("Index servers available. Copying index…")

	pubkey, err := ioutil.ReadFile("/home/michael/.ssh/dcs-auto-rs")
	if err != nil {
		log.Fatal(err)
	}
	client.WriteToFileOrDie("~/.ssh/dcs-auto-rs", pubkey)
	client.RunOrDie("chmod 600 ~/.ssh/dcs-auto-rs")

	for i, server := range indexServers {
		go func(i int, server rackspace.Server) {
			// Create /dcs/
			indexclient, err := sshutil.Connect(server.AccessIPv6)
			if err != nil {
				log.Fatal("Failed to dial: " + err.Error())
			}
			indexclient.RunOrDie("mkdir -p /dcs/")
			if indexclient.Successful(fmt.Sprintf("[ -e /dcs/index.%d.idx ]", i)) {
				log.Printf("Index already present, skipping.\n")
				done <- true
				return
			}
			// “|| true” instead of “rm -f” because globbing fails when there are
			// no matching files.
			indexclient.RunOrDie("rm /dcs/index.*.idx || true")

			client.RunOrDie(
				fmt.Sprintf("scp -o StrictHostKeyChecking=no -i ~/.ssh/dcs-auto-rs /dcs/NEW/index.%d.idx root@%s:/dcs/",
					i,
					server.PrivateIPv4()))
			indexclient.RunOrDie(fmt.Sprintf("systemctl restart dcs-index-backend@%d.service", i))
			indexclient.RunOrDie(fmt.Sprintf("systemctl enable dcs-index-backend@%d.service", i))
			done <- true
		}(i, server)
	}
	log.Printf("Waiting for the index to be copied to all %d index servers…\n",
		len(indexServerIds))
	for _ = range indexServers {
		<-done
	}
	log.Printf("index copied!\n")

	backends := []string{}
	for i, server := range indexServers {
		backends = append(backends, fmt.Sprintf("%s:%d", server.PrivateIPv4(), 29080+i))
	}

	// TODO(longterm): configure firewall?

	client.RunOrDie("mkdir -p /etc/systemd/system/dcs-web.service.d/")

	client.WriteToFileOrDie(
		"/etc/systemd/system/dcs-web.service.d/backends.conf",
		[]byte(`[Service]
Environment=GOMAXPROCS=2
ExecStart=
ExecStart=/usr/bin/dcs-web \
    -template_pattern=/usr/share/dcs/templates/* \
	-listen_address=`+server.PrivateIPv4()+`:28080 \
	-use_sources_debian_net=true \
    -index_backends=`+strings.Join(backends, ",")))

	client.RunOrDie("systemctl daemon-reload")
	client.RunOrDie("systemctl enable dcs-web.service")
	client.RunOrDie("systemctl enable dcs-source-backend.service")
	client.RunOrDie("systemctl restart dcs-source-backend.service")
	client.RunOrDie("systemctl restart dcs-web.service")

	// Install and configure nginx.
	client.RunOrDie("DEBIAN_FRONTEND=noninteractive DEBCONF_NONINTERACTIVE_SEEN=true LC_ALL=C LANGUAGE=C LANG=C apt-get update")
	client.RunOrDie("DEBIAN_FRONTEND=noninteractive DEBCONF_NONINTERACTIVE_SEEN=true LC_ALL=C LANGUAGE=C LANG=C apt-get --force-yes -y install nginx")
	client.RunOrDie("rm /etc/nginx/sites-enabled/*")
	client.RunOrDie("mkdir -p /var/cache/nginx/cache")
	client.RunOrDie("mkdir -p /var/cache/nginx/tmp")
	client.RunOrDie("chown -R www-data.www-data /var/cache/nginx/")
	nginxHost, err := ioutil.ReadFile("/home/michael/gocode/src/github.com/Debian/dcs/nginx.example")
	if err != nil {
		log.Fatal(err)
	}
	// dcs-web is listening on the Rackspace ServiceNet (private) IP address.
	nginxReplaced := strings.Replace(string(nginxHost), "localhost:28080", server.PrivateIPv4()+":28080", -1)
	client.WriteToFileOrDie("/etc/nginx/sites-available/codesearch", []byte(nginxReplaced))
	client.RunOrDie("ln -s /etc/nginx/sites-available/codesearch /etc/nginx/sites-enabled/codesearch")
	client.RunOrDie("systemctl restart nginx.service")

	// Update DNS
	domainId, err := rs.GetDomainId("rackspace.zekjur.net")
	if err != nil {
		log.Fatal(err)
	}

	records, err := rs.GetDomainRecords(domainId)
	if err != nil {
		log.Fatal(err)
	}

	var updates []rackspace.Record
	for _, record := range records {
		if record.Name == "codesearch.rackspace.zekjur.net" {
			log.Printf("record %v\n", record)
			newIp := server.AccessIPv4
			if record.Type == "AAAA" {
				newIp = server.AccessIPv6
			}
			updates = append(updates,
				rackspace.Record{
					Id:   record.Id,
					Name: record.Name,
					Data: newIp,
				})
		} else if record.Name == "int-dcs-web.rackspace.zekjur.net" {
			// This record points to the private IPv4 address, used by our
			// monitoring.
			log.Printf("record %v\n", record)
			newIp := server.PrivateIPv4()
			updates = append(updates,
				rackspace.Record{
					Id:   record.Id,
					Name: record.Name,
					Data: newIp,
				})
		} else if record.Name == "int-dcs-source-backend.rackspace.zekjur.net" {
			// This record points to the private IPv4 address, used by our
			// monitoring.
			log.Printf("record %v\n", record)
			newIp := server.PrivateIPv4()
			updates = append(updates,
				rackspace.Record{
					Id:   record.Id,
					Name: record.Name,
					Data: newIp,
				})
		}
	}

	if err := rs.UpdateRecords(domainId, updates); err != nil {
		log.Fatal(err)
	}

	// TODO: reverse dns for the server

	log.Printf(`
codesearch was deployed to:
http://codesearch.rackspace.zekjur.net/
http://[%s]/
http://%s/
`, server.AccessIPv6, server.AccessIPv4)
}
