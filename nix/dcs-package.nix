{
  pkgs,
  src,
  goAMD64 ? "v3", # services.dcs.goAMD64 sets this option via override
}:

pkgs.buildGoLatestModule {
  pname = "dcs";
  version = "unstable";

  src = src;
  nativeBuildInputs = with pkgs; [
    gitMinimal # for vcs version info
    dpkg # for dpkg-source (end-to-end tests)
  ];

  env = {
    CGO_ENABLED = "0";
    GOAMD64 = goAMD64;
    # Required for the simd/archsimd-based TurboPFor paths (Go ≥ 1.27).
    GOEXPERIMENT = "simd";
  };

  vendorHash = "sha256-wDA2oqx9LJJ3lVXSkLIqC+B+5XNb+rpLigsOCZiFzgU=";

  postInstall = ''
    # Caddy serves research/ and openapi* from here.
    mkdir -p $out/share
    cp -r internal/static $out/share/
    # Source code, not web assets: keep off codesearch.debian.net/.
    rm $out/share/static/*.go
  '';
}
