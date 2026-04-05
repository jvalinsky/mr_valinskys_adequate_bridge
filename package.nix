{ pkgs, lib ? pkgs.lib }:
let
  go = if pkgs ? go_1_26 then pkgs.go_1_26 else pkgs.go;
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

  proxyVendor = true;
  vendorHash = "sha256-8yFyMg9y7lPQJ33h8O/4Op/eE0L3yMI94ib2kufb9uE=";
  inherit go;
}
