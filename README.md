# WooCommerce Coffee Browser (SSH TUI)

A terminal-based WooCommerce product browser accessible via SSH. Built with Go using the Charm stack (Wish, Bubble Tea, Bubbles, Lip Gloss, Huh).

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                      SSH Client                              │
│                   ssh -p 23234 localhost                     │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────┐
│                    Wish SSH Server                           │
│  ┌─────────────────┐  ┌──────────────────────────────────┐  │
│  │  Auth Handler   │  │      Bubble Tea Middleware       │  │
│  │  (allowlist/    │  │  ┌────────┐ ┌─────────┐ ┌─────┐ │  │
│  │   public mode)  │  │  │ List   │→│ Details │→│ Cfg │ │  │
│  └─────────────────┘  │  └────────┘ └─────────┘ └─────┘ │  │
│                       └──────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────┐
│                      Data Layer                              │
│  ┌─────────────────┐         ┌───────────────────────────┐  │
│  │   TTL Cache     │ ──────→ │   WooCommerce REST API    │  │
│  │ (products,      │         │   /wp-json/wc/v3/...      │  │
│  │  variations)    │         └───────────────────────────┘  │
│  └─────────────────┘                                        │
└─────────────────────────────────────────────────────────────┘
```

The SSH server (Wish) handles authentication and spawns a Bubble Tea TUI session for each connection. The TUI fetches products from WooCommerce (or the mock server) with an in-memory TTL cache to reduce API calls.

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
# Start WordPress + WooCommerce + MySQL
make docker-up

# Wait for setup (watch logs in another terminal)
make docker-logs

# Once ready, start SSH server
make dev-docker
```

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
3. Creates REST API keys (displayed in logs)
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
| `c` | Configure (grind/size selection) |
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

In public mode, any SSH client can connect without authentication. This is intended **only for local development**.

## Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `SSH_ADDR` | `:23234` | SSH server listen address |
| `SSH_HOSTKEY_PATH` | `./.ssh_host_ed25519_key` | Path to host key (auto-generated if missing) |
| `SSH_AUTH_MODE` | `allowlist` | Auth mode: `allowlist` or `public` |
| `SSH_ALLOWLIST_PATH` | `./allowlist_authorized_keys` | Path to authorized keys file |
| `WOO_BASE_URL` | `http://127.0.0.1:18080` | WooCommerce API base URL |
| `WOO_CONSUMER_KEY` | _(empty)_ | WooCommerce API consumer key |
| `WOO_CONSUMER_SECRET` | _(empty)_ | WooCommerce API consumer secret |
| `CACHE_TTL_SECONDS` | `60` | Cache TTL in seconds |

## Connecting to a Real WooCommerce Store

1. Generate WooCommerce REST API keys in your store:
   - WooCommerce → Settings → Advanced → REST API
   - Create key with Read permissions

2. Configure environment:
   ```bash
   export WOO_BASE_URL=https://your-store.com
   export WOO_CONSUMER_KEY=ck_xxxxx
   export WOO_CONSUMER_SECRET=cs_xxxxx
   export SSH_AUTH_MODE=allowlist
   ```

3. Add your SSH public key to the allowlist:
   ```bash
   cat ~/.ssh/id_ed25519.pub >> allowlist_authorized_keys
   ```

4. Start the server:
   ```bash
   make woossh
   ```

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
│   ├── tui/                 # Bubble Tea UI (model, views, styles)
│   └── woo/                 # WooCommerce API client
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

# Build
make build          # Build binaries
make clean          # Clean build artifacts
```

## Features

- **Simple Products**: Browse and select grind size
- **Variable Products**: Choose size (250g/1kg) and grind size
- **Search**: Filter products by name
- **In-Stock Filter**: Show only available products
- **Caching**: In-memory TTL cache reduces API calls
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



