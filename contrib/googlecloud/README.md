# Debian Code Search on Google Compute Engine

NixOS image with `services.dcs` for an N2 VM with 4 local NVMe SSDs
(RAID0 at `/srv/dcs`). The VM imports Debian sid by itself after boot.

```sh
BUCKET=…   # any GCS bucket in the same project
ZONE=us-central1-a

# Builds github:Debian/dcs at the version pinned in flake.lock. To build this
# checkout instead, add: --override-input dcs "path:$(git rev-parse --show-toplevel)"
nix build .#image
gcloud storage cp result/*.raw.tar.gz gs://$BUCKET/dcs.tar.gz
# UEFI: configuration.nix builds an EFI image. GVNIC: the network interface below.
gcloud compute images create dcs \
  --source-uri=gs://$BUCKET/dcs.tar.gz \
  --guest-os-features=UEFI_COMPATIBLE,GVNIC
# Ice Lake is ~20 % faster per byte searched than Cascade Lake;
# When requesting an N2 machine type, without a platform constraint
# you might get either CPU generation.
gcloud compute instances create dcs \
  --zone=$ZONE \
  --machine-type=n2-standard-16 \
  --min-cpu-platform="Intel Ice Lake" \
  --image=dcs \
  --boot-disk-size=50GB \
  --boot-disk-type=pd-balanced \
  --network-interface=nic-type=GVNIC \
  --local-ssd=interface=NVME \
  --local-ssd=interface=NVME \
  --local-ssd=interface=NVME \
  --local-ssd=interface=NVME
gcloud compute ssh dcs --zone=$ZONE -- -L 30000:localhost:30000
```

Notes for agents:
* On `ZONE_RESOURCE_POOL_EXHAUSTED`, try another zone, e.g. `us-central1-b`.
* For a new image, delete the old one first: `gcloud compute images delete dcs`
* The initial import takes about an hour on Ice Lake (45 min feeding,
  then 10 min merging); searches return nothing until it is done.
  To track progress: `journalctl -fu dcs-feeder` shows 
  `[n of N] Feeding package …`; it is done when the feeder logs
  `initial import done`.
* http://localhost:30000/ will be usable at this point.
* A new VM starts with empty local SSDs, i.e. a new import.
* A plain `gcloud compute instances stop` discards these local SSDs.
