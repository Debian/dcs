# Puts a RAID0 over all of a Google Compute Engine VM's local NVMe SSDs at
# cfg.mountPoint. Local SSDs come up blank on a new VM (or after a stop that
# discards them) and keep their data across reboots, so the array is created
# on first boot and assembled on every later one.
#
# The disk names come from google-guest-configs' udev rules, which number
# local SSDs by NVMe namespace. That matches n1/n2 (one controller, one
# namespace per SSD), not c3/c4/n4 (one controller per SSD).
{
  config,
  lib,
  pkgs,
  utils,
  ...
}:

let
  cfg = config.localSSD;
  disks = builtins.genList (i: "/dev/disk/by-id/google-local-nvme-ssd-${toString i}") cfg.count;
  diskUnits = map (disk: "${utils.escapeSystemdPath disk}.device") disks;
  mountUnit = "${utils.escapeSystemdPath cfg.mountPoint}.mount";
  # Starts as soon as /dev/md/data appears, while mdadm --create still
  # holds the array (mkfs: "apparently in use by the system").
  makefsUnit = "systemd-makefs@${utils.escapeSystemdPath "/dev/md/data"}.service";
in
{
  options.localSSD = {
    count = lib.mkOption {
      type = lib.types.ints.positive;
      description = "Number of local SSDs attached to the VM (gcloud --local-ssd).";
    };
    mountPoint = lib.mkOption {
      type = lib.types.str;
      description = "Where to mount the array.";
    };
  };

  config = {
    # Includes mdadm's udev rules, which assemble an existing array as its
    # disks appear.
    boot.swraid = {
      enable = true;
      # HOMEHOST <ignore>: the array is /dev/md/data, not
      # /dev/md/<hostname>:data (the guest agent sets the hostname late).
      mdadmConf = ''
        HOMEHOST <ignore>
        MAILADDR root
      '';
    };
    systemd.services.local-ssd-create = {
      description = "Create a RAID0 over blank local SSDs";
      unitConfig.DefaultDependencies = false;
      requires = diskUnits;
      after = diskUnits;
      before = [
        makefsUnit
        mountUnit
      ];
      requiredBy = [ mountUnit ];
      path = [
        pkgs.mdadm
        pkgs.kmod # mdadm loads md_mod
      ];
      serviceConfig = {
        Type = "oneshot";
        RemainAfterExit = true;
      };
      script = ''
        with=0
        for disk in ${lib.escapeShellArgs disks}; do
          if mdadm --examine "$disk" >/dev/null 2>&1; then
            with=$((with + 1))
          fi
        done
        if [ "$with" -eq ${toString cfg.count} ]; then
          echo "all disks belong to an array, which udev assembles"
          exit 0
        fi
        if [ "$with" -ne 0 ]; then
          echo "$with of ${toString cfg.count} disks belong to an array; not touching them" >&2
          exit 1
        fi
        mdadm --create /dev/md/data --level=0 --raid-devices=${toString cfg.count} ${lib.escapeShellArgs disks}
      '';
    };

    fileSystems.${cfg.mountPoint} = {
      device = "/dev/md/data";
      fsType = "ext4";
      autoFormat = true;
      # A failed mount would otherwise drop the VM into emergency mode,
      # where SSH does not work. nofail alone would also drop the ordering
      # before local-fs.target, so e.g. tmpfiles would run before the mount.
      options = [
        "nofail"
        "x-systemd.before=local-fs.target"
      ];
    };
  };
}
