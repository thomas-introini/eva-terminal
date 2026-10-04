<?php
/** Test-only Stripe transport. Mounted exclusively by compose.test.yml. Native Woo authentication remains enabled. */
defined('ABSPATH') || exit;
if (getenv('EVA_TERMINAL_FAKE_STRIPE') !== '1') { return; }
add_filter('pre_wp_mail', static function () { update_option('eva_test_email_count', (int)get_option('eva_test_email_count') + 1, false); return true; });
add_action('woocommerce_payment_complete', static function ($order_id) { $key = 'eva_test_payment_hook_' . $order_id; update_option($key, (int)get_option($key) + 1, false); });
add_action('woocommerce_cart_calculate_fees', static function ($cart) {
    if (WC()->session && WC()->session->get('chosen_payment_method') === Eva_Terminal_Bridge::GATEWAY && $cart->get_subtotal() > 0) { $cart->add_fee('Test terminal gateway fee', '1.23', false); }
});
add_action('plugins_loaded', static function () {
    if (!class_exists(EvaTerminalVendor\Stripe\ApiRequestor::class)) { return; }
    class Eva_Test_Stripe_Client implements EvaTerminalVendor\Stripe\HttpClient\ClientInterface {
        public function request($method, $url, $headers, $params, $hasFile, $apiMode = 'v1', $maxNetworkRetries = null) {
            $path = parse_url($url, PHP_URL_PATH);
            parse_str(parse_url($url, PHP_URL_QUERY) ?? '', $query);
            $params = array_merge($query, $params);
            $key = '';
            foreach ($headers as $header) { if (stripos($header, 'Idempotency-Key:') === 0) { $key = trim(substr($header, 16)); } }
            if ($path === '/v1/checkout/sessions' && $method === 'post') {
                $id = 'cs_test_' . substr(hash('sha256', $key), 0, 24);
                $session = get_option('eva_test_' . $id);
                if (!$session) {
                    $session = ['object' => 'checkout.session', 'id' => $id, 'status' => 'open', 'payment_status' => 'unpaid', 'livemode' => false, 'currency' => 'eur', 'amount_total' => $params['line_items'][0]['price_data']['unit_amount'], 'metadata' => $params['metadata'], 'expires_at' => $params['expires_at'], 'payment_intent' => 'pi_test_' . substr($id, 8), 'url' => 'https://checkout.stripe.com/test/' . $id];
                    update_option('eva_test_' . $id, $session, false);
                    update_option('eva_test_' . $session['payment_intent'], ['object' => 'payment_intent', 'id' => $session['payment_intent'], 'status' => 'requires_payment_method', 'amount_received' => 0, 'currency' => 'eur', 'metadata' => $params['metadata']], false);
                }
                if (!empty($_SERVER['HTTP_X_EVA_TEST_LOSE_SESSION'])) { throw new RuntimeException('Test interrupted after creating Stripe session'); }
                return [wp_json_encode($session), 200, []];
            }
            if ($path === '/v1/checkout/sessions' && $method === 'get') {
                global $wpdb;
                $names = $wpdb->get_col("SELECT option_name FROM {$wpdb->options} WHERE option_name LIKE 'eva_test_cs_test_%'");
                return [wp_json_encode(['object' => 'list', 'data' => array_map('get_option', $names), 'has_more' => false, 'url' => '/v1/checkout/sessions']), 200, []];
            }
            if (preg_match('#^/v1/checkout/sessions/(cs_test_[a-f0-9]+)/expire$#', $path, $match)) {
                $session = get_option('eva_test_' . $match[1]);
                if ($session['status'] !== 'open') { return [wp_json_encode(['error' => ['type' => 'invalid_request_error', 'message' => 'Session is not open']]), 400, []]; }
                $session['status'] = 'expired'; update_option('eva_test_' . $session['id'], $session, false);
                return [wp_json_encode($session), 200, []];
            }
            if (preg_match('#^/v1/(checkout/sessions|payment_intents)/([^/]+)$#', $path, $match)) { return [wp_json_encode(get_option('eva_test_' . $match[2])), 200, []]; }
            if ($path === '/v1/refunds' && $method === 'post') {
                $id = 're_test_' . substr(hash('sha256', $key), 0, 24);
                $refund = get_option('eva_test_' . $id) ?: ['object' => 'refund', 'id' => $id, 'status' => 'succeeded', 'payment_intent' => $params['payment_intent'], 'amount' => $params['amount'], 'metadata' => $params['metadata']];
                update_option('eva_test_' . $id, $refund, false); if (!empty($_SERVER['HTTP_X_EVA_TEST_LOSE_REFUND'])) { throw new RuntimeException('Test interrupted after Stripe refund'); } return [wp_json_encode($refund), 200, []];
            }
            if ($path === '/v1/refunds' && $method === 'get') {
                global $wpdb; $names = $wpdb->get_col("SELECT option_name FROM {$wpdb->options} WHERE option_name LIKE 'eva_test_re_test_%'");
                $refunds = array_values(array_filter(array_map('get_option', $names), static function ($refund) use ($params) { return $refund['payment_intent'] === $params['payment_intent']; }));
                return [wp_json_encode(['object' => 'list', 'data' => $refunds, 'has_more' => false, 'url' => '/v1/refunds']), 200, []];
            }
            throw new RuntimeException('Unexpected test Stripe request ' . $method . ' ' . $path);
        }
    }
    EvaTerminalVendor\Stripe\ApiRequestor::setHttpClient(new Eva_Test_Stripe_Client());
    add_filter('option_woocommerce_eva_terminal_stripe_checkout_settings', static function ($settings) {
        return array_merge($settings ?: [], ['test_secret_key' => 'sk_test_fake', 'test_webhook_secret' => 'whsec_eva_local_test']);
    });
}, 100);
add_action('rest_api_init', static function () {
    register_rest_route('eva-test/v1', '/order/(?P<id>\d+)', ['methods' => 'GET', 'permission_callback' => ['Eva_Terminal_Bridge', 'permission'], 'callback' => static function ($request) {
        global $wpdb;
        $order = wc_get_order($request['id']);
        if (!$order) { return new WP_Error('missing', 'Missing test order', ['status' => 404]); }
        $expires = $wpdb->get_var($wpdb->prepare("SELECT MAX(expires) FROM {$wpdb->prefix}wc_reserved_stock WHERE order_id=%d", $order->get_id()));
        return ['status' => $order->get_status(), 'total' => (string)Eva_Terminal_Bridge::minor($order->get_total()), 'hold_until' => $expires ? strtotime($expires . ' UTC') : 0];
    }]);
    register_rest_route('eva-test/v1', '/session/(?P<id>cs_test_[a-f0-9]+)', ['methods' => ['GET', 'POST'], 'permission_callback' => ['Eva_Terminal_Bridge', 'permission'], 'callback' => static function ($request) {
        $session = get_option('eva_test_' . $request['id']);
        if (!$session) { return new WP_Error('missing', 'Missing fake session', ['status' => 404]); }
        $state = $request->get_param('state');
        if ($state === 'paid') {
            $session['status'] = 'complete'; $session['payment_status'] = 'paid';
            $intent = get_option('eva_test_' . $session['payment_intent']); $intent['status'] = 'succeeded'; $intent['amount_received'] = $session['amount_total']; update_option('eva_test_' . $session['payment_intent'], $intent, false);
        } elseif ($state === 'expired') { $session['status'] = 'expired'; }
        if ($state) { update_option('eva_test_' . $session['id'], $session, false); }
        return $session;
    }]);
});
