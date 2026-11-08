{
  description = "Debian Code Search Google Cloud VM";

  inputs = {
    dcs.url = "github:Debian/dcs";
    nixpkgs.follows = "dcs/nixpkgs";
  };

  outputs =
    {
      self,
      nixpkgs,
      dcs,
    }:
    let
      pkgs = nixpkgs.legacyPackages.x86_64-linux;
    in
    {
      nixosConfigurations.dcs-gce = nixpkgs.lib.nixosSystem {
        system = "x86_64-linux";
        specialArgs = { inherit dcs; };
        modules = [
          ./configuration.nix
        ];
      };

      packages.x86_64-linux.image = self.nixosConfigurations.dcs-gce.config.system.build.image;

      formatter.x86_64-linux = pkgs.nixfmt-tree;
    };
}
