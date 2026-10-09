<?php
defined('ABSPATH') || exit;

// Anonymous purchase outbox. Enqueue only writes locally; cron sends outside payment locks.
final class Eva_Terminal_Analytics {
    public static function table(): string { global $wpdb; return $wpdb->prefix . 'eva_terminal_analytics'; }

    private static function setting(string $key, string $default = ''): string {
        $name = 'EVA_TERMINAL_UMAMI_' . $key;
        $value = defined($name) ? constant($name) : getenv($name);
        if (is_bool($value)) { return $value ? 'true' : 'false'; }
        return $value === false ? $default : (string)$value;
    }
    public static function uuid($value): bool { return is_string($value) && (bool)preg_match('/^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$/D', $value); }
    public static function config(): ?array {
        $enabled = self::setting('ENABLED', 'false');
        if ($enabled !== 'true') { if ($enabled !== 'false') { self::diagnostic('configuration_disabled'); } return null; }
        $config = ['base_url' => rtrim(self::setting('BASE_URL'), '/'), 'website' => self::setting('WEBSITE_ID'), 'hostname' => self::setting('HOSTNAME', 'eva-terminal'), 'environment' => self::setting('ENVIRONMENT', 'development')];
        $url = wp_parse_url($config['base_url']);
        $local = is_array($url) && in_array($url['host'] ?? '', ['localhost', '127.0.0.1', '::1', '[::1]'], true);
        if (!is_array($url) || empty($url['host']) || !in_array($url['scheme'] ?? '', ['http', 'https'], true) || isset($url['user']) || isset($url['pass']) || isset($url['query']) || isset($url['fragment']) || (($url['scheme'] ?? '') === 'http' && (!$local || $config['environment'] === 'production')) || !self::uuid($config['website']) || !preg_match('/^[a-zA-Z0-9][a-zA-Z0-9.-]{0,99}$/D', $config['hostname']) || !in_array($config['environment'], ['development', 'staging', 'production'], true)) {
            self::diagnostic('configuration_disabled'); return null;
        }
        return $config;
    }

    public static function context($value): ?array {
        if ($value === null) { return null; }
        $keys = ['connection_id', 'checkout_id', 'schema_version', 'environment', 'collect'];
        if (!is_array($value) || array_diff(array_keys($value), $keys) || !self::uuid($value['connection_id'] ?? null) || !self::uuid($value['checkout_id'] ?? null) || ($value['schema_version'] ?? null) !== 1 || !is_bool($value['collect'] ?? null) || !in_array($value['environment'] ?? '', ['development', 'staging', 'production'], true)) {
            self::diagnostic('context_ignored'); return null;
        }
        return array_intersect_key($value, array_flip($keys));
    }

    public static function install(): void {
        global $wpdb;
        require_once ABSPATH . 'wp-admin/includes/upgrade.php';
        $table = self::table();
        dbDelta("CREATE TABLE $table (
            order_id bigint unsigned NOT NULL,
            event_id varchar(36) NOT NULL,
            payload longtext DEFAULT NULL,
            base_url varchar(2048) NOT NULL,
            website_id varchar(36) NOT NULL,
            environment varchar(16) NOT NULL,
            state varchar(16) NOT NULL DEFAULT 'pending',
            attempts tinyint unsigned NOT NULL DEFAULT 0,
            available_at bigint NOT NULL,
            claimed_at bigint NOT NULL DEFAULT 0,
            created_at bigint NOT NULL,
            finished_at bigint NOT NULL DEFAULT 0,
            PRIMARY KEY (order_id),
            UNIQUE KEY event_id (event_id),
            KEY delivery (state,available_at)
        ) ENGINE=InnoDB {$wpdb->get_charset_collate()};");
        self::schedule();
    }

    public static function schedule(): void {
        if (!wp_next_scheduled('eva_terminal_analytics')) { wp_schedule_event(time() + 60, 'eva_terminal_minute', 'eva_terminal_analytics'); }
    }
    public static function boot(): void {
        self::schedule();
        add_action('eva_terminal_analytics', [self::class, 'cron']);
        // Covers native payment completion before the attempt state is updated.
        add_action('woocommerce_payment_complete', static function ($id) {
            try {
                global $wpdb;
                $order = wc_get_order($id);
                $attempt = $order ? $order->get_meta('_eva_terminal_attempt') : '';
                $row = $attempt ? $wpdb->get_row($wpdb->prepare('SELECT * FROM ' . Eva_Terminal_Bridge::table() . ' WHERE attempt_id=%s', $attempt), ARRAY_A) : null;
                if ($row) { self::enqueue($row); }
            } catch (Throwable $e) { self::diagnostic('enqueue_failed'); }
        }, 100);
    }

    public static function enqueue(array $row): void {
        // All analytics errors are contained: payment/stock/recovery always continue.
        try { self::enqueue_purchase($row); }
        catch (Throwable $e) { self::diagnostic('enqueue_failed'); }
    }
    private static function enqueue_purchase(array $row): void {
        global $wpdb;
        $config = self::config();
        if (!$config || empty($row['analytics_context']) || empty($row['order_id'])) { return; }
        $context = self::context(json_decode($row['analytics_context'], true));
        if (!$context || !$context['collect'] || $context['environment'] !== $config['environment'] || ($row['mode'] === 'test' && $config['environment'] === 'production')) { return; }
        $order = wc_get_order($row['order_id']);
        if (!$order || $order->get_payment_method() !== Eva_Terminal_Bridge::GATEWAY || $order->get_meta('_eva_terminal_attempt') !== $row['attempt_id'] || (!$order->is_paid() && !$order->get_date_paid())) { return; }
        // The gateway supports EUR / two decimals. Do not guess another currency exponent.
        $currency = $order->get_currency();
        if ($currency !== 'EUR') { self::diagnostic('currency_unsupported'); return; }
        $minor = Eva_Terminal_Bridge::minor($order->get_total());
        if ($minor < 0 || $minor > 9007199254740991) { self::diagnostic('amount_invalid'); return; }
        $count = 0;
        foreach ($order->get_items('line_item') as $item) { $count += max(0, (int)$item->get_quantity()); }
        $event_id = wp_generate_uuid4();
        $payload = ['type' => 'event', 'payload' => [
            'website' => $config['website'], 'hostname' => $config['hostname'], 'url' => '/checkout/payment', 'title' => 'Checkout payment', 'referrer' => '', 'id' => $context['connection_id'], 'name' => 'purchase',
            'data' => ['channel' => 'ssh', 'schema_version' => 1, 'environment' => $context['environment'], 'connection_id' => $context['connection_id'], 'event_id' => $event_id, 'checkout_id' => $context['checkout_id'], 'revenue' => $minor / 100, 'currency' => $currency, 'item_count' => $count],
        ]];
        $ok = $wpdb->query($wpdb->prepare('INSERT IGNORE INTO ' . self::table() . ' (order_id,event_id,payload,base_url,website_id,environment,available_at,created_at) VALUES (%d,%s,%s,%s,%s,%s,%d,%d)', $order->get_id(), $event_id, wp_json_encode($payload), $config['base_url'], $config['website'], $config['environment'], time(), time()));
        if ($ok === false) { throw new RuntimeException('Outbox write failed'); }
    }

    public static function recover(): void {
        global $wpdb;
        // Scan eligible attempts, not only pending payments. Missing outbox after paid is repairable.
        // Rotate bounded batches so a damaged/unsupported order cannot starve later orders.
        $cursor = (string)get_option('eva_terminal_analytics_cursor', '');
        $table = Eva_Terminal_Bridge::table();
        $outbox = self::table();
        $rows = $wpdb->get_results($wpdb->prepare("SELECT a.* FROM $table a LEFT JOIN $outbox o ON o.order_id=a.order_id WHERE a.analytics_context IS NOT NULL AND a.order_id>0 AND o.order_id IS NULL AND a.attempt_id>%s ORDER BY a.attempt_id LIMIT 100", $cursor), ARRAY_A);
        foreach ($rows as $row) { self::enqueue($row); $cursor = $row['attempt_id']; }
        update_option('eva_terminal_analytics_cursor', count($rows) < 100 ? '' : $cursor, false);
    }

    // The compare-and-set claim persists BEFORE HTTP; only one cron owns a row.
    public static function claim(int $order_id): ?array {
        global $wpdb;
        $ok = $wpdb->query($wpdb->prepare('UPDATE ' . self::table() . " SET state='sending',attempts=attempts+1,claimed_at=%d WHERE order_id=%d AND state='pending' AND available_at<=%d AND attempts<3", time(), $order_id, time()));
        return $ok === 1 ? $wpdb->get_row($wpdb->prepare('SELECT * FROM ' . self::table() . ' WHERE order_id=%d', $order_id), ARRAY_A) : null;
    }

    public static function deliver(array $row): void {
        global $wpdb;
        // No redirects, forwarded client IP, auth, cookies or shared identity cache.
        try {
            $response = wp_remote_post($row['base_url'] . '/api/send', ['timeout' => 2, 'redirection' => 0, 'cookies' => [], 'headers' => ['Content-Type' => 'application/json', 'User-Agent' => 'EvaTerminal/1.0'], 'body' => $row['payload'], 'limit_response_size' => 8192]);
            $state = 'uncertain';
            if (!is_wp_error($response)) {
                $code = wp_remote_retrieve_response_code($response);
                $receipt = json_decode(wp_remote_retrieve_body($response), true);
                if ($code >= 200 && $code < 300 && self::uuid($receipt['sessionId'] ?? null) && self::uuid($receipt['visitId'] ?? null)) { $state = 'sent'; }
                // No other rejection is assumed safe on an unverified collector version.
            } elseif (self::definitely_not_sent($response)) { $state = (int)$row['attempts'] < 3 ? 'pending' : 'failed'; }
        } catch (Throwable $e) { $state = 'uncertain'; }
        $data = ['state' => $state, 'available_at' => time() + 60 * (2 ** max(0, (int)$row['attempts'] - 1)), 'finished_at' => $state === 'pending' ? 0 : time()];
        if ($wpdb->update(self::table(), $data, ['order_id' => $row['order_id'], 'state' => 'sending', 'claimed_at' => $row['claimed_at']]) === false) { self::diagnostic('receipt_write_failed'); }
        if ($state === 'uncertain' || $state === 'failed') { self::diagnostic('delivery_' . $state); }
    }

    private static function definitely_not_sent($error): bool {
        // cURL 6 (DNS) and 7 (connect) prove no HTTP request reached the collector.
        // Never classify a timeout/reset or a generic WordPress HTTP error as retryable.
        return $error->get_error_code() === 'http_request_failed' && (bool)preg_match('/^cURL error (6|7):/', $error->get_error_message());
    }

    public static function cleanup(): void {
        global $wpdb;
        // Keep order_id/event_id/state permanently as the deduplication tombstone.
        $wpdb->query($wpdb->prepare('UPDATE ' . self::table() . " SET payload=NULL,base_url='' WHERE state IN ('sent','failed','uncertain') AND payload IS NOT NULL AND finished_at>0 AND finished_at<%d LIMIT 100", time() - 90 * DAY_IN_SECONDS));
    }

    public static function cron(): void {
        global $wpdb;
        try {
            // A crashed sender may have inserted remotely. Never put its row back in pending.
            $wpdb->query($wpdb->prepare('UPDATE ' . self::table() . " SET state='uncertain',finished_at=%d WHERE state='sending' AND claimed_at<%d", time(), time() - 300));
            self::cleanup();
            $config = self::config();
            if (!$config) { return; }
            self::recover();
            $rows = $wpdb->get_results($wpdb->prepare('SELECT order_id FROM ' . self::table() . " WHERE state='pending' AND available_at<=%d AND base_url=%s AND website_id=%s AND environment=%s ORDER BY available_at,order_id LIMIT 20", time(), $config['base_url'], $config['website'], $config['environment']), ARRAY_A);
            foreach ($rows as $candidate) {
                $row = self::claim((int)$candidate['order_id']);
                if ($row) { self::deliver($row); }
            }
        } catch (Throwable $e) { self::diagnostic('cron_failed'); }
    }

    private static function diagnostic(string $code): void {
        // Never log payloads, error text, identifiers, payment or customer data.
        try {
            $key = 'eva_terminal_analytics_' . $code;
            if (get_transient($key)) { return; }
            set_transient($key, 1, 60);
            wc_get_logger()->warning($code, ['source' => 'eva-terminal-analytics']);
        } catch (Throwable $ignored) {}
    }
}
