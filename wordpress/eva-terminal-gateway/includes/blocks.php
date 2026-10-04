<?php
defined('ABSPATH') || exit;
class Eva_Terminal_Blocks extends Automattic\WooCommerce\Blocks\Payments\Integrations\AbstractPaymentMethodType {
    protected $name = 'eva_terminal_stripe_checkout';
    public function initialize() { $this->settings = get_option('woocommerce_' . $this->name . '_settings', []); }
    public function is_active() { return Eva_Terminal_Bridge::authorized() && ($this->settings['enabled'] ?? 'no') === 'yes'; }
    public function get_payment_method_script_handles() { return []; }
    public function get_payment_method_data() { return ['title' => 'Stripe hosted Checkout', 'supports' => ['products']]; }
}
