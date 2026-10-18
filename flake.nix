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
      pkgs = import nixpkgs {
        system = "x86_64-linux";
        overlays = [ stapelbergnix.overlays.goVcsStamping ];
      };
    in
    {
      # Run nix fmt to format all .nix files.
      formatter.x86_64-linux = pkgs.nixfmt-tree;

      packages.x86_64-linux.default = pkgs.callPackage ./nix/dcs-package.nix {
        src = self;
      };
    };
}
