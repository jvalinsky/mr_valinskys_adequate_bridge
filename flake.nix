{
  description = "NixOS configuration for nixos server";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    sops-nix.url = "github:Mic92/sops-nix";
    alejandra.url = "github:kamadorueda/alejandra/4.0.0";
    alejandra.inputs.nixpkgs.follows = "nixpkgs";
    home-manager = {
      url = "github:nix-community/home-manager";
      inputs.nixpkgs.follows = "nixpkgs";
    };
  };

  outputs =
    {
      self,
      nixpkgs,
      sops-nix,
      alejandra,
      home-manager,
      ...
    }@inputs:
    let
      system = "x86_64-linux";
    in
    {
      nixosModules.mr-valinskys-adequate-bridge = import ./nix/modules/mr-valinskys-adequate-bridge.nix;

      overlays.default = final: prev: {
        mr-valinskys-adequate-bridge = final.callPackage ./package.nix { };
      };

      nixosConfigurations = {
        snek = nixpkgs.lib.nixosSystem {
          inherit system;

          specialArgs = { inherit inputs; lib = nixpkgs.lib; };

          modules = [
            sops-nix.nixosModules.sops
            home-manager.nixosModules.home-manager
            {
              environment.systemPackages = [ alejandra.defaultPackage.${system} ];
              home-manager.useGlobalPkgs = true;
              home-manager.useUserPackages = true;
              home-manager.users.atproto = import ./home.nix;
            }

            self.nixosModules.mr-valinskys-adequate-bridge
            (import ./bridge-module.nix)

            ./configuration.nix
          ];
        };
      };

      nixConfig = {
        extra-substituters = [
          "https://nix-community.cachix.org"
          "https://crane.cachix.org"
        ];
        extra-trusted-public-keys = [
          "nix-community.cachix.org-1:mB9FSh9qf2dCimDSUo8Zy7bkq5CX+/rkCWyvRCYg3Fs="
          "crane.cachix.org-1:8Scfpmn9w+hGdXH/Q9tTLiYAE/2dnJYRJP7kl80GuRk="
        ];
      };
    };
}
