# WooCommerce Coffee Browser (SSH TUI)

A terminal-based WooCommerce product browser accessible via SSH. Built with Go using the Charm v2 stack (Wish, Bubble Tea, Bubbles, Lip Gloss, Huh).

Requires Go 1.26.8 or newer; `go.mod` selects Go 1.27.1 as the preferred toolchain. Charm SSH uses the canonical `charm.land/ssh` module.

The `.env` file is optional. Build and validation commands work from a fresh checkout using defaults or exported environment variables.

## Architecture

The terminal browses a complete local catalog snapshot and keeps one durable WooCommerce guest cart per verified SSH key. Quantity updates synchronize in the background; Woo calculates checkout totals and manages stock/orders. Final card or wallet payment opens Stripe hosted Checkout. A custom Woo gateway journals attempts and recovers payments through signed webhooks and terminal polling.

Start with the [deployment checklist](docs/deployment-checklist.md) for private staging and promotion to a production canary. See [checkout setup and recovery](docs/terminal-checkout.md) for gateway details. Build the gateway with `make gateway-build`; the Go server needs only its bridge key, never Stripe credentials.

For a single-instance container on Coolify, follow the [Coolify deployment steps](docs/deployment-checklist.md#coolify-deployment). The root Dockerfile defaults to allowlisted access with checkout and analytics disabled; `make container-check` verifies the image and persistent storage locally.

Optional anonymous SSH analytics use a dedicated Umami website; confirmed purchases are sent by the gateway even after disconnects. Tracking defaults to off. See [setup, event semantics, delivery limits and staging rollout](docs/terminal-analytics.md).

## Quick Start (Development)

There are two development workflows:

### Option A: Mock Server (Fast, Offline)

```bash
# Clone and enter directory
git clone <repo>
cd eva-terminal-go

# Download dependencies
go mod download

# Start dev servers (mock Woo + SSH in public mode)
make dev
```

### Option B: Docker WooCommerce (Realistic)

```bash
# Build the gateway SDK (requires PHP 8.2+, Composer and ZIP)
make gateway-build

# Copy the environment template and set EVA_BRIDGE_KEY to a random value
cp .env.example .env
# Generate a key with: openssl rand -hex 32

# Start WordPress + WooCommerce + MySQL
make docker-up

# Wait for setup (watch logs in another terminal)
make docker-logs

# Once ready, start SSH server
make dev-docker
```

Configure the gateway's Stripe test credentials in WooCommerce and set `CHECKOUT_ENABLED=true` in `.env` to enable checkout. The [rollout checklist](docs/terminal-checkout.md) describes webhook and staging setup. `make gateway-integration` runs a separate store with simulated Stripe responses.

### Connect

In another terminal:

```bash
ssh -p 23234 localhost
```

Each connection briefly shows a centered ASCII EVA logo for one second, with a gently pulsing orange dot. Monochrome terminals and `NO_COLOR` show a static logo. The storefront loads in parallel, and reconnecting resumes any pending payment after the splash.

You'll then see the coffee product browser with:
- Product list (name, price, stock status)
- Product details view
- Variable product configuration (size + grind selection)

## Development Workflows

| Workflow | Command | Use Case |
|----------|---------|----------|
| Mock (fast) | `make dev` | Unit tests, quick iteration, offline |
| Docker (real) | `make docker-up && make dev-docker` | Integration testing, real WooCommerce |

### Docker WooCommerce Details

The Docker stack includes:
- **WordPress** with WooCommerce plugin (port 8080)
- **MySQL 8.0** database
- **WP-CLI** for automated setup and seeding

On first run, the setup script:
1. Installs WordPress
2. Installs and activates WooCommerce
3. Activates the terminal gateway and configures shipping and test coupons
4. Seeds sample coffee products

```bash
# Useful Docker commands
make docker-up      # Start stack
make docker-down    # Stop stack
make docker-clean   # Stop and remove all data
make docker-logs    # View all logs
make docker-seed    # Re-seed products

# WordPress admin
# URL: http://localhost:8080/wp-admin
# User: admin
# Pass: admin
```

## Keyboard Shortcuts

`?` opens help for the current screen. The footer shows the primary actions; help lists all enabled actions. Close help with `Esc` or `?` to keep your screen and input focus.

| Key | Action |
|-----|--------|
| `Enter` | Add the displayed size, grind, and quantity; elsewhere perform the primary action or confirm the current order total |
| `Esc` | Go back; in search, clear the query and immediately restore results |
| `c` | Open the cart outside forms and text entry |
| `?` | Open or close help |
| `Ctrl+C` | Quit everywhere |
| `q` | Quit outside forms and text entry |
| `/` (shop) | Search locally as you type or paste; `Enter` keeps the query |
| `f` / `r` (shop) | Toggle stock filter / refresh catalog and selected options |
| `a` (shop) | Add the displayed configuration |
| `↑` / `↓` | Browse coffees, select delivery or cart items, or scroll review/payment |
| `Tab` / `Shift+Tab` (shop) | Focus size, grind, quantity, or Add; skip read-only options |
| `←` / `→` (shop) | Change the focused size, grind, or quantity; unit price updates immediately |
| `+` / `-` (shop) | Adjust draft quantity, minimum one |
| `PgUp` / `PgDown` | Scroll the product description or other long screens |
| `Home` / `End` | First / last coffee in the shop; scroll other long screens |
| `+` / `-`, `d` (cart) | Change quantity / delete the selected item |
| `p` / `u` (cart) | Apply / remove a coupon; type or paste its code and press `Enter` |
| `s` | Return to the shop outside forms/text entry, preserving search, selection, and product drafts |
| `o` | Start checkout, confirm review, or open an existing pending payment |
| `h` (cart/review) | Change delivery when multiple rates are available |
| `r` (cart/review/payment) | Retry synchronization / refresh review / recheck payment status |
| `y` (payment) | Request copying the full payment URL to your terminal's clipboard |
| `x` (cart/payment) | Ask to cancel an active payment; `Enter` confirms, `Esc` keeps it |
| `Tab` / `Shift+Tab` (forms) | Next / previous field |

Shopping follows **Address → Delivery → Review → Payment**, with Delivery omitted when unnecessary. Required address fields are marked `*`; the country starts at Italy (`IT`). Review confirms the address and current store-calculated total. A changed quote requires another explicit confirmation. Adding coffee still opens the cart.

All screens use a panel centered horizontally and vertically, capped at 100 columns × 28 rows. At 24 rows and taller, an ASCII EVA logo with a steady orange dot sits in the space above the panel; it doubles to 32×6 characters at 40 rows and taller. Its segmented header shows EVA, shop, and the cart’s confirmed total and quantity. At 80 columns and wider, products appear beside the selected coffee’s details and inline options; narrower supported terminals stack these sections. Choices stay with each product while browsing and returning from the cart. The UI supports 60×18 and larger terminals. Smaller terminals show a resize message and preserve shopping state. Long content scrolls while totals and primary controls stay visible. Cart changes show “totals updating”; checkout waits for synchronization and validation. Pending payments lock cart edits and remain available through `o` or reconnect.

Payment instructions stay open until explicit navigation. The order number, amount, confirmation status, and link time remaining remain visible. Clipboard copying uses the terminal's OSC 52 support; “Copy requested” reports the request, and the full URL remains accessible for manual copying. The coffee theme adapts to the terminal background and color capability and respects `NO_COLOR`.

## Authentication Modes

### 1. Allowlist Mode (Default)

Only SSH public keys listed in the allowlist file can connect.

```bash
# Set mode (default)
export SSH_AUTH_MODE=allowlist
export SSH_ALLOWLIST_PATH=./allowlist_authorized_keys

# Add your public key to the allowlist
cat ~/.ssh/id_ed25519.pub >> allowlist_authorized_keys

# Start server
make woossh
```

The allowlist file uses OpenSSH `authorized_keys` format (one key per line).

### 2. Public Mode

⚠️ **WARNING: Public mode allows anyone to connect. Do NOT use on internet-facing servers!**

```bash
export SSH_AUTH_MODE=public
make woossh
```

In public mode, any SSH public key can connect after the normal SSH proof of possession. This is intended **only for local development**.

## Configuration

Copy `.env.example` to `.env`. Set `WOO_BASE_URL`, `STATE_DIR`, and `EVA_BRIDGE_KEY` (the same value as WordPress's `EVA_TERMINAL_BRIDGE_KEY`). New checkouts default to disabled; use `CHECKOUT_ENABLED=true` after staging validation. `WOO_STORE_PREFIX` defaults to `/wp-json/wc/store/v1`; `CACHE_TTL_SECONDS=60` controls background catalog refresh. `CATALOG_REFRESH_COOLDOWN_SECONDS` defaults to `30` and accepts integer values from `1` to `86400`. All catalog refreshes, including `r` across SSH connections, share this cooldown after each completed attempt. During the cooldown they reuse the cached catalog and the last refresh result, including errors; no new Woo requests are made. Restart the SSH server after changing the setting. Native Store API browsing requires no Woo consumer keys.

Add your SSH key to `allowlist_authorized_keys`, then run `make woossh`. Public development mode accepts any public key after SSH verifies possession; cart identity still comes from that key.

## Project Structure

```
├── cmd/
│   ├── woossh/main.go       # SSH server entry point
│   └── mockwoo/main.go      # Mock WooCommerce server
├── docker/
│   ├── setup.sh             # WooCommerce setup script
│   ├── seed-products.php    # Product seeding script
│   └── env.example          # Environment template
├── internal/
│   ├── auth/                # SSH key allowlist handling
│   ├── cache/               # Generic TTL cache
│   ├── config/              # Environment configuration
│   ├── storeapi/            # Native Store API and checkout bridge client
│   ├── storefront/          # Durable catalog and shopper controllers
│   ├── tui/                 # Bubble Tea UI (model, views, styles)
│   └── woo/                 # WooCommerce API client
├── wordpress/eva-terminal-gateway/ # Native Woo gateway and isolated Stripe SDK
├── docs/terminal-checkout.md # Setup, recovery and release checklist
├── testdata/                # Test fixtures
├── docker-compose.yml       # Docker WooCommerce stack
├── Makefile
└── README.md
```

## Make Targets

```bash
# Development (Mock - Fast)
make dev            # Start mockwoo + woossh

# Development (Docker - Realistic)
make docker-up      # Start WordPress + WooCommerce + MySQL
make docker-down    # Stop Docker stack
make docker-clean   # Stop and remove all data
make docker-logs    # View Docker logs
make docker-seed    # Re-seed products
make dev-docker     # Start woossh with Docker WooCommerce

# Testing & Quality
make test           # Run all tests
make container-check # Build and smoke-test the deployment container
make test-coverage  # Run tests with coverage report
make fmt            # Format code
make lint           # Run go vet
make gateway-check  # Check PHP syntax and isolated SDK
make gateway-integration # Native Woo checkout and simulated Stripe checks

# Build
make build          # Build binaries
make gateway-build  # Build dist/eva-terminal-gateway-<version>.zip
make clean          # Clean build artifacts
```

## Features

- **Simple Products**: Browse and select grind size
- **Variable Products**: Choose size (250g/1kg) and grind size
- **Search**: Filter products by name
- **In-Stock Filter**: Show only available products
- **Catalog snapshots**: Local navigation and search make no API calls
- **HTML Stripping**: Clean product descriptions

## Testing

[CI](.github/workflows/ci.yml) runs on pull requests and pushes to `main`. Its three jobs check Go formatting, vetting, race tests and builds; the deployment container; and the PHP gateway build, syntax and SDK isolation. Go dependencies are cached, and the toolchain comes from `go.mod`. Jobs run with read-only repository permissions and cancel superseded runs. Use PHP 8.2 for gateway builds to match CI and the Docker test store; the current SDK scoping tool failed isolation with the local PHP 8.5 toolchain.

Run the full checkout suite before gateway releases through **Actions → Gateway integration → Run workflow**, selecting the release branch. The [manual workflow](.github/workflows/gateway-integration.yml) uses an isolated Docker store with simulated Stripe/Umami, prints store logs on failure and removes its test containers and volumes. It needs no production credentials. Commit and push the workflow files to `main` before using the manual trigger.

Configure branch protection or a ruleset to require the **Go**, **Container** and **Woo gateway** checks before merging into `main`. Keep Coolify staging deployments manual and deploy the passing commit; these workflows do not configure repository rules or trigger deployments.

```bash
# Run all tests
go test ./...

# Run with verbose output
go test -v ./...

# Run specific package tests
go test -v ./internal/woo
go test -v ./internal/cache
go test -v ./internal/tui
```

## License

MIT
