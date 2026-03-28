{
  description = "Nix flake for Mr Valinsky's Adequate Bridge";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs =
    {
      self,
      nixpkgs,
      flake-utils,
    }:
    let
      supportedSystems = [
        "x86_64-linux"
        "aarch64-linux"
        "x86_64-darwin"
        "aarch64-darwin"
      ];

      mkBridgePackage =
        pkgs:
        let
          go = if pkgs ? go_1_25 then pkgs.go_1_25 else pkgs.go;
        in
        pkgs.buildGoModule {
          pname = "mr-valinskys-adequate-bridge";
          version = "0.0.0";

          src = ./.;
          subPackages = [ "cmd/bridge-cli" ];

          env = {
            CGO_ENABLED = "1";
          };
          nativeBuildInputs = [ pkgs.pkg-config ];
          buildInputs = [ pkgs.sqlite ];
          doCheck = false;

          vendorHash = "sha256-5B8PGDmdR+uHOaC4B5zN5uRu8XcQePmEf2k7ckEdurA=";
          inherit go;
        };
    in
    (flake-utils.lib.eachSystem supportedSystems (
      system:
      let
        pkgs = import nixpkgs { inherit system; };
        bridgeCli = mkBridgePackage pkgs;
        go = if pkgs ? go_1_25 then pkgs.go_1_25 else pkgs.go;
      in
      {
        packages = {
          bridge-cli = bridgeCli;
          default = bridgeCli;
        };

        apps = {
          bridge-cli = {
            type = "app";
            program = "${bridgeCli}/bin/bridge-cli";
          };
          default = {
            type = "app";
            program = "${bridgeCli}/bin/bridge-cli";
          };
        };

        devShells.default = pkgs.mkShell {
          packages = [
            go
            pkgs.gotools
            pkgs.pkg-config
            pkgs.sqlite
          ];
          CGO_ENABLED = "1";
        };
      }
    ))
    // {
      overlays.default = final: _prev: {
        mr-valinskys-adequate-bridge = mkBridgePackage final;
      };

      nixosModules = {
        mr-valinskys-adequate-bridge = import ./nix/modules/mr-valinskys-adequate-bridge.nix;
        default = self.nixosModules.mr-valinskys-adequate-bridge;
      };
    };
}
