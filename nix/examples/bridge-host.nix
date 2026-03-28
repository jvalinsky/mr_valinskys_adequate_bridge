{ pkgs, ... }:
{
  # Example host usage:
  # imports = [
  #   inputs.mr-valinskys-adequate-bridge.nixosModules.default
  # ];
  # nixpkgs.overlays = [
  #   inputs.mr-valinskys-adequate-bridge.overlays.default
  # ];

  services.mr-valinskys-adequate-bridge = {
    enable = true;
    package = pkgs.mr-valinskys-adequate-bridge;

    environmentFile = "/run/secrets/bridge.env";

    dataDir = "/var/lib/mr-valinskys-adequate-bridge";
    dbPath = "/var/lib/mr-valinskys-adequate-bridge/bridge.sqlite";
    repoPath = "/var/lib/mr-valinskys-adequate-bridge/.ssb-bridge";

    relayUrl = "wss://bsky.network/xrpc/com.atproto.sync.subscribeRepos";
    firehoseEnable = true;
    publishWorkers = 4;
    ssbListenAddr = ":8008";

    room = {
      enable = true;
      listenAddr = "127.0.0.1:8989";
      httpListenAddr = "127.0.0.1:8976";
      mode = "community";
    };

    ui = {
      enable = true;
      listenAddr = "127.0.0.1:8080";
      authUser = "admin";
      authPasswordEnvVar = "BRIDGE_UI_PASSWORD";
    };

    logging = {
      otelLogsEndpoint = "127.0.0.1:4317";
      otelLogsProtocol = "grpc";
      otelLogsInsecure = true;
      localLogOutput = "text";
    };

    observability = {
      enable = true;
      collector.enable = true;
      loki.enable = true;
      grafana.enable = true;
      includeJournaldInLoki = false;
    };
  };

  # Secret file example:
  # BRIDGE_BOT_SEED=replace-me-with-stable-secret
  # BRIDGE_UI_PASSWORD=replace-me
}
