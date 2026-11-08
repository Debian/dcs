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
      -use_positional_index \
      -query_results_path=${cfg.dataDir}/tmp/queryresults \
      -source_indexes=${cfg.web.sourceIndexes} \
      -ranking_data_path=ranking.json \
      -tls_cert_path=prod-cert.pem \
      -tls_key_path=prod-key.pem \
      -tls_require_client_auth=false \
      -listen_address=localhost:30443 \
      -listen_address_http=localhost:30000 \
      -securecookie_hash_key=hash.key \
      -securecookie_block_key=block.key${
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
    # -source_backend is intentionally empty:
    # dcs-web runs the sourcebackends in-process in this Nix module.
    exec ${cfg.package}/bin/dcs-package-importer \
      -listen_address=localhost:$((21010+shard)) \
      -shard_path=${cfg.dataDir}/shard$shard \
      -tls_cert_path=prod-cert.pem \
      -tls_key_path=prod-key.pem \
      -source_backend=${lib.concatMapStrings (flag: " \\\n  ${flag}") cfg.importer.extraFlags}
  '';
  importer = pkgs.writeShellScriptBin "dcs-package-importer" importerSrc;

  keyring = self.packages.${pkgs.stdenv.hostPlatform.system}.debian-archive-keyring;

  feederSrc = ''
    set -eu
    exec ${cfg.package}/bin/dcs-feeder \
      -shards=${cfg.feeder.shards} \
      -tls_cert_path=prod-cert.pem \
      -tls_key_path=prod-key.pem \
      -debian_archive_keyring=${keyring}${
        lib.concatMapStrings (flag: " \\\n  ${flag}") cfg.feeder.extraFlags
      }
  '';
  feeder = pkgs.writeShellScriptBin "dcs-feeder" feederSrc;

  defaultServiceConfig = {
    # Run under a dedicated user/group.
    User = "dcs";
    Group = "dcs";
    WorkingDirectory = cfg.dataDir;

    # All DCS services go into dcs.slice, so that
    # resource dimensions can be controlled in one place.
    Slice = "dcs.slice";

    # Remove all capabilities(7):
    CapabilityBoundingSet = "";

    # Ensure the service can never gain new privileges:
    NoNewPrivileges = true;

    # Prohibit access to any kind of namespacing:
    RestrictNamespaces = true;

    # Make home directories inaccessible:
    ProtectHome = true;

    # Make device nodes except for /dev/null, /dev/zero, /dev/full,
    # /dev/random and /dev/urandom inaccessible:
    PrivateDevices = true;

    # Make users other than root and the user for this daemon inaccessible:
    PrivateUsers = true;

    # Make cgroup file system hierarchy inaccessible:
    ProtectControlGroups = true;

    # Deny kernel module loading:
    ProtectKernelModules = true;

    # Make kernel variables (e.g. /proc/sys) read-only:
    ProtectKernelTunables = true;

    # Hide processes of other users in /proc:
    ProtectProc = "invisible";

    # Filter dangerous system calls. The following is listed as safe basic
    # choice in systemd.exec(5):
    SystemCallArchitectures = "native";
    SystemCallFilter = [ "@system-service" ];

    # Make the entire file system read-only, except for the data directory:
    ProtectSystem = "strict";
    ReadWritePaths = [ cfg.dataDir ];

    # Use a private /tmp, not shared with other services:
    PrivateTmp = true;

    # Make dmesg inaccessible:
    ProtectKernelLogs = true;

    # Prohibit changing the system clock:
    ProtectClock = true;

    # Prohibit changing the hostname:
    ProtectHostname = true;

    # Prohibit acquiring realtime scheduling:
    RestrictRealtime = true;

    # Prohibit creating setuid / setgid files:
    RestrictSUIDSGID = true;

    # Prohibit changing the execution domain:
    LockPersonality = true;

    # Prohibit memory mappings that are writable and executable:
    MemoryDenyWriteExecute = true;

    # Prohibit obscure socket families (allow only IP and Unix sockets):
    RestrictAddressFamilies = [
      "AF_INET"
      "AF_INET6"
      "AF_UNIX"
    ];
  };

  defaultDaemonServiceConfig = defaultServiceConfig // {
    # Do not rate limit restarts: All DCS daemons are safe to restart
    # and will auto-recover after being killed. This also needs
    # startLimitIntervalSec = 0; on the service attrset itself.
    #
    # https://michael.stapelberg.ch/posts/2024-01-17-systemd-indefinite-service-restarts/
    Restart = "always";
    RestartSec = 1;

    Environment = [
      # Write a core dump when panicing (systemd-coredump(8) collects).
      #
      # https://michael.stapelberg.ch/posts/2024-10-22-debug-go-core-dumps-delve-export-bytes/
      "GOTRACEBACK=crash"
    ];
  };
in
{
  options.services.dcs = {
    enable = lib.mkEnableOption "Debian Code Search";

    goAMD64 = lib.mkOption {
      type = lib.types.enum [
        "v1"
        "v2"
        "v3"
        "v4"
      ];
      default = "v3";
      description = ''
        GOAMD64 microarchitecture level to compile for.
        Can be lowered to "v1" for widest binary compatibility.
        Default "v3", which is the recommendation.
        Bump to "v4" if your destination machine has AVX512.
        For more details, see:
        https://michael.stapelberg.ch/posts/2026-09-06-dcs-fast-turbopfor-go-simd/#goamd64
      '';
    };

    package = lib.mkOption {
      type = lib.types.package;
      default = self.packages.${pkgs.stdenv.hostPlatform.system}.default.override {
        inherit (cfg) goAMD64;
      };
      defaultText = lib.literalExpression ''
        dcs.packages.''${pkgs.stdenv.hostPlatform.system}.default.override {
          goAMD64 = config.services.dcs.goAMD64;
        }
      '';
      description = "The dcs package to use";
    };

    dataDir = lib.mkOption {
      type = lib.types.path;
      default = "/srv/dcs";
      description = "Directory for all DCS files (needs about 1 TB)";
    };

    numShards = lib.mkOption {
      type = lib.types.ints.between 1 10;
      default = 1; # codesearch.debian.net uses 6 shards
      description = "Number of index shards (one dcs-package-importer@N per shard)";
    };

    feeder.shards = lib.mkOption {
      type = lib.types.str;
      default = lib.concatMapStringsSep "," (i: "localhost:${toString (21010 + i)}") (
        builtins.genList (i: i) cfg.numShards
      );
      example = "localhost:21010,localhost:21011";
      description = "Comma-separated dcs-package-importer addresses that dcs-feeder feeds";
    };

    feeder.extraFlags = lib.mkOption {
      type = lib.types.listOf lib.types.str;
      default = [ ];
      example = [
        "-mirror_url=https://ftp.ch.debian.org/debian"
      ];
      description = ''
        Additional dcs-feeder flags (inserted verbatim, i.e. $(cat …) works).
      '';
    };

    web.sourceIndexes = lib.mkOption {
      type = lib.types.str;
      default = lib.concatMapStringsSep "," (i: "${cfg.dataDir}/shard${toString i}") (
        builtins.genList (i: i) cfg.numShards
      );
      example = "${cfg.dataDir}/shard0,${cfg.dataDir}/shard1";
      description = "Comma-separated index shard directories (containing src/ and full/) that dcs-web serves";
    };

    web.extraFlags = lib.mkOption {
      type = lib.types.listOf lib.types.str;
      default = [ ];
      example = [
        "-use_positional_index"
        "-securecookie_hash_key=$CREDENTIALS_DIRECTORY/securecookie_hash_key"
      ];
      description = ''
        Additional dcs-web flags (inserted verbatim, i.e. $(cat …) works).
      '';
    };

    computeRanking.extraFlags = lib.mkOption {
      type = lib.types.listOf lib.types.str;
      default = [ ];
      example = [
        "-mirror_url=https://ftp.ch.debian.org/debian"
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
      # Set this limit as appropriate: the default of 95%
      # leaves a bit of memory reserved for other processes.
      sliceConfig.MemoryMax = lib.mkDefault "95%";
    };

    users.groups.dcs = { };
    users.users.dcs = {
      isSystemUser = true;
      group = "dcs";
    };

    systemd.tmpfiles.rules = [
      "d ${cfg.dataDir} 0755 dcs dcs -"
      "d ${cfg.dataDir}/tmp 0755 dcs dcs -"
      "D ${cfg.dataDir}/tmp/importer 0755 dcs dcs -"
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
      startLimitIntervalSec = 0;
      requires = [ "dcs-init.service" ];
      after = [ "dcs-init.service" ];
      serviceConfig = defaultServiceConfig // {
        Type = "oneshot";
        ExecStart = "${computeRanking}/bin/dcs-compute-ranking";
      };
    };

    systemd.services.dcs-init = {
      description = "create certificates, secret keys, etc.";
      documentation = [ "https://github.com/Debian/dcs" ];
      unitConfig = {
        # Wait until dataDir is mounted, to avoid filling the root disk
        # in case the deployment uses a separate large disk for DCS.
        RequiresMountsFor = cfg.dataDir;
      };
      startLimitIntervalSec = 0;
      serviceConfig = defaultServiceConfig // {
        Type = "oneshot";
        # dcs-init creates keys, set umask to avoid
        # world-readable private key files.
        UMask = "0077";
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
        # All compression algorithms that tar(1) can use,
        # for upstream source tarballs (most lenient).
        bzip2 # for bzip2
        gzip # for gunzip
        lzip # for lzip
        xz # for xz
        zstd # for zstd
        # dcs-package-importer calls tar(1) directly for
        # source packages which have nested tarballs.
        gnutar
      ];
      # Use a $TMPDIR on the same disk as the target
      # to avoid cross-device copies in favor of renames.
      environment.TMPDIR = "${cfg.dataDir}/tmp/importer";
      startLimitIntervalSec = 0;
      serviceConfig = defaultDaemonServiceConfig // {
        # Defer to any other work, updating the index
        # is background work compared to serving queries.
        Nice = "19";
        IOSchedulingClass = "idle";

        ExecStart = "${importer}/bin/dcs-package-importer %i";
      };
    };

    systemd.targets."dcs-package-importers" = {
      description = "all dcs-package-importer instances";
      wantedBy = [ "multi-user.target" ];
      # Enable all dcs-package-importer@0 to @N (for numShards).
      wants = builtins.genList (i: "dcs-package-importer@${toString i}.service") cfg.numShards;
    };

    systemd.services.dcs-feeder = {
      description = "dcs-feeder (feeds Debian packages to the importers)";
      documentation = [ "https://github.com/Debian/dcs" ];
      wantedBy = [ "multi-user.target" ];
      requires = [ "dcs-init.service" ];
      wants = [ "network-online.target" ];
      after = [
        "dcs-init.service"
        # Without network connectivity, there is nothing to feed.
        # Cut down on log spam and errors by waiting until online.
        "network-online.target"
      ];
      startLimitIntervalSec = 0;
      serviceConfig = defaultDaemonServiceConfig // {
        ExecStart = "${feeder}/bin/dcs-feeder";
      };
    };

    systemd.services.dcs-web = {
      description = "dcs-web (frontend)";
      documentation = [ "https://github.com/Debian/dcs" ];
      wantedBy = [ "multi-user.target" ];
      requires = [ "dcs-init.service" ];
      after = [ "dcs-init.service" ];
      startLimitIntervalSec = 0;
      serviceConfig = defaultDaemonServiceConfig // {
        ExecStart = "${web}/bin/dcs-web";
      };
    };
  };
}
