self:
{
  config,
  lib,
  pkgs,
  ...
}:

let
  cfg = config.services.dcs;

  initSrc = ''
    set -eu
    [ -e prod-cert.pem ] || ${cfg.package}/bin/genprodcerts
    [ -e hash.key ] || head -c32 /dev/urandom | basenc --base16 -w0 > hash.key
    [ -e block.key ] || head -c32 /dev/urandom | basenc --base16 -w0 > block.key
    [ -e ranking.json ] || echo '{}' > ranking.json
  '';
  init = pkgs.writeShellScript "dcs-init" initSrc;

  webSrc = ''
    set -eu
    exec ${cfg.package}/bin/dcs-web \
      -source_indexes=/srv/dcs/shard0 \
      -ranking_data_path=ranking.json \
      -tls_cert_path=prod-cert.pem \
      -tls_key_path=prod-key.pem \
      -tls_require_client_auth=false \
      -listen_address=localhost:30443 \
      -listen_address_http=localhost:30000 \
      -securecookie_hash_key=$(cat hash.key) \
      -securecookie_block_key=$(cat block.key)
  '';
  web = pkgs.writeShellScript "dcs-web" webSrc;
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

    users.groups.dcs = { };
    users.users.dcs = {
      isSystemUser = true;
      group = "dcs";
    };

    systemd.tmpfiles.rules = [
      "d /srv/dcs 0755 dcs dcs -"
    ];

    systemd.services.dcs-web = {
      wantedBy = [ "multi-user.target" ];
      serviceConfig = {
        User = "dcs";
        Group = "dcs";
        Slice = "dcs.slice";
        WorkingDirectory = "/srv/dcs";
        ExecStartPre = init;
        ExecStart = web;
      };
    };
  };
}
