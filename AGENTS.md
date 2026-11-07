# AGENTS.md

This file provides guidance to Codex (claude.ai/code) when working with code in this repository.

## Язык общения

Отвечай пользователю по-русски.

## What this is

`git.dip.pics/dip/go/infrastructure.git` — a shared Go library of infrastructure building blocks for internal
services (not a deployable app itself). Other services `go get` this repo and compose the pieces they need:
HTTP server/client, gRPC client, observability (logging/metrics/tracing), DB connections, and a couple of
standalone utils (tx manager, data masking).

There is no formal technical spec (ТЗ) in the repo — `README.md` and the per-package `README.md` files are the
closest thing to a design doc, and this file summarizes what they say plus what the code actually does.

## Commands

```bash
go test ./...              # run all tests (make test does the same, with -v)
go test ./path/to/pkg/... -run TestName -v   # single test
go build ./...              # build everything

make lint                   # betteralign -apply, golangci-lint run --fix, goimports -w (see makefiles/50-tools.mk)
go tool golangci-lint run    # lint only, no autofix
go tool betteralign -apply ./...

make obs-up / obs-down / obs-status / obs-logs   # docker-compose observability stack (OTel Collector, Jaeger, Prometheus, Grafana) — examples/docker-compose.yml
make run-server              # run examples/http (observability stack must already be up)
make run-example             # obs-up + run-server combined
make test-endpoints          # curl the example server's endpoints
make load-test               # hey-based load test against the example server
make generate-api            # oapi-codegen for examples/http/api/openapi
```

Go 1.25. Tool dependencies (betteralign, golangci-lint, oapi-codegen, goimports) are declared via the
`tool (...)` block in `go.mod` and invoked with `go tool <name>`, not installed globally.

Note: `make lint` / `go tool golangci-lint run` currently fails with `can't load config: the format is
required` — `.golangci.yml` uses the v2 schema while the pinned tool is golangci-lint v1.64.8. Formatting
and alignment still work (`gofmt`, `go tool betteralign`), and `go vet ./...` is the usable substitute
until the tool is upgraded.

## Architecture

### Layout

- `http/server` — chi-based HTTP/HTTP3 server (`server.go`, `strategy.go`, `config.go`, `options.go`).
- `http/client` — resty-like fluent HTTP client (`client.go`, `request.go`, `options.go`, `serialize.go`).
- `http/server/middlewares` — RequestID, RealIP, request/response logging.
- `http/server/handlers` — reusable HTTP handlers.
- `grpc/client` — gRPC client with TLS, retry, keepalive, logging interceptors.
- `grpc/server` — placeholder only (`.gitkeep`), not implemented yet.
- `connections/postgres` — instrumented pgx pool (see its `README.md`); `migrate/` subpackage is optional.
- `connections/redis` — placeholder only (`.gitkeep`), not implemented yet, despite being listed as done in the root README.
- `observability/logger` — zerolog wrapper with context-field extraction and masking-on-write.
- `observability/metrics` — OpenTelemetry meter provider + HTTP/gRPC middleware, Prometheus/OTLP exporters.
- `observability/tracing` — OpenTelemetry tracer provider + HTTP middleware, OTLP/stdout exporters.
- `utils/txmanager` — cross-store (Postgres + Redis) transaction coordinator.
- `utils/masking` — package for masking sensitive data in JSON/XML/`map[string]any`.
- `examples/http` — a runnable service that wires all of the above together; treat it as the reference
  integration when changing any package's public API.

### Server: strategy pattern for HTTP vs HTTP/3

`http/server.Server` always builds a chi router and default middleware (Recoverer, RequestID, RealIP, Log), then
picks an implementation of the internal `serverStrategy` interface (`httpStrategy` or `http3Strategy`) in
`strategy.go` based on `Config.HTTP3Enabled`. HTTP/3 requires TLS to be enabled and forces TLS 1.3. TLS material
can come from files or from raw/base64 byte config fields (`TLSConfig.CertData`/`KeyData`/`CAData`,
`Base64Encoded`) — byte fields take priority over file paths. A second, optional `metricsServer` is started
alongside the main server when `MetricsEnabled` or `PprofEnabled` is set, exposing `/metrics` and
`/debug/pprof/*` on `Config.MetricsPort` (see `http/server/pprof_README.md` for endpoint-by-endpoint usage).
`Server.Run` uses `errgroup` to run the main and metrics listeners together and handles OS signals + context
cancellation for graceful shutdown, calling any hooks registered via `WithShutdownHook`.

### Observability: one story across logger/metrics/tracing

All three `observability/*` packages take the same `(app, version, environment)` triple at init and are meant to
be wired together (see `observability/README.md`). Metrics and tracing are both built on OpenTelemetry SDKs with
pluggable exporters (`prometheus` | `otlp-grpc` | `otlp-http` for metrics; `stdout` | `otlp-grpc` for tracing).
`logger` is a zerolog wrapper, not OTel-based, but reads its context keys via `SetContextKeys`/
`SetContextExtractor` so request-scoped fields (request ID, etc.) flow through automatically. Logger masking is
applied at the `io.Writer` level and is a *different, older* masking mechanism than the standalone
`utils/masking` module — don't assume they share config or behavior.

### Error propagation from handlers to the logging middleware (in progress)

`http/server/middlewares/log.go` is being changed (uncommitted, mid-implementation) to log the *original* handler
error on 5xx responses via a context key (`ErrorKey` / `SetError`), instead of synthesizing an error from the
status code. Two design docs sit next to the middleware, unindexed elsewhere:
`ERROR_HANDLING_OPTIONS.md` (comparing 5 ways to get the handler's error into the logging middleware; picks
option 4, a `HandleError(w, r, err, code)` helper) and `UNIVERSAL_ERROR_HANDLING.md` (a follow-up comparing 6
more approaches; picks a `BusinessLogic func(context.Context) error` handler-layering pattern as the long-term
direction, `ErrorResponseWriter` as the incremental compromise). Neither is finalized in code — check current
`log.go` and `http/server/handlers.go` before assuming which pattern is authoritative.

### TxManager: context-carried transactions

`txmanager` now covers **only** the cross-store case (Postgres + Redis in one operation). Postgres-only
transactions belong to `postgres.Pool.InTx`, which also does savepoints and serialization-failure retries.

`TxManager.Begin/BeginPostgres/BeginRedis` put a `*postgres.Tx` and/or `redis.Pipeliner` into the returned
`context.Context`. Repositories no longer branch on `GetPgTx(ctx)` — `postgres.Pool.Query` picks the
transaction out of the context itself, so the same repository method works inside and outside a transaction.
`CommitOrRollback(ctx, &err)` (always called via `defer` with a named `err` return) execs the Redis pipeline
first, then commits Postgres; any error rolls Postgres back, using a cancel-free context so an already
cancelled request still gets rolled back.

### Masking module

`utils/masking` is a regular package in the root module (no separate `go.mod`/`go.sum`) — import it as
`git.dip.pics/dip/go/infrastructure.git/utils/masking`. Masking supports three targets (`Masker.Mask` for
JSON/XML, `MapMasker.MaskMap` for `map[string]any`) with
key-based and value-based regex rules and built-in maskers for phone/card/passport/INN/full name/email/const;
inheritance-into-nested-structures rules differ subtly between the array and object cases (see its README's
"Семантика применения правил" section before changing masking behavior).

## Linting conventions worth knowing

`.golangci.yml` enables a large, explicit linter set (see the file for the full list/settings) with different
exclusion rules for `_test.go` files. A few conventions it enforces that aren't obvious from defaults:
- Import aliases must be `snake_case` (`import-alias-naming` revive rule) — this is deliberate, to avoid aliases
  looking like local variables.
- `gci` import grouping order is: alias imports, standard library, third-party, then anything under
  `git.dip.pics/dip` — enforced via `goimports`/`gci`, not just `gofmt`.
- Line length limit is 150 (not the gofmt default).
- `interface{}` is rewritten to `any` by the `gofmt` formatter config.
