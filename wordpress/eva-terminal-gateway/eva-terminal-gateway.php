<?php
/**
 * Plugin Name: EVA Terminal Stripe Checkout
 * Description: Durable terminal checkout bridge using native WooCommerce carts and Stripe hosted Checkout.
 * Version: 0.1.0
 * Requires PHP: 8.2
 * Requires Plugins: woocommerce
 * WC requires at least: 9.9
 * License: MIT
 */
defined('ABSPATH') || exit;
require_once __DIR__ . '/includes/stripe.php';
require_once __DIR__ . '/includes/analytics.php';
require_once __DIR__ . '/includes/bridge.php';
register_activation_hook(__FILE__, ['Eva_Terminal_Bridge', 'install']);
register_deactivation_hook(__FILE__, static function () { wp_clear_scheduled_hook('eva_terminal_reconcile'); wp_clear_scheduled_hook('eva_terminal_analytics'); });
add_action('before_woocommerce_init', static function () {
    if (class_exists(Automattic\WooCommerce\Utilities\FeaturesUtil::class)) {
        Automattic\WooCommerce\Utilities\FeaturesUtil::declare_compatibility('custom_order_tables', __FILE__, true);
    }
});
add_action('plugins_loaded', static function () {
    if (!class_exists('WC_Payment_Gateway')) { return; }
    require_once __DIR__ . '/includes/gateway.php';
    add_filter('woocommerce_payment_gateways', static function ($gateways) { $gateways[] = Eva_Terminal_Gateway::class; return $gateways; });
    Eva_Terminal_Bridge::boot();
});
add_action('woocommerce_blocks_loaded', static function () {
    if (!class_exists(Automattic\WooCommerce\Blocks\Payments\Integrations\AbstractPaymentMethodType::class)) { return; }
    require_once __DIR__ . '/includes/blocks.php';
    add_action('woocommerce_blocks_payment_method_type_registration', static function ($registry) { $registry->register(new Eva_Terminal_Blocks()); });
    Eva_Terminal_Bridge::extensions();
});
