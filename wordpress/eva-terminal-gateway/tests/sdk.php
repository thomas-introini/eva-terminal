<?php
require dirname(__DIR__) . '/includes/stripe.php';
if (class_exists('Stripe\\StripeClient', false)) { throw new RuntimeException('Unprefixed Stripe class leaked'); }
$client = new EvaTerminalVendor\Stripe\StripeClient(['api_key' => 'sk_test_build', 'stripe_version' => '2026-09-30.endive']);
if (EvaTerminalVendor\Stripe\Stripe::VERSION !== '22.0.0') { throw new RuntimeException('Wrong SDK version'); }
echo "Isolated Stripe SDK 22.0.0 loaded\n";
