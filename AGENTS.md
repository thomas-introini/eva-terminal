# Repository Guidelines

## Project Structure & Module Organization
`cmd/woossh/` runs the SSH storefront; `cmd/mockwoo/` supplies the development backend. `internal/` contains configuration/authentication, API clients, caching, durable shopper state (`storefront/`), and Bubble Tea screens (`tui/`). Go tests sit beside source; JSON fixtures live in `testdata/` and `cmd/mockwoo/testdata/`. `wordpress/eva-terminal-gateway/` contains the WooCommerce gateway and its integration checks. `docker/` provides local setup; `docs/terminal-checkout.md` covers checkout architecture and recovery.

## Build, Test, and Development Commands
Use Go 1.26.8+; `go.mod` prefers 1.27.1. Gateway builds require PHP 8.2+, Composer, and ZIP.

- `make dev`: start mock WooCommerce and SSH; connect with `ssh -p 23234 localhost`.
- `make build`: write both Go binaries to `bin/`.
- `make fmt` / `make lint`: run `gofmt` plus `go mod tidy` / `go vet`.
- `make test` / `make test-coverage`: run Go tests / generate `coverage.html`.
- `make gateway-build`: bundle the isolated Stripe SDK into `dist/eva-terminal-gateway-<version>.zip`.
- `make gateway-check`: check PHP syntax and SDK isolation after building.
- `make docker-up`, then `make dev-docker`: use real WooCommerce after gateway/environment setup.
- `make gateway-integration`: test native checkout in an isolated Docker store with simulated Stripe.

## Coding Style & Naming Conventions
Use `gofmt` (tab indentation), lowercase Go packages, `snake_case.go` filenames, and `CamelCase` exported identifiers. Match PHP's existing four-space indentation, `Eva_Terminal_*` classes, and `snake_case` methods. Reuse existing helpers before introducing dependencies.

## Testing Guidelines
Use Go's standard `testing` and `net/http/httptest`; name files `*_test.go` and functions `Test...`. Add regression checks for changed behavior, especially persistence, cart synchronization, and payment recovery. Run `go test -race ./...` for concurrency changes. No numeric coverage threshold is configured. Run gateway checks and integration tests for gateway changes.

## Commit & Pull Request Guidelines
Prefer recent history's `type(scope): summary` convention, e.g. `test(storefront): cover recovery and SSH checkout`. Keep commits focused. PRs should describe behavior changes, link relevant issues, report validation commands, and include terminal captures for UI changes. Update checkout documentation when configuration or recovery behavior changes.

## Security & Configuration
Copy `.env.example` to `.env`; keep bridge keys, SSH keys, and private `var/` state out of commits and logs. Match `EVA_BRIDGE_KEY` with WordPress's `EVA_TERMINAL_BRIDGE_KEY`. Use public SSH mode only locally. Keep Stripe credentials in WooCommerce and test Docker overrides out of deployments.
