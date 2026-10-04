# WooCommerce Coffee Browser (SSH TUI)

A terminal-based WooCommerce product browser accessible via SSH. Built with Go using the Charm v2 stack (Wish, Bubble Tea, Bubbles, Lip Gloss, Huh).

Requires Go 1.26.8 or newer; `go.mod` selects Go 1.27.1 as the preferred toolchain. Charm SSH uses the canonical `charm.land/ssh` module.

The `.env` file is optional. Build and validation commands work from a fresh checkout using defaults or exported environment variables.

## Architecture

The terminal browses a complete local catalog snapshot and keeps one durable WooCommerce guest cart per verified SSH key. Quantity updates synchronize in the background; Woo calculates checkout totals and manages stock/orders. Final card or wallet payment opens Stripe hosted Checkout. A custom Woo gateway journals attempts and recovers payments through signed webhooks and terminal polling.

See [setup, recovery and rollout checklist](docs/terminal-checkout.md). Build the gateway with `make gateway-build`; the Go server needs only its bridge key, never Stripe credentials.

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

You'll see the coffee product browser with:
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

| Key | Action |
|-----|--------|
| `/` | Search products |
| `f` | Toggle "in-stock only" filter |
| `r` | Refresh product list |
| `Enter` | Select product / confirm |
| `c` (product list/details) | Open cart / configure product |
| `a` | Add the configured product to cart |
| `+` / `-` | Adjust selected cart quantity |
| `c` / `u` (cart) | Apply / remove coupon |
| `o` (cart) | Start checkout |
| `Enter` (review) | Confirm Woo's quote |
| `o` | Open the current payment attempt |
| `x` (cart/payment) | Cancel a pending payment |
| `Esc` / `Backspace` | Go back |
| `q` / `Ctrl+C` | Quit |

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

Copy `.env.example` to `.env`. Set `WOO_BASE_URL`, `STATE_DIR`, and `EVA_BRIDGE_KEY` (the same value as WordPress's `EVA_TERMINAL_BRIDGE_KEY`). New checkouts default to disabled; use `CHECKOUT_ENABLED=true` after staging validation. `WOO_STORE_PREFIX` defaults to `/wp-json/wc/store/v1`; `CACHE_TTL_SECONDS=60` controls background catalog refresh. Native Store API browsing requires no Woo consumer keys.

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
make test-coverage  # Run tests with coverage report
make fmt            # Format code
make lint           # Run go vet
make gateway-check  # Check PHP syntax and isolated SDK
make gateway-integration # Native Woo checkout and simulated Stripe checks

# Build
make build          # Build binaries
make gateway-build  # Build dist/eva-terminal-gateway.zip
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

