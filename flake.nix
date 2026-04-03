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
    mr-valinskys-adequate-bridge = {
      url = "github:jvalinsky/mr_valinskys_adequate_bridge";
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
      mr-valinskys-adequate-bridge,
      ...
    }@inputs:
    let
      system = "x86_64-linux";
    in
    {
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

            # Mr Valinsky's Adequate Bridge
            mr-valinskys-adequate-bridge.nixosModules.mr-valinskys-adequate-bridge
            {
              nixpkgs.overlays = [ mr-valinskys-adequate-bridge.overlays.default ];

              services.mr-valinskys-adequate-bridge = {
                enable = true;
                environmentFile = "/run/secrets/rendered/mr-valinskys-adequate-bridge-env";
                firehoseEnable = true;
                publishWorkers = 2;

                room = {
                  enable = true;
                  listenAddr = ":8989";
                  httpListenAddr = "127.0.0.1:8976";
                  mode = "open";
                  httpsDomain = "room.snek.cc";
                };

                ui.enable = true;
                observability.enable = false;
              };

              services.caddy.virtualHosts."room.snek.cc" = {
                extraConfig = ''
                  reverse_proxy http://127.0.0.1:8976
                '';
              };
            }

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