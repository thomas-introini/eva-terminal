<?php
/**
 * Plugin Name: Allow REST API Access
 * Description: Allows unauthenticated access to WooCommerce REST API and Store API (DEV ONLY)
 * Version: 2.0
 *
 * WARNING: This is for LOCAL DEVELOPMENT ONLY. Do not use in production!
 */

// Allow unauthenticated access to WooCommerce REST API (v3)
add_filter('woocommerce_rest_check_permissions', function($permission, $context, $object_id, $post_type) {
    // Allow read operations without auth
    if ($context === 'read') {
        return true;
    }
    // Allow create operations for orders (checkout)
    if ($context === 'create' && $post_type === 'shop_order') {
        return true;
    }
    return $permission;
}, 10, 4);

// Fallback filter for REST API endpoints based on HTTP method
add_filter('woocommerce_rest_check_permissions', function($permission) {
    $method = $_SERVER['REQUEST_METHOD'];

    // Allow GET (read products, variations, payment gateways)
    if ($method === 'GET') {
        return true;
    }

    // Allow POST to orders endpoint (checkout)
    if ($method === 'POST') {
        $request_uri = $_SERVER['REQUEST_URI'];
        if (strpos($request_uri, '/wc/v3/orders') !== false) {
            return true;
        }
    }

    return $permission;
}, 99, 1);

// ============================================
// WooCommerce Store API Access (wc/store/v1)
// ============================================

// Allow all Store API requests without authentication
add_filter('woocommerce_store_api_disable_nonce_check', '__return_true');

// Bypass Store API rate limiting in development
add_filter('woocommerce_store_api_rate_limit_options', function($options) {
    $options['enabled'] = false;
    return $options;
});

// Allow unauthenticated cart operations
add_action('rest_api_init', function() {
    // Remove authentication requirement for Store API
    add_filter('rest_authentication_errors', function($result) {
        $request_uri = $_SERVER['REQUEST_URI'];

        // Allow all Store API endpoints without auth
        if (strpos($request_uri, '/wc/store/') !== false) {
            return true;
        }

        return $result;
    }, 99);
});

// Ensure Store API cart endpoints work without session
add_action('wp_loaded', function() {
    // Initialize cart session for Store API
    if (!empty($_SERVER['REQUEST_URI']) && strpos($_SERVER['REQUEST_URI'], '/wc/store/') !== false) {
        if (!WC()->session) {
            WC()->initialize_session();
        }
        if (!WC()->cart) {
            WC()->initialize_cart();
        }
    }
});

// Allow checkout without requiring payment for COD
add_filter('woocommerce_order_needs_payment', function($needs_payment, $order) {
    if ($order->get_payment_method() === 'cod') {
        return false;
    }
    return $needs_payment;
}, 10, 2);

// Skip authentication for WooCommerce REST API endpoints in dev
add_filter('woocommerce_rest_is_request_to_rest_api', function($is_request) {
    return $is_request;
});
