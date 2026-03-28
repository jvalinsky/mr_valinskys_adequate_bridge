# Documentation Index

This repo's bridge logic has two separate translation layers:

1. ATProto account identity (`did:...`) to deterministic SSB feed identity (`@...`).
2. ATProto record references (`at://...`) to published SSB message refs (`%...`).

The detailed docs for that flow live here:

- [ATProto to SSB Translation Overview](./atproto-ssb-translation-overview.md)
- [ATProto to SSB Identity Mapping](./atproto-ssb-identity-mapping.md)
- [ATProto to SSB Record Translation](./atproto-ssb-record-translation.md)
- [Bridge Operator Runbook](./runbook.md)

Primary code referenced by those docs:

- [`cmd/bridge-cli/main.go`](../cmd/bridge-cli/main.go)
- [`internal/bots/manager.go`](../internal/bots/manager.go)
- [`internal/ssbruntime/runtime.go`](../internal/ssbruntime/runtime.go)
- [`internal/bridge/processor.go`](../internal/bridge/processor.go)
- [`internal/bridge/dependencies.go`](../internal/bridge/dependencies.go)
- [`internal/mapper/mapper.go`](../internal/mapper/mapper.go)
- [`internal/db/schema.sql`](../internal/db/schema.sql)
