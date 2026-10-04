<?php
// A private PSR-4 loader keeps this SDK independent of other Stripe extensions.
spl_autoload_register(static function ($class) {
    $prefix = 'EvaTerminalVendor\\Stripe\\';
    if (strncmp($class, $prefix, strlen($prefix)) !== 0) { return; }
    $relative = str_replace('\\', '/', substr($class, strlen($prefix))) . '.php';
    $base = dirname(__DIR__) . '/vendor-prefixed/';
    // PHP-Scoper preserves input paths relative to the project root.
    foreach ([$base . 'vendor/stripe/stripe-php/lib/', $base] as $dir) {
        if (is_file($dir . $relative)) { require $dir . $relative; return; }
    }
});
