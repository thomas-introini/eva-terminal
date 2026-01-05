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
wp option update woocommerce_default_country "US:CA"
wp option update woocommerce_store_postcode "90210"
wp option update woocommerce_currency "USD"
wp option update woocommerce_calc_taxes "no"

# Enable REST API
wp option update woocommerce_api_enabled "yes"

# Create REST API keys if they don't exist
echo "==> Setting up REST API keys..."
EXISTING_KEY=$(wp db query "SELECT consumer_key FROM wp_woocommerce_api_keys WHERE description='EVA Terminal' LIMIT 1" --skip-column-names 2>/dev/null || echo "")

if [ -z "$EXISTING_KEY" ]; then
    echo "==> Creating new API keys..."

    # Generate keys using WooCommerce REST API key generation
    CONSUMER_KEY="ck_$(openssl rand -hex 20)"
    CONSUMER_SECRET="cs_$(openssl rand -hex 20)"

    # Hash the keys for storage (WooCommerce uses truncated sha256)
    KEY_HASH=$(echo -n "$CONSUMER_KEY" | openssl dgst -sha256 | awk '{print $2}' | cut -c1-64)
    SECRET_HASH=$(echo -n "$CONSUMER_SECRET" | openssl dgst -sha256 | awk '{print $2}' | cut -c1-64)

    # Insert into database
    wp db query "INSERT INTO wp_woocommerce_api_keys (user_id, description, permissions, consumer_key, consumer_secret, truncated_key) VALUES (1, 'EVA Terminal', 'read', '$KEY_HASH', '$SECRET_HASH', '$(echo $CONSUMER_KEY | tail -c 8)')"

    echo ""
    echo "============================================"
    echo "WooCommerce API Keys Generated!"
    echo "============================================"
    echo "Consumer Key:    $CONSUMER_KEY"
    echo "Consumer Secret: $CONSUMER_SECRET"
    echo ""
    echo "Add these to your environment:"
    echo "  export WOO_BASE_URL=http://localhost:8080"
    echo "  export WOO_CONSUMER_KEY=$CONSUMER_KEY"
    echo "  export WOO_CONSUMER_SECRET=$CONSUMER_SECRET"
    echo "============================================"
    echo ""
else
    echo "==> API keys already exist"
fi

# Create product attributes
echo "==> Creating product attributes..."

# Check if Grind Size attribute exists
GRIND_ATTR_ID=$(wp db query "SELECT attribute_id FROM wp_woocommerce_attribute_taxonomies WHERE attribute_name='grind-size' LIMIT 1" --skip-column-names 2>/dev/null || echo "")

if [ -z "$GRIND_ATTR_ID" ]; then
    wp db query "INSERT INTO wp_woocommerce_attribute_taxonomies (attribute_name, attribute_label, attribute_type, attribute_orderby) VALUES ('grind-size', 'Grind Size', 'select', 'menu_order')"
    echo "  Created: Grind Size attribute"
fi

# Check if Size attribute exists
SIZE_ATTR_ID=$(wp db query "SELECT attribute_id FROM wp_woocommerce_attribute_taxonomies WHERE attribute_name='size' LIMIT 1" --skip-column-names 2>/dev/null || echo "")

if [ -z "$SIZE_ATTR_ID" ]; then
    wp db query "INSERT INTO wp_woocommerce_attribute_taxonomies (attribute_name, attribute_label, attribute_type, attribute_orderby) VALUES ('size', 'Size', 'select', 'menu_order')"
    echo "  Created: Size attribute"
fi

# Flush rewrite rules to register attributes
wp rewrite flush

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
