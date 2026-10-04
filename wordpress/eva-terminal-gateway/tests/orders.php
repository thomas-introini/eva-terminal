<?php
// Run with wp eval-file in the isolated test stack AFTER integration.py.
defined('ABSPATH') || exit;
global $wpdb;
$row = $wpdb->get_row('SELECT * FROM ' . Eva_Terminal_Bridge::table() . " WHERE state='paid' AND stripe_intent<>'' ORDER BY created_at DESC LIMIT 1", ARRAY_A);
if (!$row) { throw new RuntimeException('No paid integration order'); }
$order = wc_get_order($row['order_id']);
if (!$order->get_date_paid() || $order->get_transaction_id() !== $row['stripe_intent']) { throw new RuntimeException('Woo native payment lifecycle missing'); }
if ((int)get_option('eva_test_payment_hook_' . $order->get_id()) !== 1) { throw new RuntimeException('Native payment hook did not run exactly once'); }
$line = array_values($order->get_items())[0];
if ($line->get_meta('Grind') !== 'Espresso') { throw new RuntimeException('Grind order line metadata missing'); }
$product = $line->get_product();
$stock = $product->get_stock_quantity();
Eva_Terminal_Bridge::reconcile($row);
$fresh = wc_get_product($product->get_id());
if ($stock !== $fresh->get_stock_quantity()) { throw new RuntimeException('Duplicate webhook reduced stock twice'); }
$gateway = new Eva_Terminal_Gateway();
$_SERVER['HTTP_X_EVA_TEST_LOSE_REFUND'] = '1';
$unknown = wc_create_refund(['order_id' => $order->get_id(), 'amount' => '1.00', 'reason' => 'EVA partial refund test', 'refund_payment' => true, 'restock_items' => false]);
if (!is_wp_error($unknown)) { throw new RuntimeException('Lost refund response not surfaced'); }
unset($_SERVER['HTTP_X_EVA_TEST_LOSE_REFUND']);
$refund = wc_create_refund(['order_id' => $order->get_id(), 'amount' => '1.00', 'reason' => 'EVA partial refund test', 'refund_payment' => true, 'restock_items' => false]);
if (is_wp_error($refund)) { throw new RuntimeException($refund->get_error_message()); }
$order = wc_get_order($order->get_id());
if (Eva_Terminal_Bridge::minor($order->get_total_refunded()) !== 100) { throw new RuntimeException('Partial Woo refund missing'); }
$wpdb->update(Eva_Terminal_Bridge::table(), ['refund_after' => 0], ['attempt_id' => $row['attempt_id']]);
Eva_Terminal_Bridge::reconcile($wpdb->get_row($wpdb->prepare('SELECT * FROM ' . Eva_Terminal_Bridge::table() . ' WHERE attempt_id=%s', $row['attempt_id']), ARRAY_A));
$order = wc_get_order($order->get_id());
if (Eva_Terminal_Bridge::minor($order->get_total_refunded()) !== 100) { throw new RuntimeException('Webhook duplicated Woo refund'); }
$remaining = number_format((Eva_Terminal_Bridge::minor($order->get_total()) - 100) / 100, 2, '.', '');
$refund = wc_create_refund(['order_id' => $order->get_id(), 'amount' => $remaining, 'reason' => 'EVA full refund test', 'refund_payment' => true, 'restock_items' => false]);
if (is_wp_error($refund)) { throw new RuntimeException($refund->get_error_message()); }
$order = wc_get_order($order->get_id());
if (Eva_Terminal_Bridge::minor($order->get_total_refunded()) !== Eva_Terminal_Bridge::minor($order->get_total())) { throw new RuntimeException('Full refund amount mismatch'); }
echo "Native payment/stock hooks, grind metadata, partial/full refunds and refund deduplication passed\n";
