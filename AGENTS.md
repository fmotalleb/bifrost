# Repository Guidelines

## Project Structure & Module Organization

Bifrost is a Go TCP reverse proxy and mixed HTTP/SOCKS5 proxy with weighted network-interface routing.

- `main.go` starts the Cobra CLI in `cmd/`.
- `config/` defines, loads, and validates configuration.
- `internal/proxy/` contains routing, interface selection, caching, telemetry, and proxy servers. Keep platform-specific dialing in `dial_route_<os>.go` files.
- `internal/metrics/server.go` serves Prometheus metrics, snapshots, and the inline HTML/CSS/JavaScript dashboard; `docs/assets/` holds documentation images.
- Tests and benchmarks live beside implementation files as `*_test.go`.

## Build, Test, and Development Commands

Use Go 1.27 or newer, as specified in `go.mod`. Adapt `config.yaml.example` to your local interfaces and upstream before running.

- `go run . --config config.yaml`: run the TCP proxy.
- `go run . socks --config config.yaml --socks 127.0.0.1:1080`: run the mixed proxy.
- `make build`: create a local GoReleaser snapshot build under `dist/`.
- `make test`: run tests with atomic coverage and generate `coverage.out` and `coverage.html`; race detection is enabled unless `CGO_ENABLED=0`.
- `make lint`: run golangci-lint with automatic fixes.
- `go tool golangci-lint fmt`: apply configured gofumpt and goimports formatting.
- `make mdlint`: check Markdown using Docker.
- `make precommit`: run the build/check pipeline plus vulnerability scanning. It may modify files; inspect the diff afterward. CI runs `make ci`, which also requires a clean checkout.

## Coding Style & Naming Conventions

Use Go formatting with tabs, lowercase package names, and descriptive exported identifiers. Follow `.golangci.yml`; explain specific `nolint` suppressions. Reuse `go-tools` helpers, retrieve loggers with `log.Of(ctx)`, and use structured Zap fields. Wrap underlying errors with `%w`. Services must honor context cancellation and release resources.

## Testing Guidelines

Use Go's standard `testing` package, `TestXxx` functions, and `BenchmarkXxx` benchmarks. Add regression coverage for routing, validation, shutdown, and telemetry changes. Run focused checks with `go test ./internal/proxy -run TestSelector`; use `make test` for the full suite. Coverage is collected without a configured minimum percentage.

## Commit & Pull Request Guidelines

Follow the history's type-prefixed style: `fix: ...`, `feat(socks): ...`, or `docs: ...`. Keep commits focused. PRs should describe behavior changes, link relevant issues, report validation, and include screenshots for dashboard changes.

## Agent & Configuration Guidance

Read `CONTRIBUTING_AI.md`. Validate new configuration fields and update `config.yaml.example`. Preserve unrelated work and always commit your changes before reporting completion.
