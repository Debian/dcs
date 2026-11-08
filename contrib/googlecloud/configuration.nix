{
  config,
  pkgs,
  dcs,
  lib,
  modulesPath,
  ...
}:

{
  imports = [
    "${modulesPath}/virtualisation/google-compute-image.nix"
    ./local-ssd.nix
    dcs.nixosModules.default
  ];

  virtualisation.googleComputeImage.efi = true;

  # Required for gcloud compute ssh
  #
  # TODO: Is this the best way to log in?
  security.googleOsLogin.enable = lib.mkForce false;
  nix.settings.trusted-users = [ "@google-sudoers" ];

  localSSD = {
    count = 4;
    mountPoint = config.services.dcs.dataDir;
  };

  services.dcs = {
    enable = true;
    # All N2 CPU platforms (Cascade Lake, Ice Lake) support AVX-512.
    goAMD64 = "v4";
    numShards = 6;
  };

  # Lower readahead from 4 MiB (md RAID0 default) to 128 KiB:
  # Most candidate files are much smaller.
  services.udev.extraRules = ''
    ACTION=="add|change", SUBSYSTEM=="block", KERNEL=="md*", ATTR{queue/read_ahead_kb}="128"
  '';

  environment.systemPackages = [
    config.services.dcs.package # for tools like dcs(1)
    pkgs.fio # for benchmarking disk performance
  ];

  # This value determines the NixOS release from which the default
  # settings for stateful data, like file locations and database versions
  # on your system were taken. It‘s perfectly fine and recommended to leave
  # this value at the release version of the first install of this system.
  # Before changing this value read the documentation for this option
  # (e.g. man configuration.nix or on https://nixos.org/nixos/options.html).
  system.stateVersion = "26.05"; # Did you read the comment?
}
