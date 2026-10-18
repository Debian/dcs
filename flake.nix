{
  description = "Debian Code Search";

  inputs = {
    # Using nixos-unstable for Go 1.27.
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    # https://michael.stapelberg.ch/posts/2026-04-05-stamp-it-all-programs-must-report-their-version/
    stapelbergnix.url = "github:stapelberg/nix";
    stapelbergnix.inputs.nixpkgs.follows = "nixpkgs";
  };

  outputs =
    {
      self,
      nixpkgs,
      stapelbergnix,
    }:
    let
      systems = [
        "x86_64-linux"
        "aarch64-linux"
      ];
      forAllSystems =
        f:
        nixpkgs.lib.genAttrs systems (
          system:
          f (
            import nixpkgs {
              inherit system;
              overlays = [ stapelbergnix.overlays.goVcsStamping ];
            }
          )
        );
    in
    {
      # Run nix fmt to format all .nix files.
      formatter = forAllSystems (pkgs: pkgs.nixfmt-tree);

      packages = forAllSystems (pkgs: {
        default = pkgs.callPackage ./nix/dcs-package.nix {
          src = self;
        };
      });

      nixosModules.default = import ./nix/module.nix self;
    };
}
