# Terminal analytics

EVA Terminal can send anonymous SSH connection navigation to a dedicated Umami website. The WooCommerce gateway sends confirmed purchases even after SSH disconnects. Collection is **off by default** and never changes storefront, payment or recovery availability. WooCommerce website tracking is unchanged.

## Collector contract and rollout prerequisite

The destination instance and its installed version have **not been verified**. The adapter implements this explicit contract:

- One JSON `POST /api/send` per record, `User-Agent: EvaTerminal/1.0`, no administrative token.
- `type=event`; pageviews omit `payload.name`, actions include `payload.name` and flat `payload.data`.
- `payload.id` is a random connection UUID. Every request repeats it. Neither adapter uses `X-Umami-Cache`, cookies or a shared identity cache.
- A successful receipt requires HTTP 2xx and JSON containing UUID `sessionId` and `visitId`. Bot-filter responses such as `{"beep":"boop"}` are not successful receipts.
- No batch endpoint, backdated timestamps, remote idempotency, browser identity or automatic revenue-report behavior is assumed.

This follows [Umami collection documentation](https://docs.umami.is/docs/api/sending-stats), [distinct IDs](https://v2.umami.is/docs/distinct-ids) and the [v2.20.0 collector implementation](https://github.com/umami-software/umami/blob/v2.20.0/src/app/api/send/route.ts); it does not certify compatibility with the deployment. In that reference implementation, `payload.id` derives a distinct native session, while a request without a cache derives its visit from the current hour. Thus a connection can span multiple native visits; an offline purchase can share the explicit connection ID while falling outside the original visit/funnel window. A version with different identity, bot or response behavior must be checked before enabling collection. Geography and device fields inferred by Umami describe the sending server, not the SSH customer; exclude them from customer reports.

## Configuration

Create **EVA Terminal staging** and **EVA Terminal** as separate websites in the existing Umami instance, separate from evacaffe.it. Do not add JavaScript or change WooCommerce browser tracking.

Go environment (`.env.example`):

```dotenv
UMAMI_ENABLED=false
UMAMI_BASE_URL=https://umami.example.com
UMAMI_WEBSITE_ID=<dedicated-website-uuid>
UMAMI_HOSTNAME=eva-terminal
UMAMI_ENVIRONMENT=staging
```

Gateway constants in `wp-config.php`, or server environment with the same names:

```php
define('EVA_TERMINAL_UMAMI_ENABLED', false);
define('EVA_TERMINAL_UMAMI_BASE_URL', 'https://umami.example.com');
define('EVA_TERMINAL_UMAMI_WEBSITE_ID', '<same-dedicated-website-uuid>');
define('EVA_TERMINAL_UMAMI_HOSTNAME', 'eva-terminal');
define('EVA_TERMINAL_UMAMI_ENVIRONMENT', 'staging');
```

Environment enable flags use the literal `true`; constants also accept boolean `true`. Allowed environments are `development`, `staging`, `production`. Website IDs are lowercase UUIDs. URLs must be HTTP(S) without userinfo, query or fragment. HTTP is allowed only on loopback for development/staging mocks; production always requires HTTPS. All redirects are refused. Hostname is an analytics convention, not a DNS dependency. Invalid configuration disables collection with a short diagnostic and leaves shopping available. Development has no collector destination by default.

Use matching website, host and environment in Go and WordPress. Production never emits purchases for Stripe test-mode attempts. Test transactions belong to the staging website. Changing the destination does not rewrite pending outbox payloads: cron sends only rows whose saved base URL, website and environment match current configuration.

Gateway schema **4** adds an optional `analytics_context` to the attempt journal and a separate InnoDB `eva_terminal_analytics` table. New installs and upgrades use idempotent `dbDelta`; existing attempts are unchanged. Activation/boot ensure a single minute cron job for analytics and payment reconciliation; deactivation removes both jobs. Run the real system cron already required by [checkout operations](terminal-checkout.md), for example `wp cron event run --due-now` every minute. A visitor-triggered cron alone is insufficient for offline payment delivery.

## Identity, attribution and privacy

Each authenticated SSH connection starting the TUI gets a cryptographically random UUID. Connections using the same SSH key still share the existing durable cart but have different analytics identities. Reconnecting generates a new UUID: these metrics count **connections**, not unique people or returning users.

At the first valid payment submission, Go creates a separate analytics-only checkout UUID and freezes the connection ID, schema version, environment and collection flag in the durable bridge body before its first HTTP request. The gateway accepts only those five context fields; it never accepts a destination URL or website ID from a checkout. Attribution is saved in the attempt table and through Woo order CRUD, compatible with HPOS and classic storage. Retry/restart/reconnect replay the exact frozen request and hash. Existing requests omit `analytics` entirely; malformed/absent context is ignored without blocking payment or attributing a legacy order to a new connection.

The event adapters send only the allowlisted scalar properties below plus `channel=ssh`, `schema_version=1`, `environment`, `connection_id`. No SSH key, fingerprint, username, customer reference, Cart-Token, email, address, coupon text, search text, payment URL, order key, Stripe ID, raw error or terminal dimensions is sent. IP forwarding is absent. Search text is retained only in TUI memory for consecutive-query deduplication. The durable attribution remains private alongside the existing shopper/order journal.

## Pageviews and actions

Only the final visible commercial screen after a Bubble Tea message is observed. Splash, help, undersized terminals and an uninitialized window do not count. When usable dimensions return, the screen then shown is observed. Rendering, scroll, resize, blink and polling do not generate repeated visits. A reconnect to pending payment can begin directly at payment.

| Screen | Path | Title |
| --- | --- | --- |
| Shop | `/shop` | Shop |
| Cart | `/cart` | Cart |
| Address | `/checkout/address` | Checkout address |
| Delivery | `/checkout/shipping` | Checkout shipping |
| Review | `/checkout/review` | Checkout review |
| Payment | `/checkout/payment` | Checkout payment |

| Event | Meaning / additional properties |
| --- | --- |
| `session_started` | First visible commercial screen; `resumed_payment` |
| `view_item` | Product details visible with selection stable for 700 ms; `product_id`; once per product per connection |
| `search` | Nonempty search confirmed or stable for 700 ms while input visible; `query_length`, `results_count` (local results) |
| `filter_changed` | Voluntary stock filter change; `in_stock_only` |
| `add_to_cart` | Accepted durable local intent; IDs, `quantity`, optional normalized `grind`, `confirmation=local_intent` |
| `remove_from_cart` | Accepted local removal/decrement; IDs, removed `quantity`, `confirmation=local_intent` |
| `cart_quantity_changed` | Accepted local increment; IDs, `quantity_before`, `quantity_after`, `confirmation=local_intent` |
| `begin_checkout` | Valid voluntary cart-to-checkout entry; `item_count` (total quantity); reentries count |
| `coupon_applied`, `coupon_removed` | Changed operation confirmed by Woo; no coupon code |
| `shipping_selected` | Requested rate confirmed selected by Woo; `shipping_method` |
| `checkout_submitted` | First persisted request for an attempt; analytics-only `checkout_id` |
| `payment_link_available` | First link known for the durable attempt; `checkout_id` |
| `payment_link_copy_requested` | Explicit OSC 52 request; does not prove successful copy or browser opening |
| `checkout_error` | Failed commercial operation; allowlisted `stage`, `error_code`; periodic payment polling is excluded |
| `purchase` | Gateway-authoritative paid order, including zero-total; `event_id`, `checkout_id`, `revenue`, `currency`, `item_count` |

New local intents preserve parent `product_id` and optional `variation_id`. Legacy/restored Store API rows with only an untyped sellable ID use **`catalog_item_id`** instead of inventing a parent. Grind values normalize to `whole_beans`, `espresso`, `fine`, `medium`, `coarse`, `filter`, `moka`, `french_press`; unknown catalog options are omitted. Delivery normalizes to `flat_rate`, `free_shipping`, `local_pickup`, `other`. Error stages are `cart_sync`, `cart`, `address`, `coupon`, `shipping`, `checkout`, `payment`; codes are `validation`, `conflict`, `unavailable`, `network`, `unknown`.

Cart actions measure **local intent**, not Woo acceptance. Asynchronous sync can coalesce actions, clamp or reject quantities; its classified error is separate. Unchanged/clamped local quantities do not emit actions. Shared-cart broadcasts, restoration, catalog refresh and Woo reconciliation do not emit local actions. If mutations coalesce across connections, sync failure attribution follows the last local actor of that revision; it does not prove which individual item caused rejection.

`checkout_submitted` and `payment_link_available` markers are persisted on the attempt before enqueue; retries cannot duplicate them. A crash between the marker write and local enqueue can lose these best-effort events. Neither marker participates in the bridge request hash. Purchase revenue uses the final Woo EUR total, including fees, taxes and delivery, converted from validated minor units (the existing gateway supports EUR with two decimals). A zero-total paid order sends numeric zero. Refund analytics are outside scope.

## Delivery and recovery

Go uses one process worker and a FIFO queue of 512 records. Enqueue never waits for HTTP; overflow drops the record. Requests time out after two seconds. Accepted records outlive SSH disconnects; shutdown stops accepting and drains for at most three seconds. Navigation has no automatic retries or restart replay. Atomic counters report `accepted`, `sent`, `dropped`, `failed`, `uncertain`; logs contain only counters at most once a minute and on shutdown.

Purchases use a separate persistent outbox with a UNIQUE order primary key and stable random event ID. Enqueue writes only locally, including all paid branches and the native completion hook. Minute cron rotates through at most 100 eligible attempts missing an outbox row, including paid orders, to repair a crash after payment completion. Legacy orders without an explicit collection context are never backfilled. It sends at most 20 rows per run, outside payment locks and checkout transactions.

| State | Policy |
| --- | --- |
| `pending` | Waiting for an atomic compare-and-set claim |
| `sending` | Persisted before HTTP; one sender owns the row |
| `sent` | Valid successful receipt |
| `uncertain` | Timeout, reset, 5xx, redirect, invalid receipt, unverified rejection or a sender abandoned for five minutes; no automatic resend |
| `failed` | Three proven-before-send failures exhausted |

Only cURL DNS/connect errors 6 and 7 prove a request was not sent and permit retries, with 60/120-second backoff, maximum three attempts. Other WordPress HTTP errors are conservative `uncertain`. No collector rejection is assumed to guarantee noninsertion without installed-version verification. A crash during `sending` becomes `uncertain`, including the case where HTTP had not yet started. Destination outages and outbox errors never change order/payment state.

This is **internal deduplication with possible undercounting**, not remote exactly-once delivery. `event_id` alone does not make Umami deduplicate. Woo is the source of order and revenue totals. Manual replay is not implemented.

Cron removes completed outbox payloads and destination URLs after 90 days in batches of 100, retaining order ID, stable event ID and terminal state permanently as a deduplication tombstone. Do not delete tombstones or clear eligibility metadata independently: the recovery scan could recreate an old purchase. Protect the outbox using the same database/backup access rules as the payment journal. Pending payloads remain until delivery resolution. State counts can be inspected with `SELECT state, COUNT(*) FROM <prefix>eva_terminal_analytics GROUP BY state`; no customer/payload dump is needed for diagnostics.

## Manual staging verification and reports

1. Record the installed Umami version. Configure Go and gateway for the staging website and enable collection explicitly. Check that the real collector accepts `EvaTerminal/1.0` without invented browser fields and returns the required receipt.
2. Connect two SSH terminals from the same server/IP, including two using the same key. Verify distinct native identities in Umami and shared cart behavior locally. Confirm repeated events with one `payload.id` link as expected; cross an hour boundary and document the actual native visit behavior.
3. Browse, pause on products, search, change stock filtering, add/increment/remove items, apply/remove coupons and complete address/delivery/review. Verify the pageview/action classification, privacy allowlist and counts in the real Umami reports. Redraws/help/polling must not inflate visits.
4. Create a Stripe test link, disconnect SSH, pay, and verify one gateway purchase. Exercise duplicate/reordered webhook delivery and cron recovery. Verify zero-total and payment-winning-cancellation cases. Compare revenue to Woo, and distinguish `uncertain` undercounts from unpaid orders.
5. If the installed version supports mixed page/event funnels, use `/shop` → `view_item` → `add_to_cart` → `begin_checkout` → `purchase`. Otherwise use the all-event equivalent `session_started` → `view_item` → `add_to_cart` → `begin_checkout` → `purchase`, with `resumed_payment=false` and a first-screen `/shop` segment. Native reports are provisional until identity linkage and funnel window behavior are verified. For offline payments outside a native visit, join the explicit connection/checkout IDs in exported event data instead of promising a continued browser visit or Stripe session merge.
6. Name reports **SSH connections reaching each checkout step**, **Products viewed/added by SSH connection**, **Checkout attempts/purchases**, **Last recorded screen per SSH connection**, and **Resumed payment connections**. Use distinct connections reaching a step, not raw click ratios. Delivery is optional. For abandonment, select connections with no subsequent event/purchase after a declared window (for example 24 hours); this is an inference and best-effort delivery can bias it. Disconnect is not an abandonment event.
7. After reviewing real staging receipts, identities, classifications and reports, configure the dedicated production website explicitly in both applications. No deployment, live secrets or external resource creation is part of this change.

## Automated validation

`make test`, `go test -race ./...`, `go vet ./...`, `make gateway-check`, `make gateway-integration`. Go tests use local `httptest` collectors and injected timer messages. The isolated Docker integration stack uses simulated Stripe and a test-only WordPress HTTP transport for Umami, with WooCommerce 10.4.3 pinned for its WordPress 6.9 image; the overrides are never deployment configuration. It covers native payment lifecycle, offline/outbox recovery, malformed/legacy attribution, duplicate enqueue, zero totals, HPOS/classic CRUD, migration/cron, atomic claims, certain retries, uncertain/crashed senders and retention tombstones. Simulated receipts and signed test webhooks do **not** verify real Umami reporting or production Stripe webhooks.
