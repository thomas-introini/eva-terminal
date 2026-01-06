<?php
/**
 * Plugin Name: Allow REST API Read Access
 * Description: Allows unauthenticated read access to WooCommerce REST API (DEV ONLY)
 * Version: 1.0
 *
 * WARNING: This is for LOCAL DEVELOPMENT ONLY. Do not use in production!
 */

// Allow unauthenticated access to WooCommerce REST API for GET requests
add_filter('woocommerce_rest_check_permissions', function($permission, $context, $object_id, $post_type) {
    // Only allow read operations without auth
    if ($context === 'read') {
        return true;
    }
    return $permission;
}, 10, 4);

// Also handle the product variations endpoint
add_filter('woocommerce_rest_check_permissions', function($permission) {
    if ($_SERVER['REQUEST_METHOD'] === 'GET') {
        return true;
    }
    return $permission;
}, 99, 1);
