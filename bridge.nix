{ inputs, config, ... }:
{
  imports = [ inputs.mr-valinskys-adequate-bridge.nixosModules.default ];

  nixpkgs.overlays = [ inputs.mr-valinskys-adequate-bridge.overlays.default ];

  sops.secrets.BRIDGE_BOT_SEED = {
    sopsFile = ./secrets/mr-valinskys-adequate-bridge.yaml;
  };

  sops.templates."mr-valinskys-adequate-bridge-env".content = ''
    BRIDGE_BOT_SEED=${config.sops.placeholder.BRIDGE_BOT_SEED}
  '';

  services.mr-valinskys-adequate-bridge = {
    enable = true;
    environmentFile = config.sops.templates."mr-valinskys-adequate-bridge-env".path;
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
