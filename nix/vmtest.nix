self:
{ pkgs, ... }:
let
  dcs = self.packages.${pkgs.stdenv.hostPlatform.system}.default;

  shardSrc = ''
    mkdir -p $out/src
    cp -r ${self} $out/src/dcs_0
    dcs create -idx=$out/full $out/src
  '';

  shard = pkgs.runCommand "dcs-source-shard" {
    nativeBuildInputs = [
      dcs
    ];
  } shardSrc;
in
{
  name = "dcs-web";

  nodes.machine = {
    imports = [ self.nixosModules.default ];
    services.dcs.enable = true;
    systemd.tmpfiles.rules = [
      "L /srv/dcs/shard0 - - - - ${shard}"
    ];
  };

  testScript = ''
    machine.wait_for_unit("dcs-web.service")
    machine.wait_for_open_port(30000)
    out = machine.succeed("curl -fs 'http://localhost:30000/search?q=openSourceIndexes&wait=1'")
    assert "internal/web/backendlocal.go" in out, out
  '';
}
