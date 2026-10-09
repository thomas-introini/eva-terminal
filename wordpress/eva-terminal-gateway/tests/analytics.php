<?php
// Run only in the isolated test store, after integration.py and orders.php.
defined('ABSPATH') || exit;
if (getenv('EVA_TERMINAL_FAKE_UMAMI') !== '1') { throw new RuntimeException('Test transport required'); }
global $wpdb;
function eva_analytics_check(bool $condition, string $message): void { if (!$condition) { throw new RuntimeException($message); } }
function eva_analytics_row(int $id): array { global $wpdb; return $wpdb->get_row($wpdb->prepare('SELECT * FROM ' . Eva_Terminal_Analytics::table() . ' WHERE order_id=%d', $id), ARRAY_A); }
function eva_analytics_due(int $id): void { global $wpdb; $wpdb->update(Eva_Terminal_Analytics::table(), ['available_at' => 0], ['order_id' => $id]); }

Eva_Terminal_Bridge::install(); Eva_Terminal_Bridge::install();
eva_analytics_check((int)get_option('eva_terminal_schema') === 4, 'Schema migration failed');
// Recreate a pre-analytics journal under an isolated table prefix, then migrate twice.
$original_prefix = $wpdb->prefix;
$wpdb->prefix .= 'eva_migration_check_';
$legacy_table = Eva_Terminal_Bridge::table(); $new_outbox = Eva_Terminal_Analytics::table();
try {
    $wpdb->query("CREATE TABLE IF NOT EXISTS $legacy_table (attempt_id varchar(32) NOT NULL, customer_ref varchar(64) NOT NULL, request_hash varchar(64) NOT NULL, created_at bigint NOT NULL, PRIMARY KEY (attempt_id)) ENGINE=InnoDB");
    $wpdb->replace($legacy_table, ['attempt_id' => str_repeat('a', 32), 'customer_ref' => str_repeat('b', 64), 'request_hash' => str_repeat('c', 64), 'created_at' => time()]);
    Eva_Terminal_Bridge::install(); Eva_Terminal_Bridge::install();
    $legacy = $wpdb->get_row("SELECT * FROM $legacy_table WHERE attempt_id='" . str_repeat('a', 32) . "'", ARRAY_A);
    eva_analytics_check(array_key_exists('analytics_context', $legacy) && $legacy['analytics_context'] === null && $legacy['request_hash'] === str_repeat('c', 64), 'Upgrade rewrote legacy attribution/hash');
    eva_analytics_check($wpdb->get_var("SHOW TABLES LIKE '$new_outbox'") === $new_outbox, 'Upgrade did not install outbox');
} finally {
    $wpdb->prefix = $original_prefix;
    $wpdb->query("DROP TABLE IF EXISTS $legacy_table"); $wpdb->query("DROP TABLE IF EXISTS $new_outbox");
}
$cron = _get_cron_array(); $counts = ['eva_terminal_reconcile' => 0, 'eva_terminal_analytics' => 0];
foreach ($cron as $hooks) { foreach ($counts as $hook => $count) { $counts[$hook] += count($hooks[$hook] ?? []); } }
eva_analytics_check($counts === ['eva_terminal_reconcile' => 1, 'eva_terminal_analytics' => 1], 'Repeated activation duplicated/missed cron');

$rows = $wpdb->get_results('SELECT a.* FROM ' . Eva_Terminal_Bridge::table() . ' a JOIN ' . Eva_Terminal_Analytics::table() . " o ON o.order_id=a.order_id WHERE a.state='paid' AND a.analytics_context IS NOT NULL AND o.payload IS NOT NULL", ARRAY_A);
eva_analytics_check(count($rows) >= 3, 'Missing webhook, zero-total or cancellation-winner cases');
foreach ($rows as $row) {
    $order = wc_get_order($row['order_id']);
    $context = json_decode($row['analytics_context'], true);
    eva_analytics_check($order->get_meta('_eva_terminal_analytics') === $context, 'Order CRUD lost frozen attribution');
    $entry = eva_analytics_row((int)$row['order_id']);
    eva_analytics_check(!empty($entry), 'Paid order not enqueued');
    $body = json_decode($entry['payload'], true); $data = $body['payload']['data'];
    eva_analytics_check($data['revenue'] === Eva_Terminal_Bridge::minor($order->get_total()) / 100 && $data['currency'] === $order->get_currency(), 'Revenue not from authoritative Woo total');
    eva_analytics_check($body['payload']['id'] === $context['connection_id'] && $data['checkout_id'] === $context['checkout_id'], 'Purchase attribution changed');
    $before = $entry['payload'];
    Eva_Terminal_Analytics::enqueue($row); Eva_Terminal_Analytics::enqueue($row);
    eva_analytics_check(eva_analytics_row((int)$row['order_id'])['payload'] === $before, 'Duplicate enqueue mutated purchase');
}
eva_analytics_check((int)$wpdb->get_var('SELECT COUNT(*) FROM ' . Eva_Terminal_Analytics::table() . ' o JOIN ' . Eva_Terminal_Bridge::table() . ' a ON o.order_id=a.order_id WHERE a.analytics_context IS NULL') === 0, 'Legacy/malformed context attributed');

// Simulate the crash gap: authoritative completion succeeded, analytics did not enqueue.
$first = $rows[0]; $id = (int)$first['order_id'];
$wpdb->delete(Eva_Terminal_Analytics::table(), ['order_id' => $id]);
Eva_Terminal_Analytics::recover();
eva_analytics_check(!empty(eva_analytics_row($id)), 'Paid scan did not repair crash gap');

// One durable sender, even if two cron invocations try the same row.
eva_analytics_due($id);
$claimed = Eva_Terminal_Analytics::claim($id);
eva_analytics_check($claimed !== null && Eva_Terminal_Analytics::claim($id) === null, 'Concurrent claim duplicated ownership');
update_option('eva_test_umami_response', 'success', false);
Eva_Terminal_Analytics::deliver($claimed);
eva_analytics_check(eva_analytics_row($id)['state'] === 'sent', 'Successful receipt not saved');
$event_id = eva_analytics_row($id)['event_id'];
eva_analytics_check((int)get_option('eva_test_umami_' . $event_id) === 1, 'Purchase sent twice');

// New eligible paid fixtures exercise storage backends and transport outcomes.
function eva_analytics_fixture(string $mode = 'test', string $environment = 'staging', bool $collect = true): array {
    global $wpdb;
    $order = wc_create_order(); $attempt = bin2hex(random_bytes(16));
    $context = ['connection_id' => wp_generate_uuid4(), 'checkout_id' => wp_generate_uuid4(), 'schema_version' => 1, 'environment' => $environment, 'collect' => $collect];
    $product = new WC_Order_Item_Product(); $product->set_name('Test'); $product->set_quantity(2); $product->set_total(0); $order->add_item($product);
    $order->set_currency('EUR'); $order->set_total('0'); $order->set_payment_method(Eva_Terminal_Bridge::GATEWAY);
    $order->update_meta_data('_eva_terminal_attempt', $attempt); $order->update_meta_data('_eva_terminal_analytics', $context); $order->save();
    $row = ['attempt_id' => $attempt, 'customer_ref' => hash('sha256', $attempt), 'active_customer' => null, 'request_hash' => hash('sha256', $attempt), 'created_at' => time(), 'mode' => $mode, 'order_id' => $order->get_id(), 'state' => 'paid', 'analytics_context' => wp_json_encode($context)];
    $wpdb->insert(Eva_Terminal_Bridge::table(), $row); $order->payment_complete(); Eva_Terminal_Analytics::enqueue($row);
    return $row;
}

foreach (['yes', 'no'] as $hpos) {
    // Use Woo's own synchronizer before switching authoritative storage.
    update_option('woocommerce_custom_orders_table_data_sync_enabled', 'yes');
    $sync = wc_get_container()->get(Automattic\WooCommerce\Internal\DataStores\Orders\DataSynchronizer::class);
    for ($batch = 0; $batch < 50; $batch++) {
        $ids = $sync->get_next_batch_to_process(100);
        if (!$ids) { break; }
        $sync->process_batch($ids);
    }
    eva_analytics_check(!$sync->has_orders_pending_sync(), 'Native order sync did not finish');
    update_option('woocommerce_custom_orders_table_enabled', $hpos);
    $fixture = eva_analytics_fixture();
    $order = wc_get_order($fixture['order_id']);
    eva_analytics_check($order->get_meta('_eva_terminal_analytics') === json_decode($fixture['analytics_context'], true), 'HPOS/classic context persistence failed');
    eva_analytics_check($order->get_data_store()->get_current_class_name() === ($hpos === 'yes' ? Automattic\WooCommerce\Internal\DataStores\Orders\OrdersTableDataStore::class : 'WC_Order_Data_Store_CPT'), 'Test did not select intended order storage');
    $entry = eva_analytics_row((int)$fixture['order_id']); $payload = json_decode($entry['payload'], true);
    eva_analytics_check($payload['payload']['data']['revenue'] === 0 && $payload['payload']['data']['item_count'] === 2, 'Zero-total/count wrong');
}

foreach (['timeout', '500', 'malformed', 'dns', 'crash'] as $mode) {
    $fixture = eva_analytics_fixture(); $id = (int)$fixture['order_id'];
    update_option('eva_test_umami_response', $mode, false);
    $claim = Eva_Terminal_Analytics::claim($id);
    if ($mode === 'crash') {
        $wpdb->update(Eva_Terminal_Analytics::table(), ['claimed_at' => time() - 301], ['order_id' => $id]);
        Eva_Terminal_Analytics::cron();
    } else { Eva_Terminal_Analytics::deliver($claim); }
    if ($mode === 'dns') {
        eva_analytics_check(eva_analytics_row($id)['state'] === 'pending', 'Certain DNS failure not retryable');
        eva_analytics_check(Eva_Terminal_Analytics::claim($id) === null, 'Backoff bypassed');
        for ($i = 0; $i < 2; $i++) { eva_analytics_due($id); Eva_Terminal_Analytics::deliver(Eva_Terminal_Analytics::claim($id)); }
        eva_analytics_check(eva_analytics_row($id)['state'] === 'failed' && (int)eva_analytics_row($id)['attempts'] === 3, 'Retries not bounded');
    } else {
        eva_analytics_check(eva_analytics_row($id)['state'] === 'uncertain', 'Ambiguous/crashed delivery replayable');
        eva_analytics_due($id);
        eva_analytics_check(Eva_Terminal_Analytics::claim($id) === null, 'Uncertain row reclaimed');
    }
}

// Cleanup retains a permanent order marker, so recovery cannot recreate the purchase.
$entry = eva_analytics_row((int)$first['order_id']);
$wpdb->update(Eva_Terminal_Analytics::table(), ['finished_at' => time() - 91 * DAY_IN_SECONDS], ['order_id' => $first['order_id']]);
Eva_Terminal_Analytics::cleanup(); Eva_Terminal_Analytics::recover();
$clean = eva_analytics_row((int)$first['order_id']);
eva_analytics_check($clean['payload'] === null && $clean['event_id'] === $entry['event_id'] && $clean['state'] === 'sent', 'Cleanup lost deduplication marker');

putenv('EVA_TERMINAL_UMAMI_ENVIRONMENT=production');
putenv('EVA_TERMINAL_UMAMI_BASE_URL=https://umami.example');
$test_mode = eva_analytics_fixture('test', 'production');
eva_analytics_check(!$wpdb->get_var($wpdb->prepare('SELECT order_id FROM ' . Eva_Terminal_Analytics::table() . ' WHERE order_id=%d', $test_mode['order_id'])), 'Test payment leaked into production website');
putenv('EVA_TERMINAL_UMAMI_ENABLED=false');
Eva_Terminal_Analytics::cron();
putenv('EVA_TERMINAL_UMAMI_ENABLED=true');
putenv('EVA_TERMINAL_UMAMI_BASE_URL=https://name:secret@umami.example');
eva_analytics_check(Eva_Terminal_Analytics::config() === null, 'Invalid analytics configuration accepted');
putenv('EVA_TERMINAL_UMAMI_BASE_URL=http://127.0.0.1:1'); putenv('EVA_TERMINAL_UMAMI_ENVIRONMENT=staging');
update_option('eva_test_umami_response', 'success', false);
eva_analytics_check(Eva_Terminal_Analytics::context(['connection_id' => 'invalid']) === null, 'Malformed context accepted');
echo "Analytics: migration/cron, HPOS/classic, frozen attribution, duplicate/offline/zero-total/cancel-winner purchases, crash recovery, atomic claim, bounded retry, uncertain no-replay and cleanup passed\n";
