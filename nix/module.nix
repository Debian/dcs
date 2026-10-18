self:
{
  config,
  lib,
  pkgs,
  ...
}:

let
  cfg = config.services.dcs;
in
{
  options.services.dcs = {
    enable = lib.mkEnableOption "Debian Code Search";

    package = lib.mkOption {
      type = lib.types.package;
      default = self.packages.${pkgs.stdenv.hostPlatform.system}.default;
      description = "The dcs package to use";
    };
  };

  config = lib.mkIf cfg.enable {
    # Shared systemd slice for all DCS services,
    # so that we can easily apply one global memory limit.
    systemd.slices.dcs = {
      description = "Debian Code Search";
      sliceConfig.MemoryMax = lib.mkDefault "52G";
    };
  };
}
