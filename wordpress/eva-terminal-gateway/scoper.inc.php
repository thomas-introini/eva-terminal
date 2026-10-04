<?php
return [
    'prefix' => 'EvaTerminalVendor',
    'finders' => [Symfony\Component\Finder\Finder::create()->files()->in(__DIR__ . '/vendor/stripe/stripe-php/lib')],
    'expose-global-classes' => false,
    'expose-global-functions' => false,
    'expose-global-constants' => false,
];
