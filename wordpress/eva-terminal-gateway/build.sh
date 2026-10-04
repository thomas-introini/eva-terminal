#!/bin/sh
set -eu
cd "$(dirname "$0")"
composer install --no-interaction --prefer-dist
vendor/bin/php-scoper add-prefix --config=scoper.inc.php --output-dir=vendor-prefixed --force
cp vendor/stripe/stripe-php/LICENSE vendor-prefixed/LICENSE.stripe
php tests/sdk.php
mkdir -p ../../dist
rm -f ../../dist/eva-terminal-gateway.zip
cd ..
zip -qr ../dist/eva-terminal-gateway.zip eva-terminal-gateway/eva-terminal-gateway.php eva-terminal-gateway/includes eva-terminal-gateway/vendor-prefixed
