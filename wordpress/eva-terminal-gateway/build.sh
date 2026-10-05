#!/bin/sh
set -eu
cd "$(dirname "$0")"
plugin_version=$(sed -n 's/^[[:space:]]*\* Version: \([0-9A-Za-z.+-]*\)[[:space:]]*$/\1/p' eva-terminal-gateway.php)
: "${plugin_version:?Plugin Version header is missing or invalid}"
zip_name="eva-terminal-gateway-${plugin_version}.zip"
composer install --no-interaction --prefer-dist
vendor/bin/php-scoper add-prefix --config=scoper.inc.php --output-dir=vendor-prefixed --force
cp vendor/stripe/stripe-php/LICENSE vendor-prefixed/LICENSE.stripe
php tests/sdk.php
mkdir -p ../../dist
rm -f "../../dist/$zip_name"
cd ..
zip -qr "../dist/$zip_name" eva-terminal-gateway/eva-terminal-gateway.php eva-terminal-gateway/includes eva-terminal-gateway/vendor-prefixed
