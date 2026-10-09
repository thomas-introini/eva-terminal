# Fast terminal storefront: operation and rollout

Follow the [deployment checklist](deployment-checklist.md) to prepare a host, deploy to private staging, validate real payments and promote to a production canary.

## Runtime

One Go process owns a shared, anonymous catalog and one controller per verified SSH key fingerprint. Simultaneous connections using the same key share a cart. Catalog navigation and search use a complete local snapshot; background refresh defaults to 60 seconds. Manual and background catalog refreshes share a cooldown after each completed attempt, including failures; `CATALOG_REFRESH_COOLDOWN_SECONDS` defaults to 30 and accepts integers from 1 to 86400. Quantity writes coalesce for 250 ms and run sequentially. WooCommerce computes prices, coupons, shipping, fees, taxes, stock limits and final order totals.

`STATE_DIR` defaults to `./var/eva-terminal`. Versioned catalog and shopper JSON files are written with atomic rename and fsync, directories `0700`, files `0600`. Shopper files contain addresses, Cart-Tokens, order keys and payment links. Back up this directory securely together with the WordPress database. Run one Go server against a state directory; multiple Go processes are unsupported. Keep the Woo base URL and Store API prefix stable because they form the store identity. Losing a key creates a different shopper; SSH usernames do not identify website accounts.

A failed catalog refresh retains the previous snapshot and displays its age. An expired Cart-Token creates a new guest cart and revalidates saved items and address information. Woo can reject an item or change a quote; confirmation is required again. Unknown cart responses are reconciled with GET before any retry. Unknown checkout responses retain their attempt ID and exact request. Do not delete recovery files to retry a payment.

Optional [terminal analytics](terminal-analytics.md) default to off. When enabled, anonymous connection attribution is frozen in that exact checkout request before the first bridge call. Gateway schema 4 adds a separate purchase outbox and `eva_terminal_analytics` minute job; the same real cron runner sends purchases after disconnects and repairs missing enqueue records. Analytics failures do not change payment recovery, and Woo remains the source of order and revenue totals.

## Terminal shopping flow

Use `Enter` for the primary action, `Esc` to go back, `c` for the cart outside active forms, and `?` for current-screen help. `Ctrl+C` quits everywhere; `q` quits outside text entry and forms. Search is local and updates on typing or paste, with its query, stock filter, and result count kept visible. `s` returns to the shop outside forms/text entry, preserving the catalog selection, search, and each product’s draft choices. Browse with `↑`/`↓`, use `Tab`/`Shift+Tab` to focus size, grind, quantity, or Add, and change values with `←`/`→`. `+`/`-` adjust draft quantity, minimum one. Size changes update the unit price immediately. `Enter`/`a` adds the displayed choices and quantity and opens the cart; single-choice options are read-only.

Checkout progresses through **Address → Delivery → Review → Payment**. Required address fields carry `*`, and Italy (`IT`) is the explicit default. Multiple delivery rates are offered after address entry; changing delivery from review returns to review and updates the quote. The review screen confirms the address and store-calculated total. Checkout is disabled while changes are unresolved or an operation is busy. Cart totals show when they are updating, and confirmation of a changed quote is required again.

Payment instructions remain open until explicit navigation. `y` requests clipboard copying through OSC 52, with the full URL available for manual copying; `r` rechecks status. `x` opens a cancellation confirmation, `Enter` submits it, and `Esc` keeps the payment. The backend's authoritative result handles payment completing during cancellation. A pending payment locks cart edits and can be reopened with `o` or by reconnecting with the same verified key.

Every connection shows the centered ASCII EVA splash for one second while the storefront loads in parallel. Every screen uses a horizontally and vertically centered panel, capped at 100×28, with an EVA / shop / cart header. At 24 rows and taller, a smaller EVA logo with a steady orange dot occupies the headroom above the panel; at 40 rows and taller, it doubles to a more detailed 32×6 version. The cart segment shows the confirmed currency, total, and item quantity, and marks totals as updating during synchronization. At 80 columns and wider, the product list sits beside details and inline options; from 60×18 it stacks above them. Product descriptions scroll with `PgUp`/`PgDown`; `Home`/`End` select the first/last coffee. Cart lines, review, and payment instructions scroll using `PgUp`/`PgDown` and `Home`/`End`; totals and primary controls remain visible. Below 60×18, resize guidance preserves the active shopping state. Forms are resized with the terminal, and the coffee theme adapts to light/dark backgrounds, detected color support, and `NO_COLOR`.

## Install the gateway

Build with PHP 8.2+, Composer, ZIP and the required Stripe PHP extensions (`curl`, `json`, `mbstring`):

```sh
make gateway-build
```

Install `dist/eva-terminal-gateway-<version>.zip` in WordPress. The filename uses the version declared in the plugin header, for example `eva-terminal-gateway-0.1.0.zip`. Runtime requires WooCommerce 9.9+ and PHP 8.2+. Native integration checks used WooCommerce 11.1.2, classic order storage and HPOS, also with official WooCommerce Stripe 11.0.0 enabled. A real SSH smoke check covered configuration, quantity updates, address/review, hosted-link display, disconnect/reconnect and payment confirmation. The build locks Stripe PHP **22.0.0**, prefixes it as `EvaTerminalVendor`, and pins API **2026-09-30.endive**. The generated SDK is included in the ZIP; source checkout users must build it before activation. No Composer tool or unprefixed SDK is loaded at runtime.

Set a long random `EVA_TERMINAL_BRIDGE_KEY` constant in `wp-config.php` (or the server environment) and the identical `EVA_BRIDGE_KEY` on the Go server. Use HTTPS outside localhost. The bridge key belongs only on the backend. Go holds no Stripe secret key. Woo REST consumer keys are unnecessary for this storefront.

In WooCommerce → Settings → Payments → EVA Terminal Stripe Checkout configure test/live API keys and webhook secrets. Enable new terminal checkouts only after validation. Create the Stripe webhook endpoint:

```text
https://STORE/wp-json/eva-terminal/v1/stripe/webhook
```

Select API version `2026-09-30.endive`. Subscribe to `checkout.session.completed`, `checkout.session.expired`, `payment_intent.succeeded`, `payment_intent.payment_failed`, `charge.refunded`, `refund.created`, `refund.updated`, `refund.failed`, `charge.dispute.created`, `charge.dispute.updated`, `charge.dispute.closed`. Recovery fetches current Stripe state, verifies the owned session and payment amount/currency, and tolerates duplicate or reordered events. Browser returns never mark an order paid.

Checkout is guest-only, **EUR with two decimal places**, cards with eligible Apple Pay/Google Pay wallets, automatic capture. Stripe charges one aggregate line for the Woo total. Products and their grind metadata remain on Woo order lines. Stripe automatic tax, promo codes, shipping calculation, adaptive currency conversion and delayed payment methods are excluded. Zero-total Woo orders bypass Stripe.

The gateway is available only on authenticated terminal requests. Existing website gateways remain configured independently. Required checkout fields or product extensions specific to your store must be exercised in staging; native Woo validation rejects unsupported configurations before payment.

## Recovery and stock

Use a real cron runner at least once per minute (`wp cron event run --due-now`); visitor traffic alone is insufficient for reliable expiry/refund recovery. Action `eva_terminal_reconcile` recovers interrupted attempts, reconciles pending payments and mirrors Stripe refunds. Hosted links last approximately 30 minutes. Terminal orders reserve stock for at least 36 minutes, preserving longer store holds, and renew reservations after session creation or unresolved scheduled recovery. Monitor cron failures: automatic stock hold expiration cannot provide an indefinite reservation during a prolonged outage.

The custom attempt table enforces one active attempt per customer and unique attempt/request pairs. MySQL/MariaDB named locks serialize checkout, webhooks, cancellation and refunds. Order and journal creation requires InnoDB. The native Woo order is committed before Stripe creation; the stable Stripe request key and session search recover an interrupted external call, including after Stripe idempotency retention.

Cancellation expires/reconciles Stripe first, then cancels an unpaid Woo order. Payment winning that race stays paid. Woo's automatic unpaid-order cancellation is suppressed for this gateway. Manual unpaid cancellation uses the same reconciliation. Paid orders use Woo refunds. Full/partial refund requests are journaled separately before Stripe; retry recovers the same request even if Woo deleted its provisional record. Refund events reconcile against Stripe's current totals. External Stripe refunds mirror into Woo without automatically restocking items. Disputes add owned order notes and preserve payment history.

Terminal polling uses 2, 4, 8, then 10 seconds, stops on disconnect/final state, and resumes on reconnect. Browsing remains available while a payment is pending; cart edits require successful cancellation. The controller retains saved desired items until payment is confirmed, and restores/revalidates them after cancellation or expiry.

Log source `eva-terminal` records event names and opaque attempt IDs. Keep HTTP proxy access logs from recording query strings on attempt routes (they contain customer references). Do not log Cart-Tokens, bridge keys, Stripe secrets, order keys, payment URLs or address payloads. Recover an attempt through the terminal/bridge before considering any manual order changes.

## Local verification

```sh
make dev                       # mock store + SSH; test payment completes after 15s
make test
go test -race ./...
go vet ./...
go test ./cmd/mockwoo -run TestMockSSHShoppingWalkthrough -v
make gateway-check
make gateway-integration       # isolated Docker store on port 18081 + fake Stripe transport
```

The mock supports independent tokens, realistic product prices, immutable orders, coupon `COFFEE10`, shipping selection and resumable payments. In a local regression, 29 quantity keys coalesced into 2 API calls in about 260 ms, including the 250 ms debounce. `MOCK_API_DELAY_MS` delays API responses; `MOCK_PAYMENT_SECONDS=0` leaves links pending for cancellation testing. Mock URLs are simulated links, not payable Stripe sessions.

The integration target builds the isolated SDK and runs native Woo carts, checkout, stock and order hooks with a deterministic SDK HTTP transport. The test transport is mounted **only** by `docker/compose.test.yml`, captures test emails locally, and requires `EVA_TERMINAL_FAKE_STRIPE=1`. Never deploy that override or MU plugin. Normal Docker uses native Store API token/nonce validation, with no permissive REST bypass. The integration stack has its own volumes and project name. Stop it with the matching Compose files and project name; backups are not deleted automatically.

## Release checklist

- [x] Isolated, durable shopper carts; same-key broadcasts; atomic private persistence.
- [x] Complete paginated catalog/variation snapshots, local search, stale snapshot retention, force refresh.
- [x] Debounced cart intentions, incremental Woo writes, quantity limits, ambiguous-response reconciliation.
- [x] Native addresses/coupons/shipping/fees/taxes and server-validated grind metadata.
- [x] Accepted quote checks, durable checkout journal, isolated pinned SDK, payment link/reconnect flow.
- [x] Signed webhook recovery, zero totals, safe cancellation/expiry, partial/full refund recovery.
- [x] Go regression/race checks; native Woo integration with classic storage and HPOS.
- [ ] Reproduce the deployed store's plugins, shipping/tax zones, required fields, stock settings and currency in its staging environment.
- [ ] Use actual Stripe **test mode** to check card success/decline, eligible wallets, 3DS, webhook delivery while SSH is offline, refunds/disputes, real email delivery and failures. Local SDK simulation cannot prove these provider/browser behaviors.
- [ ] Measure latency/API counts on that staging store with realistic catalog size and delayed Woo responses.
- [ ] Start an SSH allowlist canary, monitor pending attempts and reservations, then enable wider access.

Set Go `CHECKOUT_ENABLED=false` and/or disable **new** gateway checkouts to stop new orders. Keep the plugin, webhook endpoint, cron runner and Stripe credentials active until all existing attempts are settled. Do not disable payment recovery as part of rollback.
