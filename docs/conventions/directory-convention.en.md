# Directory Convention

## Purpose

This document explains the current directory layout, package responsibilities, and dependency boundaries.

It is written for:

- developers reading and maintaining the codebase
- users who need to understand execution and verification flows
- contributors who need stable ownership boundaries before changing structure

Update this document when directory ownership, public API shape, or type meaning changes.

## Project Goal

This repository is an L7 reverse proxy with Raft-backed state replication and VIP failover.

Current implementation direction:

- load process bootstrap config from `configs/app.json`
- manage reverse proxy desired state through the Raft log/snapshot and Admin API
- validate desired state on write
- project all namespace routes into one global route table
- project all namespace upstream pools into one global upstream registry
- keep active state in `runtime.Snapshot`
- proxy requests using the active runtime snapshot
- acquire and release VIP ownership based on Raft leadership

Out of scope:

- file watching
- static JSON loading for proxy routes/upstreams
- L4 load balancing

## Top-Level Layout

### `README.md`

Primary entry document for first-time readers.

Responsibilities:

- summarize project goals and features
- explain execution, build, and test flows
- link to deeper documentation
- summarize performance and failover measurements

### `go.mod`, `go.sum`

Single-module Go project definition and dependency lock files.

Rules:

- keep one module unless there is a clear architectural reason to split it
- keep implementation packages under `internal/`

### `main.go`

Program entrypoint.

Responsibilities:

- create OS signal context
- run the CLI
- print errors and set exit code

Rules:

- keep `main.go` thin
- keep routing policy, runtime assembly, and server wiring in `internal/app` or lower packages

### `Dockerfile`

Container image build definition.

Responsibilities:

- build a static binary in the Go builder stage
- build an Alpine runtime image
- copy default config
- expose `8080` and `9090`

### `configs/`

Process bootstrap configuration.

Current structure:

- `configs/app.json`

Intent:

- store process-local settings such as proxy listen address, dashboard listen address, and Raft data dir
- store route/upstream desired state in Raft log/snapshot, not static config files

### `docs/`

Official home for architecture, API, convention, and verification documents.

Current structure:

- `docs/api/`
- `docs/architecture/`
- `docs/conventions/`

Rules:

- keep code structure and responsibility boundaries in `docs/conventions/`
- keep runtime flow and design background in `docs/architecture/`
- keep HTTP API contracts in `docs/api/`
- do not commit one-off dated experiment notes by default

### `scripts/`

Automated project verification scripts.

Current examples:

- `scripts/raft-ha-cluster-smoke.sh`
- `scripts/raft-ha-vip-smoke.sh`

Rules:

- smoke tests tightly coupled to compose scenarios live in `scripts/`
- reusable manual validation and benchmark helpers live in `tools/`

### `tools/`

Operational, experimental, manual validation, and benchmark helper scripts.

Current examples:

- `tools/round-robin-check.sh`
- `tools/sticky-cookie-check.sh`
- `tools/5-tuple-hash-check.sh`
- `tools/least-connection-check.sh`
- `tools/benchmark-*.sh`

Rules:

- do not put code required by the core app here
- focus on scenario execution and measurement helpers

### `composes/`

Docker Compose based local verification environments.

Current examples:

- `composes/route-basic/`
- `composes/lb-multi-upstream/`
- `composes/failure-healthcheck/`
- `composes/round-robin-check/`
- `composes/sticky-cookie-check/`
- `composes/5-tuple-hash-check/`
- `composes/least-connection-check/`
- `composes/raft-ha-cluster/`
- `composes/raft-ha-vip/`
- `composes/benchmark-check/`
- `composes/test-server/`

Rules:

- document each scenario in the scenario `README.md`
- keep shared test server code in `composes/test-server/`
- basic backend scenarios may omit the proxy app; HA scenarios may run proxy nodes too

## `internal/` Package Intent

All implementation packages live under `internal/`.

General rules:

- keep core logic under `internal/`
- package names should be short and responsibility-driven
- avoid vague names such as `utils`, `helpers`, or `common`
- use explicit view models instead of exposing runtime internals directly as external API responses

## Package Responsibilities

### `internal/app`

Application wiring and lifecycle orchestration.

Responsibilities:

- connect boot config to runtime construction
- wire Raft store and FSM apply/restore callbacks
- build and swap runtime snapshots
- create proxy and dashboard handlers
- create and stop HTTP servers
- connect cluster bootstrap/join flows
- wire VIP controller

Does not own:

- detailed route matching
- upstream target selection algorithms
- raw desired config schema
- Linux netlink/raw ARP details

### `internal/boot`

Process bootstrap configuration.

Current role:

- define `AppConfig`
- load `configs/app.json`
- apply defaults
- validate app-level config

Belongs here:

- proxy listen address
- dashboard listen address
- Raft data dir

Does not belong here:

- route definitions
- upstream pool definitions
- Raft node identity
- cluster-wide Raft timing
- VIP address/GARP policy

### `internal/cli`

Operational CLI layer.

Current role:

- run the app with `serve`
- query cluster lifecycle status with `cluster status`
- bootstrap the first node with `cluster bootstrap`
- join additional nodes with `cluster join`

Rules:

- the CLI acts as a dashboard lifecycle API client
- validation and state changes still flow through app, dashboard, state, and raft boundaries

### `internal/spec`

Raw reverse proxy desired-state schema and validation.

Current role:

- define namespace-level route/upstream desired config
- define route match, algorithm, upstream pool, and health check schemas
- validate desired config

Important distinction:

- this package owns desired-state representation
- this package does not build runtime route tables or upstream registries

### `internal/state`

Raft-agreed desired-state model and runtime projection boundary.

Current role:

- define `DesiredState`
- define namespace desired config management models
- define and validate cluster-wide VIP policy
- define and validate cluster-wide Raft timing policy
- project desired state into `runtime.Snapshot`
- provide state errors used by Admin/API responses

Important distinction:

- `state` owns agreed target state and projection boundaries
- Raft log storage, transport, and FSM implementation belong to `internal/raft`

### `internal/raft`

HashiCorp Raft-backed store implementation.

Current role:

- create and stop Raft nodes
- encode and decode commands
- apply FSM commands and snapshot/restore state
- enforce leader-only writes and submit Raft applies
- persist and restore node-local Raft metadata

Rules:

- Raft internals should not leak into `internal/app` or API layers
- desired-state meaning and validation belong to `internal/state`

### `internal/raftstate`

Runtime Raft identity and timing values.

Current role:

- represent node identity
- represent bind/advertise addresses
- represent cluster-wide Raft timing

### `internal/route`

Runtime routing policy.

Current role:

- compile `spec.RouteConfig` into runtime `route.Route`
- assign global route IDs
- assign global upstream pool references
- precompile regex matchers
- build the global route table
- sort routes by precedence
- resolve request host/path to one route

Important rules:

- routes from all namespaces are projected into one global route table
- matching uses fixed precedence, not JSON array order

Current precedence:

1. exact
2. prefix
3. regex
4. any

Prefix semantics:

- segment-based, not plain string-prefix semantics

### `internal/upstream`

Runtime upstream registry and target selection state.

Current role:

- compile upstream pools from all namespaces into runtime pools
- assign global pool IDs
- preparse target URLs
- keep health state
- manage healthy target indexes
- choose round-robin targets
- choose stable-hash targets
- choose least-connection targets and track active request count

Important distinction:

- upstream pool config schema belongs to `internal/spec`
- runtime registry, target health, in-flight count, and target selection belong to `internal/upstream`
- route algorithm interpretation and reverse proxy invocation belong to `internal/proxy`

### `internal/vip`

Raft leader-based VIP failover.

Current role:

- convert Raft leadership transitions into VIP acquire/release actions
- add/remove VIP addresses on Linux interfaces
- send Gratuitous ARP after VIP acquisition

Important boundaries:

- HashiCorp Raft owns leader election and quorum judgment
- VIP address/GARP policy is Raft desired state
- VIP interface is node-local lifecycle input from bootstrap/join
- `internal/app` only wires the controller and does not know netlink/raw ARP details
- Linux-specific privileged implementation is build-tag separated

### `internal/vip/runtime`

Runtime VIP config applied by the current node.

Current role:

- represent the merged cluster-wide VIP policy and node-local interface
- determine whether VIP handling is active

### `internal/runtime`

Active in-memory state.

Current role:

- hold process-local app config
- hold Raft identity/timing
- hold VIP runtime config
- hold projected namespace metadata
- hold global route table
- hold global upstream registry
- expose snapshot reads
- support atomic snapshot swap

Important intent:

- runtime state is not the source of truth
- runtime state is the active compiled view of desired state

### `internal/proxy`

Actual reverse proxy request forwarding.

Current role:

- read current runtime snapshot
- resolve request against route table
- select upstream target based on route algorithm
- forward request to the selected upstream
- create and reuse upstream transport pool

Important boundaries:

- `internal/proxy` does not mutate desired config
- `internal/proxy` forwards using runtime route/upstream decisions

### `internal/dashboard`

Dashboard UI and JSON API.

Current role:

- serve embedded dashboard HTML
- serve cluster lifecycle page
- provide namespace config read/write/delete API
- provide runtime/status/cluster read API
- provide cluster bootstrap/join lifecycle API
- convert internal runtime/state types into external view models

Rules:

- do not expose internal types directly as JSON
- keep API contracts synchronized with `docs/api/dashboard-api.ko.md`

## Dependency Direction

Intended dependency direction:

- `main` -> `cli`
- `cli` -> `app`, `boot`
- `app` -> `boot`, `state`, `raft`, `raftstate`, `runtime`, `proxy`, `dashboard`, `vip`
- `state` -> `spec`, `route`, `upstream`, `runtime`, `raftstate`, `vip/runtime`
- `proxy` -> `runtime`, `route`, `upstream`
- `route` -> `spec`
- `upstream` -> `spec`
- `dashboard` -> `admin`, `runtime`, `state`, `spec`, `route`, `upstream`
- `admin` -> `state`, `spec`

Packages that should stay decoupled:

- `route` should not depend on `dashboard`
- `upstream` should not depend on `dashboard`
- `boot` should not depend on HTTP or UI packages
- `spec` should not depend on runtime packages

## Namespace Rule

Each namespace is managed as a key in the Raft desired-state map.

Global IDs are built using this namespace:

- route ID: `<namespace>:<route.id>`
- upstream pool ID: `<namespace>:<pool.id>`

Reason:

- allow repeated local IDs across different namespaces
- keep runtime IDs globally unique

## Design Intent Summary

The codebase intentionally separates four layers:

1. process bootstrap layer
2. desired-state schema layer
3. runtime projection/policy layer
4. application/API wiring layer

Mapping:

- process bootstrap -> `internal/boot`, `internal/cli`
- desired-state schema -> `internal/spec`, `internal/state`
- runtime projection/policy -> `internal/route`, `internal/upstream`, `internal/runtime`, `internal/vip/runtime`
- application/API wiring -> `internal/app`, `internal/proxy`, `internal/dashboard`, `internal/admin`, `internal/raft`, `internal/vip`

Preserve this separation unless there is a strong reason to change it.
