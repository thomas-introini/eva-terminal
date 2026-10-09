# Deployment checklist

Start with a private staging deployment, a few SSH testers and real Stripe test mode. Complete the staging checks before enabling live payments. These boxes record deployment work still to perform; local automated tests do not complete them.

## 1. Prepare the environments

- [ ] Choose the Go host, operating system/CPU, SSH hostname and port, and HTTPS WooCommerce staging URL. Record the application revision and gateway version being deployed.
- [ ] Prepare a separate staging Woo store with the production plugins, products, shipping/tax settings and required checkout fields. Use separate state, bridge credentials and Stripe test credentials from production.
- [ ] Confirm WooCommerce 9.9+, PHP 8.2+ with `curl`, `json` and `mbstring`, and InnoDB order/attempt tables. The terminal gateway currently requires EUR with two decimal places and guest checkout.
- [ ] Decide which testers' public SSH keys to allow and where private state/database backups will be stored.

## 2. Validate and build the release

Use Go 1.26.8+; the repository selects Go 1.27.1. Gateway builds also need Composer and ZIP. Run from the revision being deployed:

```sh
make test
go test -race ./...
go vet ./...
make build
make gateway-build
make gateway-check
make gateway-integration
```

- [ ] Confirm the release commit passes the **Go**, **Container** and **Woo gateway** CI checks. Before gateway releases, run **Actions → Gateway integration → Run workflow** against the release branch and retain its results. See [CI and manual integration usage](../README.md#testing). Configure required checks on `main` separately; keep Coolify staging deployments manual so it cannot deploy before CI finishes.
- [ ] Save the results and retain the corresponding Go binary and gateway ZIP. Build `bin/woossh` for the target operating system and CPU; a binary built on macOS cannot run on Linux.
- [ ] Deploy `bin/woossh` and `dist/eva-terminal-gateway-<version>.zip`. The ZIP includes the isolated Stripe SDK.
- [ ] Keep `docker/compose.test.yml`, the fake Stripe/Umami MU plugins and `EVA_TERMINAL_FAKE_*` settings confined to automated tests. The normal Docker setup seeds a development store with Italy-only settings; it is not a production deployment template.

## 3. Install the WooCommerce gateway

- [ ] Back up the WordPress database, then install and activate the built gateway ZIP.
- [ ] Generate a random bridge key, for example with `openssl rand -hex 32`. Store the same value as WordPress's `EVA_TERMINAL_BRIDGE_KEY` and Go's `EVA_BRIDGE_KEY`. Keep it in private backend configuration.
- [ ] In WooCommerce → Settings → Payments → EVA Terminal Stripe Checkout, select **Stripe test mode** and configure its test secret API key. Keep new terminal checkouts disabled until webhook/cron setup and the browsing smoke check are complete.
- [ ] Check selling/shipping locations, shipping zones and methods for every destination you intend to support. Enable and configure coupons if used. See [Woo shipping zones](https://woocommerce.com/document/setting-up-shipping-zones/).
- [ ] Confirm the usual website checkout and its payment gateways still work after activation.

Gateway, webhook event and recovery details are in [terminal checkout operations](terminal-checkout.md#install-the-gateway).

## 4. Set up the Go service

- [ ] Run one Go instance as a dedicated unprivileged service user. Give it a persistent, private directory for the SSH host key and shopper state, owned by that user. Do not use an ephemeral release directory for state.
- [ ] Install the testers' public keys in the allowlist file. Configure the service to load a protected environment file, start the binary, restart on failure and shut down gracefully on SIGTERM. The binary does **not** load `.env` itself; the Makefile exports it during development.
- [ ] Use explicit paths and an HTTPS Woo URL. Adapt this starting configuration to the host:

```dotenv
SSH_ADDR=:23234
SSH_AUTH_MODE=allowlist
SSH_ALLOWLIST_PATH=/etc/eva-terminal/allowlist_authorized_keys
SSH_HOSTKEY_PATH=/var/lib/eva-terminal/ssh_host_ed25519_key
STATE_DIR=/var/lib/eva-terminal/state
WOO_BASE_URL=https://staging-store.example.com
WOO_STORE_PREFIX=/wp-json/wc/store/v1
CACHE_TTL_SECONDS=60
CATALOG_REFRESH_COOLDOWN_SECONDS=30
EVA_BRIDGE_KEY=<same-private-key-as-wordpress>
CHECKOUT_ENABLED=false
UMAMI_ENABLED=false
UMAMI_ENVIRONMENT=staging
```

- [ ] Create `/var/lib/eva-terminal` before startup, with service ownership and mode `0700`. Protect the environment file and private key with mode `0600`; ensure the service can read its allowlist. Shopper directories/files use `0700`/`0600`.
- [ ] Allow the chosen SSH port for testers and outbound HTTPS to WooCommerce. Confirm proxy/WAF or staging authentication does not block required Store API and bridge requests.
- [ ] Start the service. Connect with `ssh -i /path/to/tester_key -p 23234 user@terminal-staging.example.com`; the verified key identifies the shopper, regardless of SSH username. Confirm the catalog loads and an unlisted key is rejected.
- [ ] Verify a service restart preserves the host key, cart and store identity. Back up the host key and `STATE_DIR` securely alongside the WordPress database. Keep the Woo URL/prefix stable during an environment's lifetime.

`CATALOG_REFRESH_COOLDOWN_SECONDS` accepts integers from 1 to 86400. Restart the service after changing environment settings. Woo REST consumer keys and Stripe credentials are not needed by Go.

## 5. Configure webhooks and recovery

- [ ] Register a Stripe test-mode webhook at `https://staging-store.example.com/wp-json/eva-terminal/v1/stripe/webhook`, using the API version and event list in [gateway setup](terminal-checkout.md#install-the-gateway). Use snapshot events and save this endpoint's signing secret in the gateway's **test webhook secret** setting. Stripe requires registered endpoints to be publicly accessible over HTTPS; ensure staging authentication allows this route. See [Stripe webhook setup](https://docs.stripe.com/webhooks).
- [ ] Configure the host scheduler to run due WordPress cron events at least once per minute as the WordPress service user. For example, with the actual WordPress path:

```sh
wp --path=/path/to/wordpress cron event run --due-now
```

- [ ] Verify `eva_terminal_reconcile` and `eva_terminal_analytics` are scheduled, and that the scheduler runs successfully without website visitors. See [WP-CLI cron command](https://developer.wordpress.org/cli/commands/cron/event/run/).
- [ ] Enable **new terminal checkouts** in the gateway, then set Go `CHECKOUT_ENABLED=true` and restart the service. Keep Stripe test mode selected.

## 6. Run the staging acceptance checks

Use [Stripe test payment methods](https://docs.stripe.com/testing) for these checks.

- [ ] Browse variable/simple products, select size/grind, add/update/remove cart items, and compare totals to WooCommerce.
- [ ] Apply/remove valid coupons and submit invalid/expired coupons. Check discounts and any free-shipping conditions.
- [ ] Test Italy and each intended foreign destination using its two-letter country code. Check postcode/state validation, available rates, taxes and final totals; confirm an unsupported destination cannot complete shipping checkout.
- [ ] Complete successful, declined and 3DS payments, and eligible wallet payments. Verify one Woo order, correct stock changes and customer emails.
- [ ] Create a payment link, disconnect SSH, pay, then reconnect using the same key. Confirm the webhook/cron and terminal recover the same order without duplicating it.
- [ ] Restart the Go service with an unpaid attempt, then reconnect and recover/cancel it. Exercise cancellation, expiry and payment completing during cancellation; verify paid orders stay paid and unpaid reservations are released appropriately.
- [ ] Test partial/full refunds and dispute handling in Stripe's testing environment; compare Stripe and Woo state. Exercise duplicate webhook delivery.
- [ ] Open two connections with the same key and two with different keys: shared carts for the former, isolated carts for the latter.
- [ ] Measure Woo latency and API counts with realistic catalog size. Repeated catalog `r` refreshes across connections should respect the shared cooldown, including during Woo failures. Cart synchronization and payment rechecks have no equivalent time cooldown, so keep the first test group small and monitor their traffic.

Record results and unresolved issues before promoting the release.

## 7. Enable analytics separately, if wanted

- [ ] Verify the installed Umami version and real collector receipts/identity behavior using the [analytics staging checklist](terminal-analytics.md#manual-staging-verification-and-reports).
- [ ] Configure Go and the gateway for the same dedicated staging website, hostname and environment. Verify a purchase delivered after SSH disconnect and compare revenue to Woo.
- [ ] Use a separate dedicated production website with `environment=production` when promoting. Analytics can stay disabled while checkout testing continues.

## 8. Promote to a small production canary

- [ ] Complete staging acceptance checks. Back up production WordPress and private Go state; record a rollback plan and who monitors the rollout.
- [ ] Repeat gateway/service setup against the production Woo URL with production-specific bridge credentials and persistent state. Keep checkout disabled while checking catalog access and shipping/coupon configuration.
- [ ] Configure Stripe live credentials, a live webhook endpoint and its live signing secret. Confirm recovery cron works. Retain credentials needed to settle any existing attempts.
- [ ] Enable new checkouts in both gateway and Go, restart Go, and begin with a small SSH allowlist. Monitor order/Stripe agreement, webhook failures, cron execution, pending attempts, stock reservations and API traffic before adding testers.
- [ ] Keep logs free of bridge keys, Cart-Tokens, addresses, order keys and payment URLs. Exclude query strings from proxy logs on attempt routes; they contain customer references.

To stop new orders, set Go `CHECKOUT_ENABLED=false` and restart it and/or disable **new** gateway checkouts. Keep the gateway active, webhook reachable, cron running, credentials available and private recovery state intact until existing attempts settle. See [rollback and release checks](terminal-checkout.md#release-checklist).

## Coolify deployment

Use this path for one SSH instance on a standalone Linux Docker server, connected to the existing WooCommerce host. The root `Dockerfile` builds only `cmd/woossh`, using Go `1.27.1-alpine3.24` and an Alpine `3.24.2` runtime. It builds for the server's native architecture and runs the binary directly as UID/GID `10001:10001`. The development `docker-compose.yml` and test overrides remain separate.

### First deployment

1. Run `go test ./...` and `make container-check` from the release checkout. The container check needs Docker, OpenSSH and `tar`; it builds the image with temporary keys, a new volume and automatically allocated loopback host ports. It checks non-root execution, CA certificates, image contents, allowlist authentication, listener health, graceful shutdown and storage/host identity across replacement, then removes its test resources. It does not require Woo or Stripe.
2. Review the release, commit and push it to the Git branch/tag Coolify will deploy. Record the commit and gateway version. Coolify's Git build cannot deploy uncommitted local files; leave automatic deployments off during initial staging.
3. Create an application from that repository with the [Dockerfile build strategy](https://coolify.io/docs/applications/builds/dockerfile). Use the repository root (`/`) as **Base Directory** and its root `Dockerfile` as **Dockerfile Location**. Keep the image entrypoint unchanged.
4. Set **Ports Exposes** to `23234` and **Ports Mappings** to `23234:23234`. Leave **Domains** empty: SSH uses [direct host port mapping](https://coolify.io/docs/core/networking-in-coolify), bypassing HTTP routing. Point the terminal hostname's DNS directly at this server and allow inbound TCP `23234` for staging testers. Permit outbound HTTPS to WooCommerce. Connect with `ssh -i /path/to/tester_key -p 23234 user@terminal-staging.example.com`.
5. Under [Persistent Storage](https://coolify.io/docs/applications/configuration/persistent-storage), add one **Volume Mount** with destination `/data`. Record its actual Docker volume name for backups. Add a managed **File Mount** at `/etc/eva-terminal/allowlist_authorized_keys` containing one tester public key per line in OpenSSH `authorized_keys` format. Ensure UID `10001` can read it; public keys can use mode `0644`. See [file mount setup and editing](https://coolify.io/docs/core/persistent-storage/storage-mounts/file-mounts).
6. Add the runtime environment below. In [Environment Variables](https://coolify.io/docs/applications/configuration/environment-variables), set **Build time** to **Not available during build** and **Runtime** to **Available in the container**. Mark `EVA_BRIDGE_KEY` as a secret. No runtime configuration or credentials are needed during this image build.

```dotenv
WOO_BASE_URL=https://staging-store.example.com
WOO_STORE_PREFIX=/wp-json/wc/store/v1
EVA_BRIDGE_KEY=<same-private-key-as-wordpress>
SSH_ADDR=0.0.0.0:23234
SSH_AUTH_MODE=allowlist
SSH_ALLOWLIST_PATH=/etc/eva-terminal/allowlist_authorized_keys
SSH_HOSTKEY_PATH=/data/ssh_host_ed25519_key
STATE_DIR=/data/state
CACHE_TTL_SECONDS=60
CATALOG_REFRESH_COOLDOWN_SECONDS=30
CHECKOUT_ENABLED=false
UMAMI_ENABLED=false
UMAMI_ENVIRONMENT=staging
```

7. Use the image's [command health check](https://coolify.io/docs/applications/configuration/health-checks); Coolify detects the Dockerfile `HEALTHCHECK`. It runs `nc -z -w 3 127.0.0.1 "${SSH_ADDR##*:}"` every 30 seconds, with a 5-second timeout, a 10-second startup period and three retries. It checks the SSH listener without contacting Woo or Stripe. A healthy container can still have an empty allowlist or an unavailable Woo store.
8. Keep exactly one instance. The published host port makes Coolify [stop the current container before starting its replacement](https://coolify.io/docs/applications/deployments/rolling-updates). Verify that behavior in deployment logs; never overlap instances writing `/data`. Set **Advanced → Operations → Stop Grace Period (seconds)** to `15`. Disable preview deployments so they cannot reuse this volume or host port. Redeployments disconnect SSH sessions and briefly interrupt access; testers reconnect with the same key.
9. Deploy, inspect logs and health, and confirm an allowed key loads the catalog while an unlisted key fails. Verify cart edits work with checkout disabled and analytics off. Before enabling Stripe test checkout, complete [gateway installation](#3-install-the-woocommerce-gateway) and [webhook/cron setup](#5-configure-webhooks-and-recovery) on the Woo host.

### Storage and allowlist changes

The image prepares `/data` and `/data/state` with owner `10001:10001` and mode `0700`. A new Docker named volume inherits these directories. For existing storage or a directory bind mount, stop the app first, create the `state` subdirectory, and assign UID/GID `10001:10001` to the contents. Use `0700` for private directories and `0600` for private keys/state files. Verify this UID can write `/data` and `/data/state` before starting; a bind mount hides the image's prepared permissions. The service starts without root privileges and cannot repair root-owned storage.

The image's empty allowlist placeholder rejects all keys. To add or revoke testers, load the managed file's current content from the server, edit/save the public keys, then restart the app. The allowlist is read at startup. Verify a newly allowed key succeeds and a revoked key fails after restarting. Keep a separate copy of the allowlist; Coolify's volume backups do not include managed file mounts.

### Restart, redeployment and staging acceptance

For runtime environment or allowlist changes, save them and restart the application. For code changes, review, test, commit and push the release, then deploy the recorded revision. Reuse the same `/data` volume, allowlist destination, Woo URL and Store API prefix. Record the SSH host fingerprint before and after; it must stay unchanged.

- [ ] Add items using an allowed key, redeploy through Coolify, reconnect with that key and verify the same cart and totals return.
- [ ] After webhook/cron setup and enabling checkout in Stripe test mode, create an unpaid attempt, redeploy, reconnect and recover or cancel the same attempt. Also pay after an SSH disconnect and confirm recovery after redeployment without a duplicate order.
- [ ] Confirm only one Go container writes the volume, shutdown finishes within 15 seconds, and the replacement passes the image health check. Run the remaining [staging acceptance checks](#6-run-the-staging-acceptance-checks) before promotion.

### Backup and rollback

Stop the SSH app for a consistent backup, archive the entire `/data` volume preserving numeric ownership and permissions, then start it again. Keep `/data/state`, the private/public SSH host keys, the allowlist, runtime configuration and the corresponding WordPress database backup in protected storage. Record the Woo store identity and release revision. To restore, stop the app, restore the volume to the same destination, verify UID `10001` ownership/private permissions, restore runtime settings and the allowlist, then start one instance and check host identity and recovery.

Before a rollback, disable new checkout and retain a current backup. Use [Coolify Rollback](https://coolify.io/docs/applications/deployments/rollbacks) to deploy a retained known-good image, or deploy its reviewed Git revision again. Retain the previous image and verify that it supports the current state format. Coolify rollback uses current runtime settings and does not restore storage: keep the current volume and pending-attempt records. Keep the Woo gateway, webhook, recovery cron and credentials active until existing payments settle. Recheck host identity, cart restoration and pending payments before enabling new checkout.

Actual Coolify deployment, Git publication and live payments remain rollout work. Local smoke tests verify container behavior; they do not complete staging acceptance. Losing `/data` loses shopper/payment recovery state and SSH host identity.
