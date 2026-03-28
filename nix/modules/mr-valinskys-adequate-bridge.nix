{
  config,
  lib,
  pkgs,
  ...
}:
let
  inherit (lib)
    concatStringsSep
    escapeShellArgs
    getExe
    getExe'
    hasPrefix
    hasSuffix
    removePrefix
    removeSuffix
    mkDefault
    mkEnableOption
    mkIf
    splitString
    take
    mkMerge
    mkOption
    optional
    optionalAttrs
    optionals
    types
    unique
    ;

  cfg = config.services.mr-valinskys-adequate-bridge;

  serviceName = "mr-valinskys-adequate-bridge";
  serviceUser = serviceName;
  serviceGroup = serviceName;

  dbDir = builtins.dirOf cfg.dbPath;
  repoDir = builtins.dirOf cfg.repoPath;
  requiredDirs = unique [
    cfg.dataDir
    dbDir
    repoDir
  ];

  mkBoolArg = name: value: "--${name}=${if value then "true" else "false"}";

  runtimeGlobalArgs =
    [
      "--db"
      cfg.dbPath
      "--relay-url"
      cfg.relayUrl
      "--otel-logs-protocol"
      cfg.logging.otelLogsProtocol
      "--otel-service-name"
      cfg.logging.otelServiceNameRuntime
      "--local-log-output"
      cfg.logging.localLogOutput
    ]
    ++ optionals (cfg.logging.otelLogsEndpoint != null) [
      "--otel-logs-endpoint"
      cfg.logging.otelLogsEndpoint
    ]
    ++ optionals cfg.logging.otelLogsInsecure [ "--otel-logs-insecure" ];

  runtimeCommandArgs =
    runtimeGlobalArgs
    ++ [ "start" ]
    ++ [
      "--repo-path"
      cfg.repoPath
      "--publish-workers"
      (toString cfg.publishWorkers)
      "--ssb-listen-addr"
      cfg.ssbListenAddr
      (mkBoolArg "firehose-enable" cfg.firehoseEnable)
      (mkBoolArg "room-enable" cfg.room.enable)
    ]
    ++ optionals (cfg.xrpcHost != null) [
      "--xrpc-host"
      cfg.xrpcHost
    ]
    ++ optionals cfg.room.enable [
      "--room-listen-addr"
      cfg.room.listenAddr
      "--room-http-listen-addr"
      cfg.room.httpListenAddr
      "--room-mode"
      cfg.room.mode
    ]
    ++ optionals (cfg.room.enable && cfg.room.httpsDomain != null) [
      "--room-https-domain"
      cfg.room.httpsDomain
    ]
    ++ cfg.startExtraArgs;

  uiGlobalArgs =
    [
      "--db"
      cfg.dbPath
      "--otel-logs-protocol"
      cfg.logging.otelLogsProtocol
      "--otel-service-name"
      cfg.logging.otelServiceNameUI
      "--local-log-output"
      cfg.logging.localLogOutput
    ]
    ++ optionals (cfg.logging.otelLogsEndpoint != null) [
      "--otel-logs-endpoint"
      cfg.logging.otelLogsEndpoint
    ]
    ++ optionals cfg.logging.otelLogsInsecure [ "--otel-logs-insecure" ];

  uiCommandArgs =
    uiGlobalArgs
    ++ [ "serve-ui" ]
    ++ [
      "--listen-addr"
      cfg.ui.listenAddr
    ]
    ++ optionals (cfg.ui.authUser != null) [
      "--ui-auth-user"
      cfg.ui.authUser
      "--ui-auth-pass-env"
      cfg.ui.authPasswordEnvVar
    ]
    ++ cfg.ui.extraArgs;

  extractHost =
    addr:
    let
      parts = splitString ":" addr;
      hostParts = take ((builtins.length parts) - 1) parts;
      rawHost = concatStringsSep ":" hostParts;
    in
    if builtins.length parts <= 1 then
      addr
    else if hasPrefix "[" rawHost && hasSuffix "]" rawHost then
      removeSuffix "]" (removePrefix "[" rawHost)
    else
      rawHost;

  isLoopbackHost = host: host == "localhost" || host == "::1" || hasPrefix "127." host;
  isLoopbackAddr = addr: isLoopbackHost (extractHost addr);

  roomExternallyExposed =
    cfg.room.enable
    && (
      (!isLoopbackAddr cfg.room.listenAddr)
      || (!isLoopbackAddr cfg.room.httpListenAddr)
    );

  collectorEnabled = cfg.observability.enable && cfg.observability.collector.enable;
  lokiEnabled = cfg.observability.enable && cfg.observability.loki.enable;
  grafanaEnabled = cfg.observability.enable && cfg.observability.grafana.enable;
  promtailEnabled = cfg.observability.enable && cfg.observability.includeJournaldInLoki;
in
{
  options.services.mr-valinskys-adequate-bridge = {
    enable = mkEnableOption "Mr Valinsky's Adequate Bridge runtime";

    package = mkOption {
      type = types.package;
      default = pkgs.mr-valinskys-adequate-bridge;
      defaultText = "pkgs.mr-valinskys-adequate-bridge";
      description = "Package that provides the bridge-cli binary.";
    };

    environmentFile = mkOption {
      type = types.nullOr types.str;
      default = null;
      example = "/run/secrets/bridge.env";
      description = ''
        Environment file passed to both systemd units.
        Expected to include BRIDGE_BOT_SEED for runtime operation.
      '';
    };

    dataDir = mkOption {
      type = types.str;
      default = "/var/lib/mr-valinskys-adequate-bridge";
      description = "Runtime state directory.";
    };

    dbPath = mkOption {
      type = types.str;
      default = "/var/lib/mr-valinskys-adequate-bridge/bridge.sqlite";
      description = "SQLite database path passed via --db.";
    };

    repoPath = mkOption {
      type = types.str;
      default = "/var/lib/mr-valinskys-adequate-bridge/.ssb-bridge";
      description = "Shared SSB repo path passed via --repo-path.";
    };

    relayUrl = mkOption {
      type = types.str;
      default = "wss://bsky.network/xrpc/com.atproto.sync.subscribeRepos";
      description = "ATProto firehose relay URL.";
    };

    xrpcHost = mkOption {
      type = types.nullOr types.str;
      default = null;
      description = "Optional fixed ATProto read host override.";
    };

    publishWorkers = mkOption {
      type = types.int;
      default = 1;
      description = "Publish worker count passed to start.";
    };

    firehoseEnable = mkOption {
      type = types.bool;
      default = true;
      description = "Whether firehose ingestion should be enabled.";
    };

    ssbListenAddr = mkOption {
      type = types.str;
      default = ":8008";
      description = "SSB muxrpc listen address for bridge runtime.";
    };

    startExtraArgs = mkOption {
      type = types.listOf types.str;
      default = [ ];
      description = "Additional CLI args appended to bridge-cli start.";
    };

    room = {
      enable = mkOption {
        type = types.bool;
        default = true;
        description = "Enable embedded Room2 runtime.";
      };

      listenAddr = mkOption {
        type = types.str;
        default = "127.0.0.1:8989";
        description = "Room2 muxrpc listen address.";
      };

      httpListenAddr = mkOption {
        type = types.str;
        default = "127.0.0.1:8976";
        description = "Room2 HTTP listen address.";
      };

      mode = mkOption {
        type = types.enum [
          "open"
          "community"
          "restricted"
        ];
        default = "community";
        description = "Room2 mode.";
      };

      httpsDomain = mkOption {
        type = types.nullOr types.str;
        default = null;
        description = "Room2 HTTPS domain (required for non-loopback room exposure).";
      };
    };

    ui = {
      enable = mkOption {
        type = types.bool;
        default = false;
        description = "Enable separate admin UI service (bridge-cli serve-ui).";
      };

      listenAddr = mkOption {
        type = types.str;
        default = "127.0.0.1:8080";
        description = "Admin UI listen address.";
      };

      authUser = mkOption {
        type = types.nullOr types.str;
        default = null;
        description = "HTTP Basic auth username for UI.";
      };

      authPasswordEnvVar = mkOption {
        type = types.str;
        default = "BRIDGE_UI_PASSWORD";
        description = "Environment variable name read by --ui-auth-pass-env.";
      };

      extraArgs = mkOption {
        type = types.listOf types.str;
        default = [ ];
        description = "Additional CLI args appended to bridge-cli serve-ui.";
      };
    };

    logging = {
      otelLogsEndpoint = mkOption {
        type = types.nullOr types.str;
        default = null;
        example = "127.0.0.1:4317";
        description = "OTLP logs endpoint passed via --otel-logs-endpoint.";
      };

      otelLogsProtocol = mkOption {
        type = types.enum [
          "grpc"
          "http"
        ];
        default = "grpc";
        description = "OTLP logs protocol passed via --otel-logs-protocol.";
      };

      otelLogsInsecure = mkOption {
        type = types.bool;
        default = false;
        description = "Disable OTLP transport security.";
      };

      otelServiceNameRuntime = mkOption {
        type = types.str;
        default = "bridge-cli";
        description = "OTel service.name for runtime command logs.";
      };

      otelServiceNameUI = mkOption {
        type = types.str;
        default = "bridge-ui";
        description = "OTel service.name for UI command logs.";
      };

      localLogOutput = mkOption {
        type = types.enum [
          "text"
          "none"
        ];
        default = "text";
        description = "Local log output mode passed via --local-log-output.";
      };
    };

    observability = {
      enable = mkEnableOption "local observability stack";

      collector.enable = mkOption {
        type = types.bool;
        default = true;
        description = "Enable services.opentelemetry-collector.";
      };

      loki.enable = mkOption {
        type = types.bool;
        default = true;
        description = "Enable services.loki.";
      };

      grafana.enable = mkOption {
        type = types.bool;
        default = true;
        description = "Enable services.grafana.";
      };

      includeJournaldInLoki = mkOption {
        type = types.bool;
        default = false;
        description = "Enable services.promtail to ship journald logs to Loki.";
      };
    };
  };

  config = mkIf cfg.enable (mkMerge [
    {
      assertions = [
        {
          assertion = cfg.environmentFile != null;
          message = "services.mr-valinskys-adequate-bridge.environmentFile must be set (must provide BRIDGE_BOT_SEED).";
        }
        {
          assertion = cfg.ui.authUser == null || cfg.ui.authPasswordEnvVar != "";
          message = "services.mr-valinskys-adequate-bridge.ui.authPasswordEnvVar must be non-empty when ui.authUser is set.";
        }
        {
          assertion = !roomExternallyExposed || (cfg.room.httpsDomain != null && cfg.room.httpsDomain != "");
          message = "Room is configured on a non-loopback address; set services.mr-valinskys-adequate-bridge.room.httpsDomain.";
        }
        {
          assertion = !promtailEnabled || lokiEnabled;
          message = "services.mr-valinskys-adequate-bridge.observability.includeJournaldInLoki requires observability.loki.enable = true.";
        }
      ];

      users.groups.${serviceGroup} = { };
      users.users.${serviceUser} = {
        isSystemUser = true;
        group = serviceGroup;
        home = cfg.dataDir;
        createHome = true;
      };

      systemd.tmpfiles.rules = map (
        dir: "d ${dir} 0750 ${serviceUser} ${serviceGroup} -"
      ) requiredDirs;

      systemd.services.mr-valinskys-adequate-bridge = {
        description = "Mr Valinsky's Adequate Bridge runtime";
        wantedBy = [ "multi-user.target" ];
        wants =
          [ "network-online.target" ]
          ++ optionals collectorEnabled [ "opentelemetry-collector.service" ];
        after =
          [ "network-online.target" ]
          ++ optionals collectorEnabled [ "opentelemetry-collector.service" ];

        serviceConfig =
          {
            Type = "simple";
            User = serviceUser;
            Group = serviceGroup;
            WorkingDirectory = cfg.dataDir;
            ExecStart = "${getExe' cfg.package "bridge-cli"} ${escapeShellArgs runtimeCommandArgs}";
            Restart = "on-failure";
            RestartSec = "5s";
            NoNewPrivileges = true;
            ProtectSystem = "strict";
            ProtectHome = true;
            PrivateTmp = true;
            PrivateDevices = true;
            ProtectControlGroups = true;
            ProtectKernelTunables = true;
            ProtectKernelModules = true;
            LockPersonality = true;
            RestrictRealtime = true;
            RestrictSUIDSGID = true;
            MemoryDenyWriteExecute = true;
            ReadWritePaths = requiredDirs;
          }
          // optionalAttrs (cfg.environmentFile != null) {
            EnvironmentFile = cfg.environmentFile;
          };
      };

      systemd.services.mr-valinskys-adequate-bridge-ui = mkIf cfg.ui.enable {
        description = "Mr Valinsky's Adequate Bridge admin UI";
        wantedBy = [ "multi-user.target" ];
        wants =
          [ "network-online.target" "mr-valinskys-adequate-bridge.service" ]
          ++ optionals collectorEnabled [ "opentelemetry-collector.service" ];
        after =
          [ "network-online.target" "mr-valinskys-adequate-bridge.service" ]
          ++ optionals collectorEnabled [ "opentelemetry-collector.service" ];

        serviceConfig =
          {
            Type = "simple";
            User = serviceUser;
            Group = serviceGroup;
            WorkingDirectory = cfg.dataDir;
            ExecStart = "${getExe' cfg.package "bridge-cli"} ${escapeShellArgs uiCommandArgs}";
            Restart = "on-failure";
            RestartSec = "5s";
            NoNewPrivileges = true;
            ProtectSystem = "strict";
            ProtectHome = true;
            PrivateTmp = true;
            PrivateDevices = true;
            ProtectControlGroups = true;
            ProtectKernelTunables = true;
            ProtectKernelModules = true;
            LockPersonality = true;
            RestrictRealtime = true;
            RestrictSUIDSGID = true;
            MemoryDenyWriteExecute = true;
            ReadWritePaths = requiredDirs;
          }
          // optionalAttrs (cfg.environmentFile != null) {
            EnvironmentFile = cfg.environmentFile;
          };
      };
    }

    (mkIf collectorEnabled {
      services.mr-valinskys-adequate-bridge.logging.otelLogsEndpoint = mkDefault "127.0.0.1:4317";
      services.mr-valinskys-adequate-bridge.logging.otelLogsProtocol = mkDefault "grpc";
      services.mr-valinskys-adequate-bridge.logging.otelLogsInsecure = mkDefault true;

      services.opentelemetry-collector = {
        enable = true;
        package = mkDefault (
          if pkgs ? opentelemetry-collector-contrib then
            pkgs.opentelemetry-collector-contrib
          else
            pkgs.opentelemetry-collector
        );
        settings = {
          receivers = {
            otlp = {
              protocols = {
                grpc.endpoint = "127.0.0.1:4317";
                http.endpoint = "127.0.0.1:4318";
              };
            };
          };
          processors = {
            batch = { };
          };
          exporters = {
            loki = {
              endpoint = "http://127.0.0.1:3100/loki/api/v1/push";
            };
          };
          extensions = {
            health_check.endpoint = "127.0.0.1:13133";
          };
          service = {
            extensions = [ "health_check" ];
            pipelines = {
              logs = {
                receivers = [ "otlp" ];
                processors = [ "batch" ];
                exporters = [ "loki" ];
              };
            };
          };
        };
      };
    })

    (mkIf lokiEnabled {
      services.loki = {
        enable = true;
        configuration = {
          auth_enabled = false;
          server = {
            http_listen_address = "127.0.0.1";
            http_listen_port = 3100;
          };
          common = {
            path_prefix = "/var/lib/loki";
            replication_factor = 1;
            ring = {
              kvstore.store = "inmemory";
            };
          };
          schema_config = {
            configs = [
              {
                from = "2024-01-01";
                store = "tsdb";
                object_store = "filesystem";
                schema = "v13";
                index = {
                  prefix = "index_";
                  period = "24h";
                };
              }
            ];
          };
          storage_config = {
            filesystem = {
              directory = "/var/lib/loki/chunks";
            };
          };
          compactor = {
            working_directory = "/var/lib/loki/compactor";
            compaction_interval = "10m";
          };
          limits_config = {
            allow_structured_metadata = true;
            retention_period = "168h";
          };
        };
      };
    })

    (mkIf grafanaEnabled {
      services.grafana = {
        enable = true;
        settings = {
          server = {
            http_addr = "127.0.0.1";
            http_port = 3000;
          };
          users = {
            allow_sign_up = false;
          };
        };
        provision = {
          enable = true;
          datasources.settings = {
            apiVersion = 1;
            datasources = [
              {
                name = "Loki";
                type = "loki";
                access = "proxy";
                url = "http://127.0.0.1:3100";
                isDefault = true;
                editable = false;
              }
            ];
          };
        };
      };
    })

    (mkIf promtailEnabled {
      services.promtail = {
        enable = true;
        configuration = {
          server = {
            http_listen_address = "127.0.0.1";
            http_listen_port = 9080;
            grpc_listen_port = 0;
          };
          positions = {
            filename = "/var/cache/promtail/positions.yaml";
          };
          clients = [
            {
              url = "http://127.0.0.1:3100/loki/api/v1/push";
            }
          ];
          scrape_configs = [
            {
              job_name = "systemd-journal";
              journal = {
                max_age = "12h";
                labels = {
                  job = "systemd-journal";
                };
              };
              relabel_configs = [
                {
                  source_labels = [ "__journal__systemd_unit" ];
                  target_label = "systemd_unit";
                }
                {
                  source_labels = [ "__journal_syslog_identifier" ];
                  target_label = "syslog_identifier";
                }
              ];
            }
          ];
        };
      };
    })
  ]);
}
