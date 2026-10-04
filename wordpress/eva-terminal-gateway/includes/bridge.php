<?php
defined('ABSPATH') || exit;

final class Eva_Terminal_Bridge {
    public const GATEWAY = 'eva_terminal_stripe_checkout';
    public const API_VERSION = '2026-09-30.endive';
    public static ?array $context = null;
    private static bool $transaction = false;
    private static bool $changing_order = false;
    private static array $refund_context = [];

    public static function table(): string { global $wpdb; return $wpdb->prefix . 'eva_terminal_attempts'; }

    public static function install(): void {
        global $wpdb;
        require_once ABSPATH . 'wp-admin/includes/upgrade.php';
        $table = self::table();
        dbDelta("CREATE TABLE $table (
            attempt_id varchar(32) NOT NULL,
            customer_ref varchar(64) NOT NULL,
            active_customer varchar(64) DEFAULT NULL,
            request_hash varchar(64) NOT NULL,
            order_id bigint unsigned NOT NULL DEFAULT 0,
            state varchar(24) NOT NULL DEFAULT 'resolving',
            stripe_session varchar(255) NOT NULL DEFAULT '',
            stripe_intent varchar(255) NOT NULL DEFAULT '',
            stripe_params longtext DEFAULT NULL,
            mode varchar(8) NOT NULL DEFAULT 'test',
            created_at bigint NOT NULL,
            expires_at bigint NOT NULL DEFAULT 0,
            refund_after bigint NOT NULL DEFAULT 0,
            refund_pending longtext DEFAULT NULL,
            needs_reconcile tinyint NOT NULL DEFAULT 0,
            last_checked bigint NOT NULL DEFAULT 0,
            PRIMARY KEY (attempt_id),
            UNIQUE KEY active_customer (active_customer),
            KEY order_id (order_id),
            KEY state (state)
        ) ENGINE=InnoDB {$wpdb->get_charset_collate()};");
        if (!wp_next_scheduled('eva_terminal_reconcile')) { wp_schedule_event(time() + 60, 'eva_terminal_minute', 'eva_terminal_reconcile'); }
        update_option('eva_terminal_schema', 3, false);
    }

    public static function boot(): void {
        add_filter('cron_schedules', static function ($s) { $s['eva_terminal_minute'] = ['interval' => 60, 'display' => 'Every minute']; return $s; });
        if ((int)get_option('eva_terminal_schema') !== 3) { self::install(); }
        add_action('rest_api_init', [self::class, 'routes']);
        add_action('eva_terminal_reconcile', [self::class, 'cron']);
        add_action('woocommerce_store_api_checkout_update_order_from_request', [self::class, 'journal'], 100, 2);
        add_action('woocommerce_store_api_checkout_order_processed', [self::class, 'processed'], 100);
        add_action('woocommerce_checkout_create_order_line_item', static function ($item, $key, $values) {
            if (!empty($values['eva_terminal_grind'])) { $item->add_meta_data('Grind', $values['eva_terminal_grind'], true); }
        }, 10, 3);
        add_filter('woocommerce_store_api_add_to_cart_data', [self::class, 'grind'], 10, 2);
        add_filter('woocommerce_order_hold_stock_minutes', static function ($minutes, $order) {
            return self::$context || $order->get_payment_method() === self::GATEWAY ? max(36, (int)$minutes) : $minutes;
        }, 100, 2);
        // Core must not cancel an order while a Stripe link can still accept funds.
        add_filter('woocommerce_cancel_unpaid_order', static function ($cancel, $order) { return $order->get_payment_method() === self::GATEWAY ? false : $cancel; }, 100, 2);
        add_action('woocommerce_before_order_object_save', [self::class, 'guard_cancellation']);
        add_action('woocommerce_create_refund', static function ($refund, $args) {
            if (!empty($args['refund_payment'])) { self::$refund_context[$args['order_id']] = $refund; }
        }, 100, 2);
        add_action('woocommerce_refund_created', static function ($id, $args) {
            if (empty($args['refund_payment'])) { return; }
            $order = wc_get_order($args['order_id']);
            $row = $order ? self::row((string)$order->get_meta('_eva_terminal_attempt')) : null;
            if (!$row) { return; }
            self::lock($row['customer_ref']);
            try {
                $row = self::row($row['attempt_id']);
                $pending = json_decode($row['refund_pending'] ?: 'null', true);
                if ($pending && (int)$pending['woo_refund'] === (int)$id && !empty($pending['confirmed'])) { self::update($row['attempt_id'], ['refund_pending' => null]); }
            } finally { self::unlock($row['customer_ref']); }
        }, 100, 2);
    }

    public static function authorized(): bool {
        $key = defined('EVA_TERMINAL_BRIDGE_KEY') ? EVA_TERMINAL_BRIDGE_KEY : getenv('EVA_TERMINAL_BRIDGE_KEY');
        $given = $_SERVER['HTTP_X_EVA_BRIDGE_KEY'] ?? '';
        return is_string($key) && strlen($key) >= 32 && is_string($given) && hash_equals($key, $given);
    }
    public static function permission() { return self::authorized() ? true : new WP_Error('eva_auth', 'Bridge authentication required', ['status' => 403]); }

    public static function routes(): void {
        register_rest_route('eva-terminal/v1', '/checkout', ['methods' => 'POST', 'permission_callback' => [self::class, 'permission'], 'callback' => [self::class, 'checkout']]);
        register_rest_route('eva-terminal/v1', '/attempts/(?P<id>[a-f0-9]{32})', ['methods' => 'GET', 'permission_callback' => [self::class, 'permission'], 'callback' => static function ($r) { return self::attempt_route($r, false); }]);
        register_rest_route('eva-terminal/v1', '/attempts/(?P<id>[a-f0-9]{32})/cancel', ['methods' => 'POST', 'permission_callback' => [self::class, 'permission'], 'callback' => static function ($r) { return self::attempt_route($r, true); }]);
        register_rest_route('eva-terminal/v1', '/stripe/webhook', ['methods' => 'POST', 'permission_callback' => '__return_true', 'callback' => [self::class, 'webhook']]);
    }

    private static function canonical($data) {
        if (is_array($data)) { if (!array_is_list($data)) { ksort($data); } foreach ($data as &$value) { $value = self::canonical($value); } }
        return $data;
    }
    public static function request_hash(array $data): string { return hash('sha256', wp_json_encode(self::canonical($data))); }
    public static function minor($value): int {
        $decimal = wc_format_decimal($value, 2);
        if (!preg_match('/^\d+\.\d{2}$/', $decimal)) { throw new RuntimeException('Invalid EUR amount'); }
        return (int)str_replace('.', '', $decimal);
    }

    public static function quote(): array {
        $cart = WC()->cart;
        $items = [];
        foreach ($cart->get_cart() as $key => $item) { $items[$key] = [$item['product_id'], $item['variation_id'], $item['quantity'], $item['eva_terminal_grind'] ?? '', $item['variation']]; }
        $customer = WC()->customer;
        $data = ['items' => $items, 'totals' => $cart->get_totals(), 'coupons' => $cart->get_applied_coupons(), 'rates' => WC()->session->get('chosen_shipping_methods', []), 'billing' => $customer->get_billing(), 'shipping' => $customer->get_shipping(), 'gateway' => WC()->session->get('chosen_payment_method')];
        return ['fingerprint' => self::request_hash($data), 'total' => (string)self::minor($cart->get_total('edit')), 'currency' => get_woocommerce_currency()];
    }

    public static function extensions(): void {
        woocommerce_store_api_register_endpoint_data([
            'endpoint' => Automattic\WooCommerce\StoreApi\Schemas\V1\ProductSchema::IDENTIFIER, 'namespace' => 'eva_terminal',
            'data_callback' => static function ($product) { return ['grinds' => self::grinds($product)]; },
            'schema_callback' => static function () { return ['grinds' => ['type' => 'array', 'items' => ['type' => 'string'], 'readonly' => true]]; }, 'schema_type' => ARRAY_A,
        ]);
        woocommerce_store_api_register_endpoint_data([
            'endpoint' => Automattic\WooCommerce\StoreApi\Schemas\V1\CartItemSchema::IDENTIFIER, 'namespace' => 'eva_terminal',
            'data_callback' => static function ($item) { return ['grind' => $item['eva_terminal_grind'] ?? '']; },
            'schema_callback' => static function () { return ['grind' => ['type' => 'string', 'readonly' => true]]; }, 'schema_type' => ARRAY_A,
        ]);
        woocommerce_store_api_register_endpoint_data([
            'endpoint' => Automattic\WooCommerce\StoreApi\Schemas\V1\CartSchema::IDENTIFIER, 'namespace' => 'eva_terminal',
            'data_callback' => static function () { return self::authorized() ? ['quote' => self::quote()] : []; },
            'schema_callback' => static function () { return ['quote' => ['type' => 'object', 'readonly' => true, 'properties' => ['fingerprint' => ['type' => 'string'], 'total' => ['type' => 'string'], 'currency' => ['type' => 'string']]]]; }, 'schema_type' => ARRAY_A,
        ]);
        woocommerce_store_api_register_update_callback(['namespace' => 'eva_terminal', 'callback' => static function ($data) {
            if (!self::authorized()) { throw new Automattic\WooCommerce\StoreApi\Exceptions\RouteException('eva_auth', 'Bridge authentication required', 403); }
            WC()->session->set('chosen_payment_method', self::GATEWAY);
            WC()->cart->calculate_totals();
        }]);
    }

    private static function grinds($product): array {
        if ($product->is_type('variation')) { $product = wc_get_product($product->get_parent_id()); }
        foreach ($product->get_attributes() as $attribute) {
            if (strtolower(wc_attribute_label($attribute->get_name())) !== 'grind size') { continue; }
            return $attribute->is_taxonomy() ? wc_get_product_terms($product->get_id(), $attribute->get_name(), ['fields' => 'names']) : array_values($attribute->get_options());
        }
        return [];
    }
    public static function grind($data, $request) {
        $extensions = $request['extensions'] ?? [];
        $grind = $extensions['eva_terminal']['grind'] ?? '';
        if ($grind === '') { return $data; }
        $product = wc_get_product($data['id']);
        if (!is_string($grind) || !$product || !in_array($grind, self::grinds($product), true)) {
            throw new Automattic\WooCommerce\StoreApi\Exceptions\RouteException('eva_grind', 'Select a valid grind size', 400);
        }
        $data['cart_item_data']['eva_terminal_grind'] = $grind;
        return $data;
    }

    private static function native(string $method, string $path, array $data = []) {
        $r = new WP_REST_Request($method, '/wc/store/v1' . $path);
        $r->set_header('Cart-Token', $_SERVER['HTTP_CART_TOKEN'] ?? '');
        $r->set_header('X-EVA-Bridge-Key', $_SERVER['HTTP_X_EVA_BRIDGE_KEY'] ?? '');
        $r->set_header('Content-Type', 'application/json');
        $r->set_body(wp_json_encode($data));
        return rest_do_request($r);
    }
    private static function row(string $id): ?array {
        global $wpdb;
        return $wpdb->get_row($wpdb->prepare('SELECT * FROM ' . self::table() . ' WHERE attempt_id=%s', $id), ARRAY_A) ?: null;
    }
    private static function update(string $id, array $data): void {
        global $wpdb;
        if ($wpdb->update(self::table(), $data, ['attempt_id' => $id]) === false) { throw new RuntimeException('Cannot persist payment attempt'); }
    }
    private static function lock(string $owner): void {
        global $wpdb;
        if ((int)$wpdb->get_var($wpdb->prepare('SELECT GET_LOCK(%s, 10)', 'eva_' . substr($owner, 0, 56))) !== 1) { throw new RuntimeException('Payment attempt is busy; retry'); }
    }
    private static function unlock(string $owner): void { global $wpdb; $wpdb->get_var($wpdb->prepare('SELECT RELEASE_LOCK(%s)', 'eva_' . substr($owner, 0, 56))); }
    private static function commit(): void { global $wpdb; if (self::$transaction) { if ($wpdb->query('COMMIT') === false) { throw new RuntimeException('Cannot commit order journal'); } self::$transaction = false; } }

    public static function checkout($request) {
        global $wpdb;
        $body = $request->get_json_params();
        $id = $body['attempt_id'] ?? ''; $owner = $body['customer_ref'] ?? '';
        if (!is_array($body) || !is_string($id) || !preg_match('/^[a-f0-9]{32}$/', $id) || !is_string($owner) || !preg_match('/^[a-f0-9]{64}$/', $owner) || empty($_SERVER['HTTP_CART_TOKEN'])) {
            return new WP_Error('eva_request', 'Valid attempt, customer reference and Cart-Token required', ['status' => 400]);
        }
        $locked = false;
        try {
            self::lock($owner); $locked = true;
            $row = self::row($id);
            $hash = self::request_hash($body);
            if ($row && ($row['customer_ref'] !== $owner || $row['request_hash'] !== $hash)) { return new WP_Error('eva_attempt_mismatch', 'Attempt ID was used with different checkout data', ['status' => 409]); }
            if ($row && $row['order_id']) { return self::payment(wc_get_order($row['order_id']), $row); }
            if ($row && in_array($row['state'], ['failed', 'cancelled', 'expired'], true)) { return self::response($row); }
            $settings = get_option('woocommerce_' . self::GATEWAY . '_settings', []);
            if (($settings['enabled'] ?? 'no') !== 'yes' || get_woocommerce_currency() !== 'EUR' || wc_get_price_decimals() !== 2 || !class_exists(EvaTerminalVendor\Stripe\StripeClient::class)) { return new WP_Error('eva_disabled', 'New terminal checkouts are disabled or gateway is not configured', ['status' => 503]); }
            $mode = ($settings['testmode'] ?? 'yes') === 'yes' ? 'test' : 'live';
            if (empty($settings[$mode . '_secret_key']) || empty($settings[$mode . '_webhook_secret'])) { return new WP_Error('eva_disabled', 'Stripe API and webhook credentials must be configured before creating orders', ['status' => 503]); }
            $cart = self::native('GET', '/cart');
            if ($cart->is_error()) { return $cart; }
            WC()->session->set('chosen_payment_method', self::GATEWAY);
            WC()->cart->calculate_totals();
            if (self::canonical($body['accepted_quote'] ?? []) !== self::canonical(self::quote())) {
                return new WP_Error('eva_quote_changed', 'Review and confirm the updated WooCommerce quote', ['status' => 409, 'cart' => $cart->get_data(), 'quote' => self::quote()]);
            }
            // Verify transactional storage before creating a native order. HPOS and classic orders are supported.
            foreach ([$wpdb->posts, $wpdb->postmeta, self::table(), $wpdb->prefix . 'woocommerce_order_items', $wpdb->prefix . 'woocommerce_order_itemmeta', $wpdb->prefix . 'wc_orders', $wpdb->prefix . 'wc_orders_meta', $wpdb->prefix . 'wc_order_addresses', $wpdb->prefix . 'wc_order_operational_data'] as $table) {
                $engine = $wpdb->get_var($wpdb->prepare('SELECT ENGINE FROM information_schema.TABLES WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME=%s', $table));
                if ($engine !== null && strtoupper($engine) !== 'INNODB') { throw new RuntimeException('Checkout requires InnoDB order and attempt tables'); }
            }
            if (!$row) {
                $ok = $wpdb->insert(self::table(), ['attempt_id' => $id, 'customer_ref' => $owner, 'active_customer' => $owner, 'request_hash' => $hash, 'created_at' => time(), 'mode' => ($settings['testmode'] ?? 'yes') === 'yes' ? 'test' : 'live']);
                if (!$ok) { return new WP_Error('eva_active_attempt', 'Recover or cancel the existing active payment before placing another order', ['status' => 409]); }
                $row = self::row($id);
            }
            $wpdb->query('START TRANSACTION'); self::$transaction = true;
            self::$context = $row + ['accepted_quote' => $body['accepted_quote']];
            $fields = $body['checkout'] ?? [];
            if (!is_array($fields) || !empty($fields['create_account'])) { throw new RuntimeException('Guest checkout fields required'); }
            $fields['payment_method'] = self::GATEWAY;
            // Native Woo validation, extension hooks, stock reservation and order creation.
            $result = self::native('POST', '/checkout', $fields);
            if (self::$transaction && $result->is_error()) {
                $wpdb->query('ROLLBACK'); self::$transaction = false;
                self::update($id, ['state' => 'failed', 'active_customer' => null, 'order_id' => 0]);
                return $result;
            }
            self::commit();
            $row = self::row($id);
            if (!$row['order_id']) { throw new RuntimeException('Native checkout did not journal an order'); }
            if ($result->is_error()) {
                // A later extension can throw after gateway processing; restore the committed order's reservation.
                if (!in_array($row['state'], ['paid', 'cancelled', 'expired'], true)) { wc_reserve_stock_for_order(wc_get_order($row['order_id'])); }
                return self::response($row); // Order exists; recover, never create another.
            }
            $order = wc_get_order($row['order_id']);
            if ($order->is_paid() || self::minor($order->get_total()) === 0) { self::update($id, ['state' => 'paid', 'active_customer' => null]); }
            return self::response(self::row($id));
        } catch (Throwable $e) {
            if (self::$transaction) { $wpdb->query('ROLLBACK'); self::$transaction = false; self::update($id, ['order_id' => 0]); }
            self::log('checkout_recovery_required', $id);
            return new WP_Error('eva_resolving', 'Checkout outcome requires recovery; retry the same attempt ID', ['status' => 503]);
        } finally { self::$context = null; if ($locked) { self::unlock($owner); } }
    }

    public static function journal($order, $request = null): void {
        if (!self::$context) { return; }
        $order->set_payment_method(self::GATEWAY);
        $order->update_meta_data('_eva_terminal_attempt', self::$context['attempt_id']);
        $order->update_meta_data('_eva_terminal_customer', self::$context['customer_ref']);
        $order->save();
        self::update(self::$context['attempt_id'], ['order_id' => $order->get_id()]);
    }
    public static function processed($order): void {
        if (!self::$context) { return; }
        $quote = self::$context['accepted_quote'];
        if ((string)self::minor($order->get_total()) !== $quote['total'] || $order->get_currency() !== $quote['currency'] || self::quote() !== $quote) {
            throw new RuntimeException('Quote changed during native validation; confirm again');
        }
        self::journal($order);
        self::commit(); // Journal the actual order before any external Stripe request.
    }

    private static function settings(string $mode): array {
        $settings = get_option('woocommerce_' . self::GATEWAY . '_settings', []);
        return ['key' => $settings[$mode . '_secret_key'] ?? '', 'secret' => $settings[$mode . '_webhook_secret'] ?? ''];
    }
    private static function stripe(array $row) {
        $settings = self::settings($row['mode']);
        if (!$settings['key']) { throw new RuntimeException('Stripe API key missing'); }
        $http = EvaTerminalVendor\Stripe\HttpClient\CurlClient::instance();
        $http->setTimeout(10); $http->setConnectTimeout(5);
        return new EvaTerminalVendor\Stripe\StripeClient(['api_key' => $settings['key'], 'stripe_version' => self::API_VERSION, 'max_network_retries' => 2]);
    }

    public static function payment($order, array $row): array {
        if (!$order || $order->get_payment_method() !== self::GATEWAY || $order->get_meta('_eva_terminal_attempt') !== $row['attempt_id']) { throw new RuntimeException('Order ownership mismatch'); }
        $row = self::row($row['attempt_id']);
        if (in_array($row['state'], ['paid', 'cancelled', 'expired', 'failed'], true)) { return self::response($row); }
        if (self::minor($order->get_total()) === 0) { if (!$order->is_paid()) { $order->payment_complete(); } self::update($row['attempt_id'], ['state' => 'paid', 'active_customer' => null]); return self::response(self::row($row['attempt_id'])); }
        if ($row['stripe_session']) { self::reconcile($row); return self::response(self::row($row['attempt_id'])); }
        $stripe = self::stripe($row);
        // Recover after a process died between Stripe creation and saving its ID, including after Stripe's idempotency retention.
        if ($row['stripe_params']) {
            foreach ($stripe->checkout->sessions->all(['created' => ['gte' => (int)$row['created_at'] - 5], 'limit' => 100])->autoPagingIterator() as $candidate) {
                if (($candidate->metadata['eva_attempt'] ?? '') === $row['attempt_id'] && ($candidate->metadata['eva_order'] ?? '') === (string)$order->get_id()) {
                    self::update($row['attempt_id'], ['stripe_session' => $candidate->id, 'expires_at' => $candidate->expires_at]); self::reconcile(self::row($row['attempt_id'])); return self::response(self::row($row['attempt_id']));
                }
            }
        }
        $params = $row['stripe_params'] ? json_decode($row['stripe_params'], true, 512, JSON_THROW_ON_ERROR) : null;
        if (!$params) {
            $metadata = ['eva_attempt' => $row['attempt_id'], 'eva_order' => (string)$order->get_id()];
            $params = ['mode' => 'payment', 'payment_method_types' => ['card'], 'payment_intent_data' => ['capture_method' => 'automatic', 'metadata' => $metadata], 'metadata' => $metadata, 'client_reference_id' => $row['attempt_id'], 'customer_email' => $order->get_billing_email(), 'expires_at' => time() + 1805,
                'line_items' => [['quantity' => 1, 'price_data' => ['currency' => 'eur', 'unit_amount' => self::minor($order->get_total()), 'product_data' => ['name' => 'WooCommerce order #' . $order->get_order_number()]]]],
                'success_url' => $order->get_checkout_order_received_url(), 'cancel_url' => home_url('/')];
            self::update($row['attempt_id'], ['stripe_params' => wp_json_encode($params), 'expires_at' => $params['expires_at']]);
        }
        if ($params['expires_at'] <= time()) {
            // The original expiry has passed; even a delayed creation cannot produce a payable link.
            self::cancel_order($row, 'expired'); return self::response(self::row($row['attempt_id']));
        }
        if ($params['expires_at'] < time() + 1800) {
            // Do not release stock while a timed-out creation may still be executing.
            // Search/reconcile the same request until its session is found or its actual expiry passes.
            throw new RuntimeException('Stripe session creation outcome requires recovery');
        }
        $session = $stripe->checkout->sessions->create($params, ['idempotency_key' => 'eva-checkout-' . $row['attempt_id']]);
        self::update($row['attempt_id'], ['stripe_session' => $session->id, 'state' => 'awaiting_payment', 'expires_at' => $session->expires_at]);
        $order->update_meta_data('_eva_terminal_stripe_session', $session->id); $order->save();
        wc_reserve_stock_for_order($order);
        return self::response(self::row($row['attempt_id']), $session);
    }

    public static function response(array $row, $session = null): array {
        $order = $row['order_id'] ? wc_get_order($row['order_id']) : null;
        $url = $session ? (string)$session->url : '';
        if (!$url && $row['stripe_session'] && $row['state'] === 'awaiting_payment') { $url = (string)self::stripe($row)->checkout->sessions->retrieve($row['stripe_session'])->url; }
        return ['attempt_id' => $row['attempt_id'], 'order_id' => $order ? $order->get_id() : 0, 'order_key' => $order ? $order->get_order_key() : '', 'payment_state' => $row['state'], 'payment_url' => $row['state'] === 'awaiting_payment' ? $url : '', 'expires_at' => (int)$row['expires_at'], 'total' => $order ? (string)self::minor($order->get_total()) : '', 'currency' => $order ? $order->get_currency() : 'EUR'];
    }

    public static function attempt_route($request, bool $cancel) {
        $owner = $request->get_param('customer_ref'); $id = $request['id'];
        $row = self::row($id);
        if (!$row || !is_string($owner) || !hash_equals($row['customer_ref'], $owner)) { return new WP_Error('eva_missing', 'Attempt not found', ['status' => 404]); }
        $locked = false;
        try {
            self::lock($owner); $locked = true; $row = self::row($id);
            if ($row['order_id']) {
                if ($cancel) { self::cancel($row); }
                elseif ($row['stripe_session']) { self::reconcile($row); }
                elseif ($row['state'] === 'resolving') { self::payment(wc_get_order($row['order_id']), $row); }
            } elseif ($cancel) { self::update($id, ['state' => 'cancelled', 'active_customer' => null]); }
            return self::response(self::row($id));
        } catch (Throwable $e) { self::log('attempt_recovery_required', $id); return new WP_Error('eva_resolving', 'Payment outcome is not confirmed; retry', ['status' => 503]); }
        finally { if ($locked) { self::unlock($owner); } }
    }

    private static function verify_session(array $row, $session, $order): void {
        if ($session->id !== $row['stripe_session'] || ($session->metadata['eva_attempt'] ?? '') !== $row['attempt_id'] || ($session->metadata['eva_order'] ?? '') !== (string)$order->get_id() || (int)$session->amount_total !== self::minor($order->get_total()) || strtoupper($session->currency) !== $order->get_currency() || (bool)$session->livemode !== ($row['mode'] === 'live')) {
            throw new RuntimeException('Stripe session ownership or amount mismatch');
        }
    }
    public static function reconcile(array $row): void {
        if (!$row['stripe_session']) { return; }
        $order = wc_get_order($row['order_id']);
        $session = self::stripe($row)->checkout->sessions->retrieve($row['stripe_session']);
        self::verify_session($row, $session, $order);
        if ($session->status === 'complete' && $session->payment_status === 'paid') {
            $intent = self::stripe($row)->paymentIntents->retrieve($session->payment_intent);
            if ($intent->status !== 'succeeded' || (int)$intent->amount_received !== self::minor($order->get_total()) || strtoupper($intent->currency) !== $order->get_currency() || ($intent->metadata['eva_attempt'] ?? '') !== $row['attempt_id']) { throw new RuntimeException('Stripe payment not verified'); }
            self::$changing_order = true;
            try { if (!$order->is_paid() && !$order->get_date_paid()) { $order->payment_complete($intent->id); } }
            finally { self::$changing_order = false; }
            self::update($row['attempt_id'], ['state' => 'paid', 'active_customer' => null, 'stripe_intent' => $intent->id]);
            self::sync_refunds(self::row($row['attempt_id']));
        } elseif ($session->status === 'expired' && $row['state'] !== 'paid') { self::cancel_order($row, 'expired'); }
        elseif ($session->status === 'open' && $row['state'] === 'resolving') { self::update($row['attempt_id'], ['state' => 'awaiting_payment']); }
    }
    private static function cancel(array $row): void {
        if (in_array($row['state'], ['paid', 'cancelled', 'expired', 'failed'], true)) { return; }
        if (!$row['stripe_session']) {
            if ($row['order_id'] && $row['stripe_params']) { self::payment(wc_get_order($row['order_id']), $row); $row = self::row($row['attempt_id']); }
            if (!$row['stripe_session']) { self::cancel_order($row, 'cancelled'); return; }
        }
        self::reconcile($row); $row = self::row($row['attempt_id']);
        if ($row['state'] === 'paid' || $row['state'] === 'expired') { return; }
        $stripe = self::stripe($row);
        $session = $stripe->checkout->sessions->retrieve($row['stripe_session']);
        if ($session->status === 'open') {
            try { $session = $stripe->checkout->sessions->expire($session->id, [], ['idempotency_key' => 'eva-expire-' . $row['attempt_id']]); }
            catch (Throwable $e) { self::reconcile($row); if (self::row($row['attempt_id'])['state'] === 'paid') { return; } throw $e; }
        }
        if ($session->status !== 'expired') { throw new RuntimeException('Stripe session cannot be safely cancelled'); }
        self::cancel_order($row, 'cancelled');
    }
    private static function cancel_order(array $row, string $state): void {
        $order = $row['order_id'] ? wc_get_order($row['order_id']) : null;
        if ($order && ($order->is_paid() || $order->get_date_paid())) { self::update($row['attempt_id'], ['state' => 'paid', 'active_customer' => null]); return; }
        self::$changing_order = true;
        try { if ($order && !$order->has_status('cancelled')) { $order->update_status('cancelled', 'Stripe Checkout ' . $state . '; funds not received.'); wc_release_stock_for_order($order); } }
        finally { self::$changing_order = false; }
        self::update($row['attempt_id'], ['state' => $state, 'active_customer' => null]);
    }
    public static function guard_cancellation($order): void {
        if (self::$changing_order || $order->get_payment_method() !== self::GATEWAY || !isset($order->get_changes()['status']) || $order->get_status() !== 'cancelled') { return; }
        $row = self::row((string)$order->get_meta('_eva_terminal_attempt'));
        if (!$row || in_array($row['state'], ['paid', 'cancelled', 'expired', 'failed'], true)) { return; }
        self::lock($row['customer_ref']);
        try { self::cancel($row); if (self::row($row['attempt_id'])['state'] === 'paid') { throw new RuntimeException('Order was paid; use a refund'); } }
        finally { self::unlock($row['customer_ref']); }
    }

    public static function webhook($request) {
        $event = null;
        foreach (['test', 'live'] as $mode) {
            $secret = self::settings($mode)['secret'];
            if (!$secret) { continue; }
            try { $event = EvaTerminalVendor\Stripe\Webhook::constructEvent($request->get_body(), $request->get_header('Stripe-Signature'), $secret); if ((bool)$event->livemode !== ($mode === 'live')) { $event = null; continue; } break; }
            catch (Throwable $e) { continue; }
        }
        if (!$event) { return new WP_Error('eva_signature', 'Invalid webhook signature', ['status' => 400]); }
        $object = $event->data->object;
        $id = $object->metadata['eva_attempt'] ?? '';
        if (!$id && !empty($object->payment_intent)) {
            global $wpdb;
            $id = $wpdb->get_var($wpdb->prepare('SELECT attempt_id FROM ' . self::table() . ' WHERE stripe_intent=%s', $object->payment_intent));
        }
        $row = is_string($id) ? self::row($id) : null;
        if (!$row) { return ['received' => true]; } // Other gateways' events are not ours.
        $locked = false;
        try {
            self::lock($row['customer_ref']); $locked = true; $row = self::row($id);
            self::update($id, ['needs_reconcile' => 1]);
            if (!$row['stripe_session'] && str_starts_with($event->type, 'checkout.session.') && ($object->metadata['eva_order'] ?? '') === (string)$row['order_id']) { self::update($id, ['stripe_session' => $object->id, 'expires_at' => $object->expires_at]); $row = self::row($id); }
            self::reconcile($row); // Fetch current Stripe state, never trust event arrival order.
            if (str_starts_with($event->type, 'charge.dispute.')) {
                $dispute = self::stripe($row)->disputes->retrieve($object->id);
                if ($dispute->payment_intent !== $row['stripe_intent']) { throw new RuntimeException('Dispute ownership mismatch'); }
                $order = wc_get_order($row['order_id']); $key = '_eva_terminal_dispute_' . $dispute->id . '_' . $dispute->status;
                if (!$order->get_meta($key)) { $order->update_meta_data($key, 1); $order->save(); $order->add_order_note('Stripe dispute ' . $dispute->id . ': ' . $dispute->status); }
            }
            self::update($id, ['needs_reconcile' => 0]);
            return ['received' => true];
        } catch (Throwable $e) { self::log('webhook_retry', $id); return new WP_Error('eva_retry', 'Payment reconciliation unavailable; retry webhook', ['status' => 503]); }
        finally { if ($locked) { self::unlock($row['customer_ref']); } }
    }

    public static function refund($order, $amount, string $reason): void {
        $row = $order ? self::row((string)$order->get_meta('_eva_terminal_attempt')) : null;
        if (!$row || !$row['stripe_intent'] || $amount === null || self::minor($amount) <= 0) { throw new RuntimeException('Paid order and positive refund amount required'); }
        self::lock($row['customer_ref']);
        try {
            $row = self::row($row['attempt_id']);
            $refund = self::$refund_context[$order->get_id()] ?? null;
            if (!$refund || !$refund->get_id()) { throw new RuntimeException('Native Woo refund context required'); }
            $pending = json_decode($row['refund_pending'] ?: 'null', true);
            if ($pending && (int)$pending['woo_refund'] !== $refund->get_id()) {
                // Woo deletes a provisional refund after an unknown gateway result. A retry resumes its saved Stripe request.
                if (wc_get_order($pending['woo_refund']) || $pending['amount'] !== self::minor($amount) || $pending['reason'] !== $reason) { throw new RuntimeException('Resolve the previous refund before requesting another'); }
                $pending['woo_refund'] = $refund->get_id();
            }
            if (!$pending) {
                $key = 'eva-refund-' . $order->get_id() . '-' . $refund->get_id();
                $pending = ['key' => $key, 'woo_refund' => $refund->get_id(), 'amount' => self::minor($amount), 'reason' => $reason,
                    'params' => ['payment_intent' => $row['stripe_intent'], 'amount' => self::minor($amount), 'metadata' => ['eva_attempt' => $row['attempt_id'], 'eva_order' => (string)$order->get_id(), 'eva_refund_key' => $key]]];
            }
            // Persist before Stripe. Delay mirroring while Woo finalizes (or deletes) its provisional refund.
            self::update($row['attempt_id'], ['refund_pending' => wp_json_encode($pending)]);
            self::update($row['attempt_id'], ['refund_after' => time() + 60]);
            $remote = self::recover_refund($row, $pending);
            if ($remote->status !== 'succeeded') { throw new RuntimeException('Refund is pending; scheduled recovery will finish it'); }
            $pending['confirmed'] = true;
            self::update($row['attempt_id'], ['refund_pending' => wp_json_encode($pending)]);
        } finally { self::unlock($row['customer_ref']); }
    }
    private static function recover_refund(array $row, array $pending) {
        $stripe = self::stripe($row);
        foreach ($stripe->refunds->all(['payment_intent' => $row['stripe_intent'], 'limit' => 100])->autoPagingIterator() as $refund) {
            if (($refund->metadata['eva_refund_key'] ?? '') === $pending['key']) {
                if (!in_array($refund->status, ['succeeded', 'pending'], true)) {
                    $order = wc_get_order($row['order_id']); $key = '_eva_terminal_refund_failed_' . $refund->id;
                    if (!$order->get_meta($key)) { $order->update_meta_data($key, 1); $order->save(); $order->add_order_note('Stripe refund ' . $refund->id . ' failed; no refund completed.'); }
                    self::update($row['attempt_id'], ['refund_pending' => null, 'refund_after' => 0]);
                    throw new RuntimeException('Stripe refund was rejected');
                }
                if ((int)$refund->amount !== $pending['amount']) { throw new RuntimeException('Stripe refund amount mismatch'); }
                return $refund;
            }
        }
        $refund = $stripe->refunds->create($pending['params'], ['idempotency_key' => $pending['key']]);
        if (!in_array($refund->status, ['succeeded', 'pending'], true)) { throw new RuntimeException('Stripe refund was rejected'); }
        return $refund;
    }
    private static function sync_refunds(array $row): void {
        if (!$row['stripe_intent'] || (int)$row['refund_after'] > time()) { return; }
        $pending = json_decode($row['refund_pending'] ?: 'null', true);
        if ($pending) {
            $remote = self::recover_refund($row, $pending);
            if ($remote->status !== 'succeeded') { return; }
            $refund = wc_get_order($pending['woo_refund']);
            if ($refund instanceof WC_Order_Refund) { $refund->set_refunded_payment(true); $refund->save(); }
        }
        $refunded = 0;
        foreach (self::stripe($row)->refunds->all(['payment_intent' => $row['stripe_intent'], 'limit' => 100])->autoPagingIterator() as $refund) { if ($refund->status === 'succeeded') { $refunded += $refund->amount; } }
        $order = wc_get_order($row['order_id']);
        $difference = $refunded - self::minor($order->get_total_refunded());
        if ($difference > 0) {
            $result = wc_create_refund(['order_id' => $order->get_id(), 'amount' => number_format($difference / 100, 2, '.', ''), 'reason' => 'Stripe refund synchronized', 'refund_payment' => false, 'restock_items' => false]);
            if (is_wp_error($result)) { throw new RuntimeException('Cannot synchronize Woo refund'); }
        }
        self::update($row['attempt_id'], ['refund_pending' => null, 'refund_after' => 0]);
    }

    public static function cron(): void {
        global $wpdb;
        // Settled payments are webhook-driven. Scan only pending work; rotate batches to avoid starving older attempts.
        $rows = $wpdb->get_results('SELECT * FROM ' . self::table() . " WHERE state IN ('resolving','awaiting_payment') OR needs_reconcile=1 OR refund_pending IS NOT NULL OR refund_after>0 ORDER BY last_checked ASC LIMIT 100", ARRAY_A);
        foreach ($rows as $row) {
            $locked = false;
            try {
                self::lock($row['customer_ref']); $locked = true; $row = self::row($row['attempt_id']);
                self::update($row['attempt_id'], ['last_checked' => time()]);
                if ($row['stripe_session']) { self::reconcile($row); }
                elseif ($row['order_id']) { self::payment(wc_get_order($row['order_id']), $row); }
                elseif ((int)$row['created_at'] < time() - 1800) { self::update($row['attempt_id'], ['state' => 'expired', 'active_customer' => null]); }
                self::update($row['attempt_id'], ['needs_reconcile' => 0]);
            } catch (Throwable $e) {
                self::log('scheduled_recovery_required', $row['attempt_id']);
                if ($row['order_id'] && $row['state'] !== 'paid') { try { wc_reserve_stock_for_order(wc_get_order($row['order_id'])); } catch (Throwable $ignored) {} }
            } finally { if ($locked) { self::unlock($row['customer_ref']); } }
        }
    }
    private static function log(string $event, string $id): void { wc_get_logger()->warning($event . ' attempt=' . $id, ['source' => 'eva-terminal']); }
}
