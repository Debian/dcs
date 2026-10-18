{
  description = "Debian Code Search";

  inputs = {
    # Using nixos-unstable for Go 1.27.
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
  };

  outputs =
    { self, nixpkgs }:
    let
      pkgs = nixpkgs.legacyPackages.x86_64-linux;
    in
    {
      # Run nix fmt to format all .nix files.
      formatter.x86_64-linux = pkgs.nixfmt-tree;

      packages.x86_64-linux.default = pkgs.buildGoLatestModule {
        pname = "dcs";
        version = "unstable";

        src = self;
        nativeBuildInputs = with pkgs; [
          gitMinimal # for vcs version info
          dpkg # for dpkg-source (end-to-end tests)
        ];

        env.GOAMD64 = "v4";
        # Required for the simd/archsimd-based TurboPFor paths (Go ≥ 1.27).
        env.GOEXPERIMENT = "simd";
        env.CGO_ENABLED = "0";

        vendorHash = "sha256-vexNc6e8cRogC/2fLl+NCG+jATjUqfETZ87YAs+aiis=";

        postInstall = ''
          # Caddy serves research/ and openapi* from here.
          mkdir -p $out/share
          cp -r internal/static $out/share/
          # Source code, not web assets: keep off codesearch.debian.net/.
          rm $out/share/static/*.go
        '';
      };
    };
}
