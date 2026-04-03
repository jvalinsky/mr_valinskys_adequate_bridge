{ inputs, config, lib, ... }:
{
  imports = [ ];

  nixpkgs.overlays = [ inputs.mr-valinskys-adequate-bridge.overlays.default ];

  sops.defaultSopsFile = ./secrets/bluesky-pds.yaml;

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

    repoPath = "/var/lib/mr-valinskys-adequate-bridge/.ssb-bridge";
    dataDir = "/var/lib/mr-valinskys-adequate-bridge";

    room = {
      enable = true;
      listenAddr = ":8989";
      httpListenAddr = "127.0.0.1:8976";
      mode = "open";
      httpsDomain = "room.snek.cc";
    };

    ui = {
      enable = true;
      extraArgs = [
        "--repo-path" "/var/lib/mr-valinskys-adequate-bridge/.ssb-bridge"
        "--room-repo-path" "/var/lib/mr-valinskys-adequate-bridge/.ssb-bridge/room"
        "--room-http-base-url" "https://room.snek.cc"
      ];
    };

    observability.enable = false;
  };

  services.caddy.virtualHosts."room.snek.cc" = {
    extraConfig = ''
      reverse_proxy http://127.0.0.1:8976
    '';
  };

  services.caddy.virtualHosts."admin-room.snek.cc" = {
    extraConfig = ''
      reverse_proxy http://127.0.0.1:8080
    '';
  };
}
