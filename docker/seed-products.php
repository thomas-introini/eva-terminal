<?php
/**
 * WooCommerce Product Seeder for EVA Terminal
 *
 * This script creates sample coffee products including:
 * - 2 simple products with grind size attribute
 * - 2 variable products with size variations (250g/1kg) and grind size
 *
 * Run via: wp eval-file seed-products.php
 */

if (!defined('ABSPATH')) {
    exit;
}

// Ensure WooCommerce is loaded
if (!class_exists('WC_Product')) {
    echo "Error: WooCommerce not loaded\n";
    exit(1);
}

// Grind size options
$grind_options = ['Whole Beans', 'Espresso', 'Moka Pot', 'Filter', 'French Press', 'Turkish'];

// Size options for variable products
$size_options = ['250g', '1kg'];

/**
 * Create or get a product attribute term
 */
function get_or_create_term($term_name, $taxonomy) {
    $term = get_term_by('name', $term_name, $taxonomy);
    if (!$term) {
        $result = wp_insert_term($term_name, $taxonomy);
        if (is_wp_error($result)) {
            echo "  Warning: Could not create term '$term_name': " . $result->get_error_message() . "\n";
            return null;
        }
        return $result['term_id'];
    }
    return $term->term_id;
}

/**
 * Check if a product already exists by name
 */
function product_exists($name) {
    $existing = get_posts([
        'post_type' => 'product',
        'title' => $name,
        'post_status' => 'publish',
        'numberposts' => 1
    ]);
    return !empty($existing);
}

/**
 * Create a simple product
 */
function create_simple_product($data) {
    global $grind_options;

    if (product_exists($data['name'])) {
        echo "  Skipping (exists): {$data['name']}\n";
        return;
    }

    $product = new WC_Product_Simple();
    $product->set_name($data['name']);
    $product->set_description($data['description']);
    $product->set_short_description($data['short_description']);
    $product->set_regular_price($data['regular_price']);
    if (!empty($data['sale_price'])) {
        $product->set_sale_price($data['sale_price']);
    }
    $product->set_stock_status($data['stock_status']);
    if (isset($data['stock_quantity'])) {
        $product->set_manage_stock(true);
        $product->set_stock_quantity($data['stock_quantity']);
    }
    $product->set_status('publish');
    $product->set_catalog_visibility('visible');

    // Set grind size attribute
    $grind_opts = isset($data['grind_options']) ? $data['grind_options'] : $grind_options;
    $grind_attr = new WC_Product_Attribute();
    $grind_attr->set_id(0); // 0 = custom product attribute
    $grind_attr->set_name('Grind Size');
    $grind_attr->set_position(0);
    $grind_attr->set_visible(true);
    $grind_attr->set_variation(false);
    $grind_attr->set_options($grind_opts);

    $product->set_attributes(array($grind_attr));

    $product_id = $product->save();
    echo "  Created simple product: {$data['name']} (ID: $product_id)\n";

    return $product_id;
}

/**
 * Create a variable product with variations
 */
function create_variable_product($data) {
    global $grind_options;

    if (product_exists($data['name'])) {
        echo "  Skipping (exists): {$data['name']}\n";
        return;
    }

    $product = new WC_Product_Variable();
    $product->set_name($data['name']);
    $product->set_description($data['description']);
    $product->set_short_description($data['short_description']);
    $product->set_status('publish');
    $product->set_catalog_visibility('visible');

    // Build attributes array - must use proper format for WooCommerce
    $attributes = array();

    // Size attribute (for variations) - use lowercase slug
    $size_attr = new WC_Product_Attribute();
    $size_attr->set_id(0); // 0 = custom product attribute (not taxonomy)
    $size_attr->set_name('Size');
    $size_attr->set_position(0);
    $size_attr->set_visible(true);
    $size_attr->set_variation(true);
    $size_attr->set_options($data['size_options']);
    $attributes[] = $size_attr;

    // Grind size attribute (not for variations)
    $grind_opts = isset($data['grind_options']) ? $data['grind_options'] : $grind_options;
    $grind_attr = new WC_Product_Attribute();
    $grind_attr->set_id(0);
    $grind_attr->set_name('Grind Size');
    $grind_attr->set_position(1);
    $grind_attr->set_visible(true);
    $grind_attr->set_variation(false);
    $grind_attr->set_options($grind_opts);
    $attributes[] = $grind_attr;

    $product->set_attributes($attributes);

    $product_id = $product->save();
    echo "  Created variable product: {$data['name']} (ID: $product_id)\n";

    // Create variations
    foreach ($data['variations'] as $var_data) {
        $variation = new WC_Product_Variation();
        $variation->set_parent_id($product_id);
        $variation->set_regular_price($var_data['regular_price']);
        if (!empty($var_data['sale_price'])) {
            $variation->set_sale_price($var_data['sale_price']);
        }
        $variation->set_stock_status($var_data['stock_status']);
        if (isset($var_data['stock_quantity'])) {
            $variation->set_manage_stock(true);
            $variation->set_stock_quantity($var_data['stock_quantity']);
        }
        // Use lowercase attribute name for variation
        $variation->set_attributes(array('size' => $var_data['size']));
        $variation->save();
        echo "    - Variation: {$var_data['size']} @ \${$var_data['regular_price']}\n";
    }

    // Sync the variable product
    WC_Product_Variable::sync($product_id);

    return $product_id;
}

// ============================================
// SEED DATA
// ============================================

echo "\n==> Creating sample coffee products...\n\n";

// Simple Product 1: Ethiopian Yirgacheffe
create_simple_product([
    'name' => 'Ethiopian Yirgacheffe',
    'description' => '<p>A bright and fruity coffee from the <strong>Yirgacheffe</strong> region of Ethiopia. Notes of blueberry, lemon, and floral undertones.</p><p>Perfect for pour-over and filter brewing methods.</p>',
    'short_description' => '<p>Bright and fruity Ethiopian coffee with blueberry notes.</p>',
    'regular_price' => '18.99',
    'sale_price' => '',
    'stock_status' => 'instock',
    'stock_quantity' => 50,
    'grind_options' => ['Whole Beans', 'Espresso', 'Moka Pot', 'Filter', 'French Press', 'Turkish']
]);

// Simple Product 2: Colombian Supremo
create_simple_product([
    'name' => 'Colombian Supremo',
    'description' => '<p>A classic <em>Colombian</em> coffee with a well-balanced profile. Rich chocolate and nutty flavors with a smooth finish.</p>',
    'short_description' => '<p>Classic Colombian with chocolate and nutty notes.</p>',
    'regular_price' => '17.99',
    'sale_price' => '15.99',
    'stock_status' => 'instock',
    'stock_quantity' => 100,
    'grind_options' => ['Whole Beans', 'Espresso', 'Moka Pot', 'Filter', 'French Press']
]);

// Simple Product 3: Decaf Swiss Water (out of stock)
create_simple_product([
    'name' => 'Decaf Swiss Water',
    'description' => '<p>Premium decaffeinated coffee using the <strong>Swiss Water Process</strong>. 99.9% caffeine-free while retaining full flavor.</p>',
    'short_description' => '<p>Chemical-free decaf with full flavor.</p>',
    'regular_price' => '16.99',
    'sale_price' => '',
    'stock_status' => 'outofstock',
    'stock_quantity' => 0,
    'grind_options' => ['Whole Beans', 'Espresso', 'Filter', 'French Press']
]);

// Variable Product 1: House Blend Signature
create_variable_product([
    'name' => 'House Blend Signature',
    'description' => '<p>Our signature <strong>House Blend</strong> combines beans from Brazil, Colombia, and Guatemala.</p><ul><li>Medium roast</li><li>Notes of caramel and cocoa</li><li>Low acidity</li></ul>',
    'short_description' => '<p>Signature blend with caramel and cocoa notes.</p>',
    'size_options' => ['250g', '1kg'],
    'grind_options' => ['Whole Beans', 'Espresso', 'Moka Pot', 'Filter', 'French Press', 'Turkish'],
    'variations' => [
        [
            'size' => '250g',
            'regular_price' => '14.99',
            'sale_price' => '',
            'stock_status' => 'instock',
            'stock_quantity' => 25
        ],
        [
            'size' => '1kg',
            'regular_price' => '54.99',
            'sale_price' => '49.99',
            'stock_status' => 'instock',
            'stock_quantity' => 15
        ]
    ]
]);

// Variable Product 2: Single Origin Sumatra
create_variable_product([
    'name' => 'Single Origin Sumatra',
    'description' => '<p>A bold and earthy coffee from <strong>Sumatra, Indonesia</strong>.</p><p>Tasting notes include dark chocolate, tobacco, and a hint of spice. Full body with low acidity.</p><p>Wet-hulled processing gives this coffee its distinctive character.</p>',
    'short_description' => '<p>Bold Sumatran coffee with earthy, chocolatey notes.</p>',
    'size_options' => ['250g', '1kg'],
    'grind_options' => ['Whole Beans', 'Espresso', 'Moka Pot', 'Filter', 'French Press'],
    'variations' => [
        [
            'size' => '250g',
            'regular_price' => '19.99',
            'sale_price' => '',
            'stock_status' => 'instock',
            'stock_quantity' => 30
        ],
        [
            'size' => '1kg',
            'regular_price' => '69.99',
            'sale_price' => '',
            'stock_status' => 'outofstock',
            'stock_quantity' => 0
        ]
    ]
]);

echo "\n==> Product seeding complete!\n";
echo "==> Total: 3 simple products, 2 variable products with variations\n\n";
