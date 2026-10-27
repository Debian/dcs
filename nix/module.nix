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
  init = pkgs.writeShellScriptBin "dcs-init" initSrc;

  webSrc = ''
    set -eu
    exec ${cfg.package}/bin/dcs-web \
      -source_indexes=${cfg.web.sourceIndexes} \
      -ranking_data_path=ranking.json \
      -tls_cert_path=prod-cert.pem \
      -tls_key_path=prod-key.pem \
      -tls_require_client_auth=false \
      -listen_address=localhost:30443 \
      -listen_address_http=localhost:30000 \
      -securecookie_hash_key=$(cat hash.key) \
      -securecookie_block_key=$(cat block.key)${
        lib.concatMapStrings (flag: " \\\n  ${flag}") cfg.web.extraFlags
      }
  '';
  # https://wiki.nixos.org/wiki/Nix-writers
  web = pkgs.writeShellScriptBin "dcs-web" webSrc;

  computeRankingSrc = ''
    set -eu
    exec ${cfg.package}/bin/dcs-compute-ranking \
      -output_path=ranking.json${
        lib.concatMapStrings (flag: " \\\n  ${flag}") cfg.computeRanking.extraFlags
      }
  '';
  computeRanking = pkgs.writeShellScriptBin "dcs-compute-ranking" computeRankingSrc;

  importerSrc = ''
    set -eu
    shard="$1"
    exec ${cfg.package}/bin/dcs-package-importer \
      -listen_address=localhost:$((21010+shard)) \
      -shard_path=/srv/dcs/shard$shard \
      -tls_cert_path=prod-cert.pem \
      -tls_key_path=prod-key.pem \
      -source_backend=${lib.concatMapStrings (flag: " \\\n  ${flag}") cfg.importer.extraFlags}
  '';
  importer = pkgs.writeShellScriptBin "dcs-package-importer" importerSrc;

  defaultServiceConfig = {
    # Run under a dedicated user/group.
    User = "dcs";
    Group = "dcs";
    WorkingDirectory = "/srv/dcs";

    # All DCS services go into dcs.slice, so that
    # resource dimensions can be controlled in one place.
    Slice = "dcs.slice";
  };
in
{
  options.services.dcs = {
    enable = lib.mkEnableOption "Debian Code Search";

    package = lib.mkOption {
      type = lib.types.package;
      default = self.packages.${pkgs.stdenv.hostPlatform.system}.default;
      description = "The dcs package to use";
    };

    numShards = lib.mkOption {
      type = lib.types.ints.between 1 10;
      default = 1; # codesearch.debian.net uses 6 shards
      description = "Number of index shards (one dcs-package-importer@N per shard)";
    };

    web.sourceIndexes = lib.mkOption {
      type = lib.types.str;
      default = lib.concatMapStringsSep "," (i: "/srv/dcs/shard${toString i}") (
        builtins.genList (i: i) cfg.numShards
      );
      example = "/srv/dcs/shard0,/srv/dcs/shard1";
      description = "Comma-separated index shard directories (containing src/ and full/) that dcs-web serves";
    };

    web.extraFlags = lib.mkOption {
      type = lib.types.listOf lib.types.str;
      default = [ ];
      example = [
        "-use_positional_index"
        "-securecookie_hash_key=$(cat $CREDENTIALS_DIRECTORY/securecookie_hash_key)"
      ];
      description = ''
        Additional dcs-web flags (inserted verbatim, i.e. $(cat …) works).
      '';
    };

    computeRanking.extraFlags = lib.mkOption {
      type = lib.types.listOf lib.types.str;
      default = [ ];
      example = [
        "-mirror_url=http://ftp.ch.debian.org/debian"
      ];
      description = ''
        Additional dcs-compute-ranking flags (inserted verbatim, i.e. $(cat …) works).
      '';
    };

    importer.extraFlags = lib.mkOption {
      type = lib.types.listOf lib.types.str;
      default = [ ];
      example = [
        "-debug_skip"
      ];
      description = ''
        Additional dcs-package-importer flags (inserted verbatim, i.e. $(cat …) works).
      '';
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

    systemd.timers.dcs-compute-ranking = {
      description = "derive ranking.json from Debian popcon (hourly timer)";
      wantedBy = [ "timers.target" ];
      timerConfig = {
        OnActiveSec = "1min";
        OnCalendar = "hourly";
      };
    };

    systemd.services.dcs-compute-ranking = {
      description = "derive ranking.json from Debian popcon";
      documentation = [ "https://github.com/Debian/dcs" ];
      serviceConfig = defaultServiceConfig // {
        Type = "oneshot";
        ExecStart = "${computeRanking}/bin/dcs-compute-ranking";
      };
    };

    systemd.services.dcs-init = {
      description = "create certificates, secret keys, etc.";
      documentation = [ "https://github.com/Debian/dcs" ];
      serviceConfig = defaultServiceConfig // {
        Type = "oneshot";
        ExecStart = "${init}/bin/dcs-init";
      };
    };

    systemd.services."dcs-package-importer@" = {
      description = "dcs-package-importer (shard %i)";
      documentation = [ "https://github.com/Debian/dcs" ];
      # No wantedBy: units like dcs-package-importer@N instantiate.
      requires = [ "dcs-init.service" ];
      after = [ "dcs-init.service" ];
      path = with pkgs; [
        dpkg # for dpkg-source
        patch
        gzip # for gunzip
        bzip2 # for bzip2
        xz # for xz
        zstd # for zstd
      ];
      # Do not rate limit restarts; DCS is safe to restart
      # and will auto-recover after being killed.
      #
      # https://michael.stapelberg.ch/posts/2024-01-17-systemd-indefinite-service-restarts/
      startLimitIntervalSec = 0;
      serviceConfig = defaultServiceConfig // {
        Restart = "always";
        RestartSec = 1;

        # Defer to any other work, updating the index
        # is background work compared to serving queries.
        Nice = "19";
        IOSchedulingClass = "idle";

        # Disable stdout (very verbose); only enable when debugging.
        # TODO: reduce logspam to make this setting obsolete.
        StandardOutput = "null";
        StandardError = "journal";

        Environment = [
          # Write a core dump when panicing (systemd-coredump(8) collects).
          #
          # https://michael.stapelberg.ch/posts/2024-10-22-debug-go-core-dumps-delve-export-bytes/
          "GOTRACEBACK=crash"
        ];

        ExecStart = "${importer}/bin/dcs-package-importer %i";
      };
    };

    systemd.targets."dcs-package-importers" = {
      description = "all dcs-package-importer instances";
      wantedBy = [ "multi-user.target" ];
      # Enable all dcs-package-importer@0 to @N (for numShards).
      wants = builtins.genList (i: "dcs-package-importer@${toString i}.service") cfg.numShards;
    };

    systemd.services.dcs-web = {
      description = "dcs-web (frontend)";
      documentation = [ "https://github.com/Debian/dcs" ];
      wantedBy = [ "multi-user.target" ];
      requires = [ "dcs-init.service" ];
      after = [ "dcs-init.service" ];
      # Do not rate limit restarts; DCS is safe to restart
      # and will auto-recover after being killed.
      #
      # https://michael.stapelberg.ch/posts/2024-01-17-systemd-indefinite-service-restarts/
      startLimitIntervalSec = 0;
      serviceConfig = defaultServiceConfig // {
        Restart = "always";
        RestartSec = 1;

        # Disable stdout (very verbose); only enable when debugging.
        # TODO: reduce logspam to make this setting obsolete.
        StandardOutput = "null";
        StandardError = "journal";

        Environment = [
          # Write a core dump when panicing (systemd-coredump(8) collects).
          #
          # https://michael.stapelberg.ch/posts/2024-10-22-debug-go-core-dumps-delve-export-bytes/
          "GOTRACEBACK=crash"
        ];

        ExecStart = "${web}/bin/dcs-web";
      };
    };
  };
}
