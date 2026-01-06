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
        --url="http://localhost:8080" \
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

# Install and activate WooCommerce (version 9.3.3 is compatible with WP 6.7)
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
wp option update woocommerce_default_country "US:CA"
wp option update woocommerce_store_postcode "90210"
wp option update woocommerce_currency "USD"
wp option update woocommerce_calc_taxes "no"

# Enable REST API
wp option update woocommerce_api_enabled "yes"

# Create REST API keys using WooCommerce's PHP API (avoids MySQL client issues)
echo "==> Setting up REST API keys..."
wp eval '
global $wpdb;

// Check if key exists
$existing = $wpdb->get_var("SELECT consumer_key FROM {$wpdb->prefix}woocommerce_api_keys WHERE description = \"EVA Terminal\" LIMIT 1");

if (!$existing) {
    // Generate keys
    $consumer_key = "ck_" . wc_rand_hash();
    $consumer_secret = "cs_" . wc_rand_hash();

    $wpdb->insert(
        $wpdb->prefix . "woocommerce_api_keys",
        array(
            "user_id" => 1,
            "description" => "EVA Terminal",
            "permissions" => "read",
            "consumer_key" => wc_api_hash($consumer_key),
            "consumer_secret" => $consumer_secret,
            "truncated_key" => substr($consumer_key, -7),
        )
    );

    echo "\n============================================\n";
    echo "WooCommerce API Keys Generated!\n";
    echo "============================================\n";
    echo "Consumer Key:    " . $consumer_key . "\n";
    echo "Consumer Secret: " . $consumer_secret . "\n";
    echo "\nAdd these to your environment:\n";
    echo "  export WOO_BASE_URL=http://localhost:8080\n";
    echo "  export WOO_CONSUMER_KEY=" . $consumer_key . "\n";
    echo "  export WOO_CONSUMER_SECRET=" . $consumer_secret . "\n";
    echo "============================================\n";
} else {
    echo "API keys already exist\n";
}
'

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

echo ""
echo "==> Setup complete!"
echo "==> WordPress admin: http://localhost:8080/wp-admin (admin/admin)"
echo "==> WooCommerce REST API: http://localhost:8080/wp-json/wc/v3/"
echo ""

# Keep container alive briefly for logs
sleep 5
