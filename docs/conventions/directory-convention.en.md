# Directory Convention

## Purpose

This document explains the intended directory structure, package responsibilities, and dependency boundaries of this repository.

It is written for two audiences:

- humans reading and extending the codebase
- coding agents that need stable context about why the project is structured this way

This document should be updated when the ownership or responsibility of a directory changes in a meaningful way.

## Project Goal

This repository is a reverse proxy POC.

Current implementation direction:

- load app-level bootstrap config from `configs/app.json`
- manage reverse proxy desired state through the Raft log/snapshot and Admin API
- validate desired state on write
- build one global route table
- build one global upstream registry
- keep active runtime state in memory
- proxy requests using the active runtime snapshot

Out of scope for the current phase:

- file watch
- static JSON file loading for proxy routes and upstreams

## Top-Level Layout

### `go.mod`

Single-module Go project definition.

Rules:

- keep one module during the POC phase
- do not split into multiple modules unless there is a strong reason
- keep internal implementation under `internal/`

### `main.go`

Program entrypoint.

Responsibilities:

- determine config path
- initialize logger
- load app config
- create app
- run servers

Rules:

- keep `main.go` thin
- do not move routing policy or runtime orchestration logic into `main.go`

### `configs/`

Configuration files used by the application.

Current structure:

- `configs/app.json`

Intent:

- `app.json` stores server bootstrap configuration
- reverse proxy route/upstream desired state is stored in the Raft log/snapshot, not static config files

### `docs/`

Official home for human-readable architecture, API, and convention notes.

Intent:

- preserve project structure intent
- help future contributors understand package boundaries quickly
- help operators and developers inspect API and runtime model contracts

Current examples:

- `docs/architecture/architecture.ko.md`
- `docs/api/dashboard-api.ko.md`
- `docs/conventions/directory-convention.ko.md`
- `docs/conventions/type-reference.ko.md`

### `scripts/`

Project verification scripts, especially HA smoke tests.

Current examples:

- `scripts/raft-ha-cluster-smoke.sh`
- `scripts/raft-ha-vip-smoke.sh`

Rules:

- automated checks tightly coupled to compose scenarios live in `scripts/`
- reusable manual validation and benchmark helpers live in `tools/`

## `internal/` Package Intent

All main implementation packages live under `internal/`.

General rule:

- business logic stays under `internal/`
- package names should be short and responsibility-driven
- avoid vague names such as `utils`, `helpers`, or `common`

## Package Responsibilities

### `internal/app`

Application wiring and startup orchestration.

Responsibilities:

- connect config loading with runtime building
- construct runtime snapshot
- create proxy handler and dashboard handler
- create HTTP servers
- own application lifecycle flow

What belongs here:

- app construction
- startup wiring
- shutdown flow
- reload orchestration

What should not live here:

- detailed routing match logic
- upstream balancing logic
- raw config schema definitions

### `internal/boot`

App-level bootstrap configuration only.

Current role:

- define `AppConfig`
- load `configs/app.json`
- apply defaults
- validate app-level config

Examples of data that belong here:

- proxy listen address
- dashboard listen address
- Raft bind/bootstrap/join node bootstrap settings

Examples of data that do not belong here:

- route definitions
- upstream pool definitions
- runtime health state

Reason:

The app bootstrap config changes more rarely than reverse proxy desired state and has different lifecycle semantics.

### `internal/spec`

Raw reverse proxy desired-state schema and validation.

Current role:

- define the route/upstream desired-state schema
- validate namespace-level proxy configs
- keep the input representation used at the Raft/Admin API boundary

Important distinction:

- this package owns desired-state representation
- this package does not own runtime routing behavior

### `internal/state`

Raft-agreed desired-state model and runtime projection.

Current role:

- define namespace-level desired-state models
- validate cluster-wide VIP/Raft timing policy
- project desired state into `runtime.Snapshot`
- provide state errors used by Admin/API responses

Important distinction:

- `state` owns the agreed target state and projection boundary
- Raft log storage, transport, and FSM implementation belong to `internal/raft`

### `internal/raft`

HashiCorp Raft-backed store implementation.

Current role:

- create and shut down Raft nodes
- encode and decode commands
- apply FSM commands and snapshot/restore state
- enforce leader-only writes and submit Raft applies
- persist and restore node-local Raft metadata

### `internal/route`

Runtime routing policy.

Current role:

- compile `spec` routes into runtime routes
- assign global route IDs
- assign global upstream pool references
- compile regex matchers
- build the global route table
- sort routes by precedence
- resolve request host/path to one route

Important rule:

- routes from all namespaces are projected into one global route table
- route matching is based on fixed precedence, not JSON array order

Current precedence:

1. exact
2. prefix
3. regex
4. any

Prefix semantics:

- segment-based, not plain string-prefix semantics

### `internal/upstream`

Runtime upstream registry and balancing.

Current role:

- compile upstream pools from all namespaces into runtime pools
- assign global pool IDs
- build the global registry
- select a target from a pool

Current balancing:

- simple round-robin

Important distinction:

- config schema for upstream pools belongs to `internal/spec`
- runtime pool registry and target selection belongs to `internal/upstream`

### `internal/vip`

Raft leader-based VIP failover.

Current role:

- convert Raft leadership transitions into VIP acquire/release actions
- add/remove VIP addresses on Linux interfaces
- send Gratuitous ARP after VIP acquisition

### `internal/vip/runtime`

Runtime VIP config applied by the current node.

Current role:

- represent the merged cluster-wide VIP policy and node-local interface
- determine whether VIP handling is active

### `internal/runtime`

Active in-memory state.

Current role:

- hold the active app config
- hold loaded proxy config metadata
- hold global route table
- hold global upstream registry
- expose snapshot reads
- support atomic snapshot swap

Important intent:

- runtime state is not a source-of-truth replacement
- runtime state is the active compiled view of the desired configuration

### `internal/proxy`

Actual reverse proxy request forwarding.

Current role:

- read current runtime snapshot
- resolve request against route table
- select upstream target
- forward request to selected upstream

Important boundary:

- `internal/proxy` should not define routing policy
- `internal/proxy` consumes routing and upstream decisions

### `internal/dashboard`

Read-oriented management HTTP endpoints for the current phase.

Current role:

- expose active config and runtime state
- return structured views for app config, loaded proxy configs, routes, and upstreams

Current scope:

- read APIs only
- no config mutation APIs yet

### `internal/middleware`

Cross-cutting HTTP middleware.

Current role:

- shared middleware such as request logging

Rule:

- only shared HTTP concerns belong here

## Dependency Direction

Intended dependency direction:

- `main` -> `app`
- `app` -> `boot`, `state`, `raft`, `route`, `upstream`, `runtime`, `proxy`, `dashboard`
- `proxy` -> `runtime`, `route`
- `route` -> `spec`
- `upstream` -> `spec`
- `dashboard` -> `runtime`

Packages that should stay decoupled:

- `route` should not depend on `dashboard`
- `upstream` should not depend on `dashboard`
- `boot` should not depend on HTTP or UI packages

## Namespace Rule

Each namespace is managed as a key in the Raft desired-state map.

Global IDs are built using this namespace:

- route ID: `<namespace>:<route.id>`
- upstream pool ID: `<namespace>:<pool.id>`

Reason:

- allow repeated local IDs across different namespaces
- keep runtime IDs globally unique

## Design Intent Summary

The codebase intentionally separates three layers:

1. file schema layer
2. runtime policy layer
3. application wiring layer

Mapping:

- file schema layer -> `internal/boot`, `internal/spec`
- runtime policy layer -> `internal/route`, `internal/upstream`, `internal/runtime`
- application wiring layer -> `internal/app`, `internal/proxy`, `internal/dashboard`

This separation should be preserved unless there is a strong reason to change it.
