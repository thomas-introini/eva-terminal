.PHONY: dev test fmt lint build clean mockwoo woossh docker-up docker-down docker-logs docker-seed dev-docker container-check

-include .env
export

# Default target
all: build

# Build both binaries
build:
	go build -o bin/woossh ./cmd/woossh
	go build -o bin/mockwoo ./cmd/mockwoo

# Run development servers (mockwoo + woossh)
dev:
	@echo "Starting mock WooCommerce server and SSH server..."
	@echo "Connect with: ssh -p 23234 localhost"
	@echo ""
	@trap 'kill 0' EXIT; \
	EVA_BRIDGE_KEY=eva-local-mock-development-key-32-characters SSH_AUTH_MODE=public WOO_BASE_URL=http://127.0.0.1:18080 go run ./cmd/mockwoo & \
	sleep 1 && \
	CHECKOUT_ENABLED=true EVA_BRIDGE_KEY=eva-local-mock-development-key-32-characters SSH_AUTH_MODE=public WOO_BASE_URL=http://127.0.0.1:18080 go run ./cmd/woossh

# Run only the mock WooCommerce server
mockwoo:
	go run ./cmd/mockwoo

# Run only the SSH server (requires mockwoo or real Woo)
woossh:
	go run ./cmd/woossh

# Run tests
test:
	go test -v ./...

# Build and smoke-test the deployment container with isolated temporary storage
container-check:
	sh docker/container-check.sh

# Run tests with coverage
test-coverage:
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

# Format code
fmt:
	gofmt -w .
	go mod tidy

# Lint code
lint:
	go vet ./...

# Clean build artifacts
clean:
	rm -rf bin/
	rm -f coverage.out coverage.html
	rm -f .ssh_host_ed25519_key .ssh_host_ed25519_key.pub

# Generate host key
hostkey:
	ssh-keygen -t ed25519 -f .ssh_host_ed25519_key -N ""

# ============================================
# Docker WooCommerce targets
# ============================================

# Start Docker WooCommerce stack
docker-up:
	@echo "Starting WooCommerce Docker stack..."
	@echo "This will take a few minutes on first run."
	@echo ""
	docker compose up -d
	@echo ""
	@echo "Waiting for setup to complete..."
	@echo "Run 'make docker-logs' to watch progress."
	@echo ""
	@echo "Once ready, run 'make dev-docker' to start the SSH server."

# Stop Docker WooCommerce stack
docker-down:
	docker compose down

# Stop and remove all Docker data (fresh start)
docker-clean:
	docker compose down -v
	@echo "Removed containers and volumes"

# View Docker logs
docker-logs:
	docker compose logs -f

# View setup/seeding logs specifically
docker-logs-setup:
	docker compose logs -f wpcli

# Re-run product seeding
docker-seed:
	docker compose run --rm wpcli wp eval-file /var/www/html/seed-products.php

# Run SSH server connected to Docker WooCommerce
dev-docker:
	@echo "Starting SSH server connected to Docker WooCommerce..."
	@echo "Make sure 'make docker-up' has completed first!"
	@echo ""
	@echo "Connect with: ssh -p 23234 localhost"
	@echo ""
	SSH_AUTH_MODE=public WOO_BASE_URL=http://localhost:$${WOO_HTTP_PORT:-8080} go run ./cmd/woossh

# Help
help:
	@echo "Available targets:"
	@echo ""
	@echo "  Development (Mock Server - Fast):"
	@echo "    make dev           - Start mockwoo + woossh in public mode"
	@echo "    make mockwoo       - Run mock WooCommerce server only"
	@echo "    make woossh        - Run SSH server only"
	@echo ""
	@echo "  Development (Docker WooCommerce - Realistic):"
	@echo "    make docker-up     - Start WordPress + WooCommerce + MySQL"
	@echo "    make docker-down   - Stop Docker stack"
	@echo "    make docker-clean  - Stop and remove all Docker data"
	@echo "    make docker-logs   - View Docker logs"
	@echo "    make docker-seed   - Re-run product seeding"
	@echo "    make dev-docker    - Start woossh connected to Docker WooCommerce"
	@echo ""
	@echo "  Testing & Quality:"
	@echo "    make test          - Run all tests"
	@echo "    make container-check - Check the deployment image, SSH auth and persistence"
	@echo "    make test-coverage - Run tests with coverage report"
	@echo "    make fmt           - Format code and tidy modules"
	@echo "    make lint          - Run go vet"
	@echo ""
	@echo "  Build & Utilities:"
	@echo "    make build         - Build binaries to bin/"
	@echo "    make clean         - Remove build artifacts"
	@echo "    make hostkey       - Generate SSH host key"
.PHONY: gateway-build gateway-check gateway-integration

gateway-build:
	sh wordpress/eva-terminal-gateway/build.sh

gateway-check:
	find wordpress/eva-terminal-gateway/includes -name '*.php' -print0 | xargs -0 -n 1 php -l
	php -l wordpress/eva-terminal-gateway/eva-terminal-gateway.php
	php wordpress/eva-terminal-gateway/tests/sdk.php

gateway-integration: gateway-build
	EVA_BRIDGE_KEY=eva-terminal-local-check-key-32-characters WOO_HTTP_PORT=18081 docker compose -p eva-terminal-check -f docker-compose.yml -f docker/compose.test.yml up --wait -d wordpress
	@echo "Preparing the isolated test store..."
	EVA_BRIDGE_KEY=eva-terminal-local-check-key-32-characters WOO_HTTP_PORT=18081 docker compose -p eva-terminal-check -f docker-compose.yml -f docker/compose.test.yml run --rm --no-deps wpcli
	EVA_BRIDGE_KEY=eva-terminal-local-check-key-32-characters python3 wordpress/eva-terminal-gateway/tests/integration.py
	EVA_BRIDGE_KEY=eva-terminal-local-check-key-32-characters WOO_HTTP_PORT=18081 docker compose -p eva-terminal-check -f docker-compose.yml -f docker/compose.test.yml run --rm --no-deps --entrypoint wp wpcli eval-file /var/www/html/wp-content/plugins/eva-terminal-gateway/tests/orders.php
	EVA_BRIDGE_KEY=eva-terminal-local-check-key-32-characters WOO_HTTP_PORT=18081 docker compose -p eva-terminal-check -f docker-compose.yml -f docker/compose.test.yml run --rm --no-deps --entrypoint wp wpcli eval-file /var/www/html/wp-content/plugins/eva-terminal-gateway/tests/analytics.php
