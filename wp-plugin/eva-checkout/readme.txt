=== EVA Checkout API ===
Contributors: thomas
Tags: woocommerce, checkout, api, headless, rest
Requires at least: 6.0
Tested up to: 6.4
Requires PHP: 7.4
Stable tag: 1.0.0
License: GPLv2 or later
License URI: https://www.gnu.org/licenses/gpl-2.0.html

Stateless quote-based checkout REST API for headless WooCommerce integrations.

== Description ==

EVA Checkout provides a stateless, quote-based checkout API designed for server-to-server integrations with WooCommerce. It enables headless applications (like SSH terminal apps) to:

* Request quotes with calculated shipping rates, taxes, and discounts
* Validate coupon codes against WooCommerce rules
* Create orders from confirmed quotes with idempotency support

**Key Features:**

* Stateless design - no sessions or cookies required
* Full shipping zone/method support
* Complete coupon validation with all WooCommerce rules
* HPOS (High-Performance Order Storage) compatible
* Stock validation and reservation
* Idempotent order creation for safe retries

== Installation ==

1. Upload the `eva-checkout` folder to `/wp-content/plugins/`
2. Activate the plugin through the 'Plugins' menu in WordPress
3. Ensure WooCommerce REST API keys are configured

== API Endpoints ==

= POST /wp-json/eva/v1/quote =
Create a quote with shipping rates and totals.

= GET /wp-json/eva/v1/quote/{id} =
Retrieve an existing quote.

= POST /wp-json/eva/v1/coupon/validate =
Validate a coupon code.

= POST /wp-json/eva/v1/order =
Create an order from a confirmed quote.

== Changelog ==

= 1.0.0 =
* Initial release

== Upgrade Notice ==

= 1.0.0 =
Initial release of EVA Checkout API.
