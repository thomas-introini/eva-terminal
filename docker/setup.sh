#!/bin/sh
# WooCommerce setup script for Docker
# This script runs via wp-cli container after WordPress is ready

set -e

echo "==> Waiting for WordPress to be fully ready..."
sleep 10

# Check if WordPress is already installed
if ! wp core is-installed 2>/dev/null; then
    echo "==> Installing WordPress..."
    wp core install \
        --url="http://localhost:${WOO_HTTP_PORT:-8080}" \
        --title="Coffee Shop" \
        --admin_user="admin" \
        --admin_password="admin" \
        --admin_email="admin@localhost.local" \
        --skip-email
else
    echo "==> WordPress already installed"
fi

# CRITICAL: Enable pretty permalinks for REST API to work
echo "==> Configuring permalinks..."
wp option update permalink_structure '/%postname%/'
# Create .htaccess content via database - WordPress will use it
wp rewrite flush 2>/dev/null || true

# Install and activate WooCommerce
if ! wp plugin is-installed woocommerce 2>/dev/null; then
    echo "==> Installing WooCommerce..."
    wp plugin install woocommerce --activate
else
    echo "==> WooCommerce already installed"
    wp plugin activate woocommerce 2>/dev/null || true
fi

# Configure WooCommerce
echo "==> Configuring WooCommerce..."
wp option update woocommerce_store_address "123 Coffee Street"
wp option update woocommerce_store_city "Bean Town"
wp option update woocommerce_default_country "IT:RM"
wp option update woocommerce_store_postcode "90210"
wp option update woocommerce_currency "EUR"
wp option update woocommerce_calc_taxes "no"

# Native Store API authentication remains enabled. No REST bypass plugin.
wp plugin activate eva-terminal-gateway
wp option update woocommerce_hold_stock_minutes 35
wp option update woocommerce_eva_terminal_stripe_checkout_settings '{"enabled":"yes","testmode":"yes"}' --format=json

# Create product attributes using WooCommerce PHP API
echo "==> Creating product attributes..."
wp eval '
global $wpdb;

$attributes = array(
    array("name" => "grind-size", "label" => "Grind Size"),
    array("name" => "size", "label" => "Size"),
);

foreach ($attributes as $attr) {
    $exists = $wpdb->get_var($wpdb->prepare(
        "SELECT attribute_id FROM {$wpdb->prefix}woocommerce_attribute_taxonomies WHERE attribute_name = %s",
        $attr["name"]
    ));

    if (!$exists) {
        $wpdb->insert(
            $wpdb->prefix . "woocommerce_attribute_taxonomies",
            array(
                "attribute_name" => $attr["name"],
                "attribute_label" => $attr["label"],
                "attribute_type" => "select",
                "attribute_orderby" => "menu_order",
            )
        );
        echo "  Created: " . $attr["label"] . " attribute\n";
    }
}
'

# Flush rewrite rules to register attributes
wp rewrite flush 2>/dev/null || true

# Run the PHP seed script
echo "==> Seeding sample coffee products..."
wp eval-file /var/www/html/seed-products.php
wp eval-file /var/www/html/checkout-settings.php

echo ""
echo "==> Setup complete!"
echo "==> WordPress admin: http://localhost:${WOO_HTTP_PORT:-8080}/wp-admin (admin/admin)"
echo "==> WooCommerce Store API: http://localhost:${WOO_HTTP_PORT:-8080}/wp-json/wc/store/v1/"
echo ""

# Keep container alive briefly for logs
sleep 5
