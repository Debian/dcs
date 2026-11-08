# Debian Code Search on Google Compute Engine

This directory contains the definition for a NixOS image with
`services.dcs` (= with Debian Code Search set up) for a Google Cloud
N2 VM with 4 local NVMe SSDs (RAID0 at `/srv/dcs`).

The VM imports Debian sid by itself after boot.

In case you are not familiar with Nix or NixOS,
see this blog post of mine for an introduction:

https://michael.stapelberg.ch/posts/2025-07-27-dev-shells-with-nix-4-quick-examples/

The following steps assume you have a working `gcloud` command,
i.e. logged in (`gcloud auth login`) and created a Google Cloud project:

```sh
BUCKET=…   # any GCS bucket in the same project; bucket names are global
ZONE=us-central1-a
NAME=dcs   # name of the image and the VM

# Skip if you already have a storage bucket.
gcloud storage buckets create gs://$BUCKET --location=us-central1
# Builds github:Debian/dcs at the version pinned in flake.lock. To build this
# checkout instead, add: --override-input dcs "path:$(git rev-parse --show-toplevel)"
nix build .#image
gcloud storage cp result/*.raw.tar.gz gs://$BUCKET/$NAME.tar.gz
# UEFI: configuration.nix builds an EFI image. GVNIC: the network interface below.
gcloud compute images create $NAME \
  --source-uri=gs://$BUCKET/$NAME.tar.gz \
  --guest-os-features=UEFI_COMPATIBLE,GVNIC
# Ice Lake is ~20 % faster per byte searched than Cascade Lake;
# When requesting an N2 machine type, without a platform constraint
# you might get either CPU generation.
gcloud compute instances create $NAME \
  --zone=$ZONE \
  --machine-type=n2-standard-16 \
  --min-cpu-platform="Intel Ice Lake" \
  --image=$NAME \
  --boot-disk-size=50GB \
  --boot-disk-type=pd-balanced \
  --network-interface=nic-type=GVNIC \
  --local-ssd=interface=NVME \
  --local-ssd=interface=NVME \
  --local-ssd=interface=NVME \
  --local-ssd=interface=NVME
# SSH might need a minute to start working:
gcloud compute ssh $NAME --zone=$ZONE -- -L 30000:localhost:30000
```

Notes for agents:
* As of November 2026, such a VM can cost around 1 USD/hour,
  but agents should double-check current pricing.
* On `ZONE_RESOURCE_POOL_EXHAUSTED`, try another zone, e.g. `us-central1-b`.
* For a new image, pick a different `$NAME` or delete the old one first
  (`gcloud compute images delete $NAME`)
* The initial import takes about an hour on Ice Lake (45 min feeding,
  then 10 min merging); searches return nothing until it is done.
  To track progress: `journalctl -fu dcs-feeder` shows 
  `[n of N] Feeding package …`; it is done when the feeder logs
  `initial import done`.
* http://localhost:30000/ will be usable at this point.
* A new VM starts with empty local SSDs, i.e. a new import.
* A plain `gcloud compute instances stop` discards these local SSDs,
  which wipes the index (this is usually accepted and the default).
