# Directory Convention

This document summarizes the responsibility of each major directory and the current state flow.

## Core Flow

```text
Admin API
  -> internal/admin.Service
  -> internal/raft.Store or local state store
  -> state.DesiredState.ProxyConfig
  -> state.ProjectSnapshot()
  -> route.BuildTable(spec.Config)
  -> upstream.BuildRegistry(spec.Config)
  -> runtime.Snapshot
  -> proxy / dashboard read path
```

Proxy configuration is one cluster-wide `spec.Config`. Routes and upstream pools reference each other directly by ID inside that config.

## Directory Responsibilities

### `internal/spec`

Defines the editable proxy desired-config schema and validation rules.

### `internal/state`

Owns Raft desired state and runtime projection.

- Validates `DesiredState.ProxyConfig`.
- Merges cluster VIP/Raft timing policy with local runtime settings.
- Compiles the validated config into a route table and upstream registry.

### `internal/route`

Compiles `spec.Config.Routes` into a runtime route table.

### `internal/upstream`

Compiles `spec.Config.UpstreamPools` into a runtime upstream registry.

### `internal/runtime`

Stores the currently applied snapshot atomically.

### `internal/proxy`

Matches HTTP requests against the current runtime snapshot and proxies them to upstream targets.

### `internal/admin`

Provides the service boundary between Dashboard config API and the state store. Public config operations are `GetConfig` and `ReplaceConfig`.

### `internal/raft`

Replicates cluster desired state in HA mode. Proxy config writes use a single `replace_config` command that replaces the full config.

### `internal/dashboard`

Serves Dashboard HTML and JSON APIs.

- `GET /api/config`
- `PUT /api/config`
- `GET /api/status`
- `GET /api/runtime`
- cluster lifecycle/status APIs

## ID Rule

Route IDs and upstream pool IDs must be unique inside the single config.

- Runtime route ID equals `RouteConfig.ID`.
- Runtime upstream pool ID equals the `Config.UpstreamPools` map key.
- No scope prefix is added to runtime IDs.

## Write Rule

Config writes replace the full config.

- Route create/update/delete happens by editing the client-side config and saving it with `PUT /api/config`.
- Upstream pool create/update/delete uses the same flow.
- Do not add separate partial CRUD commands.
