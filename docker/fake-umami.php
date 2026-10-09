<?php
/** Test-only Umami transport. Mounted exclusively by compose.test.yml. */
defined('ABSPATH') || exit;
if (getenv('EVA_TERMINAL_FAKE_UMAMI') !== '1') { return; }
add_filter('pre_http_request', static function ($pre, $args, $url) {
    if ($url !== 'http://127.0.0.1:1/api/send') { return $pre; }
    global $wpdb;
    $body = json_decode($args['body'], true);
    $data = $body['payload']['data'] ?? [];
    $allowed = ['channel', 'schema_version', 'environment', 'connection_id', 'event_id', 'checkout_id', 'revenue', 'currency', 'item_count'];
    if (($body['type'] ?? '') !== 'event' || ($body['payload']['name'] ?? '') !== 'purchase' || array_diff(array_keys($data), $allowed) || count($data) !== count($allowed) || ($args['headers']['User-Agent'] ?? '') !== 'EvaTerminal/1.0' || ($args['redirection'] ?? -1) !== 0 || ($args['timeout'] ?? 0) !== 2) { throw new RuntimeException('Purchase transport/privacy contract violated'); }
    $outbox = $wpdb->get_row($wpdb->prepare('SELECT * FROM ' . Eva_Terminal_Analytics::table() . ' WHERE event_id=%s', $data['event_id']), ARRAY_A);
    if (!$outbox || $outbox['state'] !== 'sending') { throw new RuntimeException('HTTP before durable claim'); }
    $owner = $wpdb->get_var($wpdb->prepare('SELECT customer_ref FROM ' . Eva_Terminal_Bridge::table() . ' WHERE order_id=%d', $outbox['order_id']));
    if ($owner && $wpdb->get_var($wpdb->prepare('SELECT IS_USED_LOCK(%s)', 'eva_' . substr($owner, 0, 56))) !== null) { throw new RuntimeException('Analytics HTTP while payment locked'); }
    $key = 'eva_test_umami_' . $data['event_id'];
    update_option($key, (int)get_option($key) + 1, false);
    $mode = get_option('eva_test_umami_response', 'success');
    if ($mode === 'timeout') { return new WP_Error('http_request_failed', 'cURL error 28: response timeout'); }
    if ($mode === 'dns') { return new WP_Error('http_request_failed', 'cURL error 6: Could not resolve host'); }
    $code = $mode === '500' ? 500 : 200;
    $receipt = $mode === 'malformed' ? ['beep' => 'boop'] : ['sessionId' => $body['payload']['id'], 'visitId' => wp_generate_uuid4(), 'cache' => 'never-reuse'];
    return ['headers' => [], 'body' => wp_json_encode($receipt), 'response' => ['code' => $code, 'message' => 'Test'], 'cookies' => []];
}, 10, 3);
