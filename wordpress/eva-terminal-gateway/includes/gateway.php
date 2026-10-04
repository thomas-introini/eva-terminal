<?php
defined('ABSPATH') || exit;
class Eva_Terminal_Gateway extends WC_Payment_Gateway {
    public function __construct() {
        $this->id = 'eva_terminal_stripe_checkout';
        $this->method_title = 'EVA Terminal Stripe Checkout';
        $this->method_description = 'Available only to authenticated terminal bridge requests. Cards and eligible wallets; EUR; automatic capture.';
        $this->title = 'Card / Apple Pay / Google Pay';
        $this->has_fields = false;
        $this->supports = ['products', 'refunds'];
        $this->form_fields = [
            'enabled' => ['title' => 'Enable new terminal checkouts', 'type' => 'checkbox', 'default' => 'no'],
            'testmode' => ['title' => 'Stripe test mode', 'type' => 'checkbox', 'default' => 'yes'],
            'test_secret_key' => ['title' => 'Test secret API key', 'type' => 'password'],
            'test_webhook_secret' => ['title' => 'Test webhook signing secret', 'type' => 'password'],
            'live_secret_key' => ['title' => 'Live secret API key', 'type' => 'password'],
            'live_webhook_secret' => ['title' => 'Live webhook signing secret', 'type' => 'password'],
        ];
        $this->init_settings();
        $this->enabled = $this->get_option('enabled', 'no');
        add_action('woocommerce_update_options_payment_gateways_' . $this->id, [$this, 'process_admin_options']);
    }
    public function is_available() {
        return Eva_Terminal_Bridge::authorized() && parent::is_available() && get_woocommerce_currency() === 'EUR' && wc_get_price_decimals() === 2 && class_exists(EvaTerminalVendor\Stripe\StripeClient::class);
    }
    public function process_payment($order_id) {
        if (!Eva_Terminal_Bridge::$context || !Eva_Terminal_Bridge::authorized()) { throw new RuntimeException('Authenticated checkout bridge required'); }
        try {
            $attempt = Eva_Terminal_Bridge::payment(wc_get_order($order_id), Eva_Terminal_Bridge::$context);
            WC()->cart->empty_cart(); // The placed order owns its items; Go retains intentions for unpaid recovery.
            return ['result' => 'success', 'redirect' => $attempt['payment_url']];
        } catch (Throwable $e) {
            // The journal is committed and Stripe may have created a payable session.
            // Reporting a native payment failure would make Woo release its stock reservation.
            wc_get_logger()->warning('payment_session_recovery_required attempt=' . Eva_Terminal_Bridge::$context['attempt_id'], ['source' => 'eva-terminal']);
            WC()->cart->empty_cart();
            return ['result' => 'success', 'redirect' => ''];
        }
    }
    public function process_refund($order_id, $amount = null, $reason = '') {
        try { Eva_Terminal_Bridge::refund(wc_get_order($order_id), $amount, $reason); return true; }
        catch (Throwable $e) { return new WP_Error('eva_refund_failed', 'Stripe refund could not be confirmed. Retry the same refund after checking Stripe.'); }
    }
}
