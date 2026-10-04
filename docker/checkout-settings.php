<?php
// Applied only to the disposable development store by docker/setup.sh.
defined('ABSPATH') || exit;
update_option('woocommerce_ship_to_countries', '');
update_option('woocommerce_ship_to_destination', 'shipping');
update_option('woocommerce_hold_stock_minutes', 35);
update_option('woocommerce_manage_stock', 'yes');
update_option('woocommerce_allowed_countries', 'specific');
update_option('woocommerce_specific_allowed_countries', ['IT']);
$zones = WC_Shipping_Zones::get_zones();
$zone = null;
foreach ($zones as $data) { if ($data['zone_name'] === 'EVA Italy') { $zone = new WC_Shipping_Zone($data['zone_id']); break; } }
if (!$zone) {
    $zone = new WC_Shipping_Zone(); $zone->set_zone_name('EVA Italy'); $zone->add_location('IT', 'country'); $zone->save();
    $flat = $zone->add_shipping_method('flat_rate');
    update_option('woocommerce_flat_rate_' . $flat . '_settings', ['enabled' => 'yes', 'title' => 'Standard delivery', 'cost' => '5', 'tax_status' => 'none']);
    $free = $zone->add_shipping_method('free_shipping');
    update_option('woocommerce_free_shipping_' . $free . '_settings', ['enabled' => 'yes', 'title' => 'Free shipping', 'requires' => 'min_amount', 'min_amount' => '50']);
}
foreach (['COFFEE10' => ['percent', 10], 'FREECOFFEE' => ['percent', 100]] as $code => $data) {
    if (!wc_get_coupon_id_by_code($code)) { $coupon = new WC_Coupon(); $coupon->set_code($code); $coupon->set_discount_type($data[0]); $coupon->set_amount($data[1]); $coupon->set_free_shipping($code === 'FREECOFFEE'); $coupon->save(); }
}
// Add a genuine zero-total virtual product for the no-Stripe checkout branch.
if (!get_posts(['post_type' => 'product', 'title' => 'EVA Free Test Sample', 'numberposts' => 1])) {
    $sample = new WC_Product_Simple(); $sample->set_name('EVA Free Test Sample'); $sample->set_regular_price('0'); $sample->set_virtual(true); $sample->set_status('publish'); $sample->save();
}
echo "Native shipping, coupons, stock holds and zero-total sample configured\n";
