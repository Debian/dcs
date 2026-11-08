self:
{ pkgs, ... }:
let
  mirrorSrc = ''
    cp -r ${../internal/endtoend/testdata} $out
    cd $out
    chmod -R u+w .
    # overwrite Sources.{gz,bz2} with only i3-wm and zsh,
    # which is the minimal index: dcs-package-importer
    # only creates a full index with at least 2 packages.
    # This speeds up the vmtest significantly.
    # (dpkg-scansources takes at most 1 directory per call.)
    (dpkg-scansources pool/main/i; dpkg-scansources pool/main/z) | gzip > dists/sid/main/source/Sources.gz
    (dpkg-scansources pool/main/i; dpkg-scansources pool/main/z) | bzip2 -c > dists/sid/main/source/Sources.bz2
    # dcs-feeder fails if contrib does not exist
    mkdir -p dists/sid/contrib/source
    gzip < /dev/null > dists/sid/contrib/source/Sources.gz
  '';

  testDebianMirror = pkgs.runCommand "dcs-test-debian-mirror" {
    nativeBuildInputs = with pkgs; [
      dpkg
    ];
  } mirrorSrc;
in
{
  name = "dcs-web";

  nodes.machine = {
    imports = [ self.nixosModules.default ];
    services.static-web-server = {
      enable = true;
      root = testDebianMirror;
    };
    services.dcs = {
      enable = true;
      # Make the tests pass on any x86-64 CPU,
      # to enable machines without AVX512 to build GOAMD64=v4.
      goAMD64 = "v1";
      feeder.extraFlags = [
        "-mirror_url=http://localhost:8787"
        "-merge_last_activity=1s"
      ];
      # Enable Caddy (for testing) without HTTPS.
      caddy.enable = true;
      domain = "http://localhost";
    };
  };

  # dcs-package-importer creates a relative 'full' symlink,
  # dcs-web replaces that with an absolute symlink in ReplaceIndex,
  # i.e. by polling for an absolute symlink we wait until
  # dcs-web has actually picked up the index.
  testScript = ''
    machine.wait_until_succeeds("readlink /srv/dcs/shard0/full | grep -q ^/", timeout=60)

    machine.wait_for_unit("dcs-web.service")
    machine.wait_for_open_port(30000)
    out = machine.succeed("curl -fs 'http://localhost:30000/search?q=i3Font&wait=1'")
    assert "i3-wm_4.5.1-2" in out, out

    machine.wait_for_open_port(80)
    out = machine.succeed("curl -fs 'http://localhost/search?q=i3Font&wait=1'")
    assert "i3-wm_4.5.1-2" in out, out
  '';
}
