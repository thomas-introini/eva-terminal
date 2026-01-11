<?php
/**
 * Quote Handler - manages quote creation and storage.
 *
 * @package EVA_Checkout
 */

defined( 'ABSPATH' ) || exit;

/**
 * Quote Handler class.
 */
class EVA_Quote_Handler {

	/**
	 * Quote transient prefix.
	 *
	 * @var string
	 */
	const TRANSIENT_PREFIX = 'eva_quote_';

	/**
	 * Quote TTL in seconds (15 minutes).
	 *
	 * @var int
	 */
	const QUOTE_TTL = 900;

	/**
	 * Coupon handler instance.
	 *
	 * @var EVA_Coupon_Handler
	 */
	private $coupon_handler;

	/**
	 * Constructor.
	 *
	 * @param EVA_Coupon_Handler $coupon_handler Coupon handler instance.
	 */
	public function __construct( EVA_Coupon_Handler $coupon_handler ) {
		$this->coupon_handler = $coupon_handler;
	}

	/**
	 * Create a quote from request data.
	 *
	 * @param array $data Quote request data.
	 * @return array|WP_Error Quote response or error.
	 */
	public function create_quote( array $data ) {
		// Validate required fields.
		if ( empty( $data['items'] ) || ! is_array( $data['items'] ) ) {
			return new WP_Error(
				'eva_invalid_items',
				__( 'Items array is required.', 'eva-checkout' ),
				array( 'status' => 400 )
			);
		}

		// Build line items and validate products.
		$line_items   = array();
		$stock_status = array();
		$cart_total   = 0;

		foreach ( $data['items'] as $item ) {
			$result = $this->process_line_item( $item );
			if ( is_wp_error( $result ) ) {
				return $result;
			}

			$line_items[]   = $result['line_item'];
			$stock_status[] = $result['stock_status'];
			$cart_total    += (float) $result['line_item']['line_total'];
		}

		// Process coupons.
		$coupons          = array();
		$total_discount   = 0;
		$coupon_codes     = isset( $data['coupons'] ) ? (array) $data['coupons'] : array();

		foreach ( $coupon_codes as $code ) {
			$validation = $this->coupon_handler->validate_coupon( $code, $line_items, $cart_total );
			$coupons[]  = $validation;

			if ( $validation['valid'] ) {
				$total_discount += (float) $validation['discount'];
			}
		}

		// Calculate shipping rates.
		$shipping_address = isset( $data['shipping_address'] ) ? $data['shipping_address'] : array();
		$shipping_rates   = $this->calculate_shipping_rates( $line_items, $shipping_address );

		// Calculate taxes.
		$tax_result = $this->calculate_taxes( $line_items, $shipping_address );

		// Build totals.
		$subtotal = $cart_total;
		$tax      = $tax_result['total_tax'];

		// Get currency info.
		$currency = array(
			'code'     => get_woocommerce_currency(),
			'symbol'   => get_woocommerce_currency_symbol(),
			'decimals' => wc_get_price_decimals(),
		);

		// Generate quote ID.
		$quote_id   = $this->generate_quote_id();
		$expires_at = gmdate( 'Y-m-d\TH:i:s\Z', time() + self::QUOTE_TTL );

		// Build quote response.
		$quote = array(
			'quote_id'       => $quote_id,
			'expires_at'     => $expires_at,
			'line_items'     => $this->format_line_items_for_response( $line_items, $tax_result['line_taxes'] ),
			'coupons'        => $coupons,
			'shipping_rates' => $shipping_rates,
			'totals'         => array(
				'subtotal' => $this->format_price( $subtotal ),
				'discount' => $this->format_price( $total_discount ),
				'shipping' => '0', // Will be set when shipping is selected.
				'tax'      => $this->format_price( $tax ),
				'total'    => $this->format_price( $subtotal - $total_discount + $tax ),
			),
			'currency'       => $currency,
			'stock_status'   => $stock_status,
		);

		// Store quote data for later retrieval.
		$this->store_quote( $quote_id, array(
			'quote'           => $quote,
			'line_items_raw'  => $line_items,
			'shipping_address' => $shipping_address,
			'coupon_codes'    => $coupon_codes,
			'customer_id'     => isset( $data['customer_id'] ) ? absint( $data['customer_id'] ) : 0,
		) );

		return $quote;
	}

	/**
	 * Get a quote by ID.
	 *
	 * @param string $quote_id Quote ID.
	 * @return array|WP_Error Quote data or error.
	 */
	public function get_quote( string $quote_id ) {
		$stored = $this->retrieve_quote( $quote_id );

		if ( ! $stored ) {
			return new WP_Error(
				'eva_quote_not_found',
				__( 'Quote not found or expired.', 'eva-checkout' ),
				array( 'status' => 404 )
			);
		}

		return $stored['quote'];
	}

	/**
	 * Get stored quote data (internal use).
	 *
	 * @param string $quote_id Quote ID.
	 * @return array|null Stored quote data or null.
	 */
	public function get_quote_data( string $quote_id ) {
		return $this->retrieve_quote( $quote_id );
	}

	/**
	 * Delete a quote.
	 *
	 * @param string $quote_id Quote ID.
	 */
	public function delete_quote( string $quote_id ) {
		delete_transient( self::TRANSIENT_PREFIX . $quote_id );
	}

	/**
	 * Process a single line item.
	 *
	 * @param array $item Item data.
	 * @return array|WP_Error Processed item or error.
	 */
	private function process_line_item( array $item ) {
		$product_id   = isset( $item['product_id'] ) ? absint( $item['product_id'] ) : 0;
		$variation_id = isset( $item['variation_id'] ) ? absint( $item['variation_id'] ) : 0;
		$quantity     = isset( $item['quantity'] ) ? absint( $item['quantity'] ) : 1;
		$meta         = isset( $item['meta'] ) ? (array) $item['meta'] : array();

		if ( ! $product_id ) {
			return new WP_Error(
				'eva_invalid_product',
				__( 'Product ID is required.', 'eva-checkout' ),
				array( 'status' => 400 )
			);
		}

		// Get product.
		$product = $variation_id ? wc_get_product( $variation_id ) : wc_get_product( $product_id );

		if ( ! $product || ! $product->exists() ) {
			return new WP_Error(
				'eva_product_not_found',
				/* translators: %d: product ID */
				sprintf( __( 'Product %d not found.', 'eva-checkout' ), $product_id ),
				array( 'status' => 404 )
			);
		}

		if ( ! $product->is_purchasable() ) {
			return new WP_Error(
				'eva_product_not_purchasable',
				/* translators: %s: product name */
				sprintf( __( 'Product "%s" is not purchasable.', 'eva-checkout' ), $product->get_name() ),
				array( 'status' => 400 )
			);
		}

		// Check stock.
		$stock_status = $this->check_stock( $product, $quantity );

		// Calculate prices.
		$price      = (float) $product->get_price();
		$line_total = $price * $quantity;

		// Build line item.
		$line_item = array(
			'product_id'   => $product_id,
			'variation_id' => $variation_id,
			'product'      => $product,
			'name'         => $product->get_name(),
			'sku'          => $product->get_sku(),
			'quantity'     => $quantity,
			'unit_price'   => $price,
			'line_total'   => $line_total,
			'meta'         => $meta,
			'tax_class'    => $product->get_tax_class(),
		);

		return array(
			'line_item'    => $line_item,
			'stock_status' => $stock_status,
		);
	}

	/**
	 * Check product stock.
	 *
	 * @param WC_Product $product  Product object.
	 * @param int        $quantity Requested quantity.
	 * @return array Stock status.
	 */
	private function check_stock( WC_Product $product, int $quantity ): array {
		$available = $product->get_stock_quantity();
		$manage    = $product->managing_stock();

		if ( ! $manage ) {
			// Not managing stock - check stock status.
			$in_stock = $product->is_in_stock();
			return array(
				'product_id' => $product->get_id(),
				'available'  => $in_stock ? PHP_INT_MAX : 0,
				'requested'  => $quantity,
				'ok'         => $in_stock,
			);
		}

		return array(
			'product_id' => $product->get_id(),
			'available'  => $available ?? 0,
			'requested'  => $quantity,
			'ok'         => ( $available === null || $available >= $quantity ) || $product->backorders_allowed(),
		);
	}

	/**
	 * Calculate shipping rates.
	 *
	 * @param array $line_items      Line items.
	 * @param array $shipping_address Shipping address.
	 * @return array Shipping rates.
	 */
	private function calculate_shipping_rates( array $line_items, array $shipping_address ): array {
		if ( empty( $shipping_address ) ) {
			return array();
		}

		// Check if any items need shipping.
		$needs_shipping = false;
		foreach ( $line_items as $item ) {
			if ( $item['product']->needs_shipping() ) {
				$needs_shipping = true;
				break;
			}
		}

		if ( ! $needs_shipping ) {
			return array();
		}

		// Build package for shipping calculation.
		$package = $this->build_shipping_package( $line_items, $shipping_address );

		// Calculate shipping rates.
		$shipping = new WC_Shipping();
		$shipping->calculate_shipping( array( $package ) );

		$rates = array();
		foreach ( $shipping->get_packages() as $package_data ) {
			if ( ! empty( $package_data['rates'] ) ) {
				foreach ( $package_data['rates'] as $rate ) {
					$rates[] = array(
						'rate_id'     => $rate->get_id(),
						'method_id'   => $rate->get_method_id(),
						'instance_id' => $rate->get_instance_id(),
						'label'       => $rate->get_label(),
						'cost'        => $this->format_price( $rate->get_cost() ),
						'tax'         => $this->format_price( array_sum( $rate->get_taxes() ) ),
						'meta_data'   => $rate->get_meta_data(),
					);
				}
			}
		}

		return $rates;
	}

	/**
	 * Build shipping package.
	 *
	 * @param array $line_items      Line items.
	 * @param array $shipping_address Shipping address.
	 * @return array Package data.
	 */
	private function build_shipping_package( array $line_items, array $shipping_address ): array {
		$contents       = array();
		$contents_cost  = 0;
		$contents_total = 0;

		foreach ( $line_items as $key => $item ) {
			$product = $item['product'];

			if ( ! $product->needs_shipping() ) {
				continue;
			}

			$contents[ $key ] = array(
				'key'               => $key,
				'product_id'        => $item['product_id'],
				'variation_id'      => $item['variation_id'],
				'variation'         => array(),
				'quantity'          => $item['quantity'],
				'data'              => $product,
				'line_total'        => $item['line_total'],
				'line_tax'          => 0,
				'line_subtotal'     => $item['line_total'],
				'line_subtotal_tax' => 0,
			);

			$contents_cost  += $item['line_total'];
			$contents_total += $item['line_total'];
		}

		return array(
			'contents'        => $contents,
			'contents_cost'   => $contents_cost,
			'applied_coupons' => array(),
			'user'            => array( 'ID' => 0 ),
			'destination'     => array(
				'country'   => $shipping_address['country'] ?? '',
				'state'     => $shipping_address['state'] ?? '',
				'postcode'  => $shipping_address['postcode'] ?? '',
				'city'      => $shipping_address['city'] ?? '',
				'address'   => $shipping_address['address_1'] ?? '',
				'address_1' => $shipping_address['address_1'] ?? '',
				'address_2' => $shipping_address['address_2'] ?? '',
			),
			'cart_subtotal'   => $contents_total,
		);
	}

	/**
	 * Calculate taxes for line items.
	 *
	 * @param array $line_items      Line items.
	 * @param array $shipping_address Shipping address.
	 * @return array Tax calculation results.
	 */
	private function calculate_taxes( array $line_items, array $shipping_address ): array {
		if ( ! wc_tax_enabled() ) {
			return array(
				'total_tax'  => 0,
				'line_taxes' => array(),
			);
		}

		$line_taxes = array();
		$total_tax  = 0;

		// Get tax location.
		$country  = $shipping_address['country'] ?? WC()->countries->get_base_country();
		$state    = $shipping_address['state'] ?? WC()->countries->get_base_state();
		$postcode = $shipping_address['postcode'] ?? WC()->countries->get_base_postcode();
		$city     = $shipping_address['city'] ?? WC()->countries->get_base_city();

		foreach ( $line_items as $key => $item ) {
			$tax_class = $item['tax_class'];
			$rates     = WC_Tax::find_rates( array(
				'country'   => $country,
				'state'     => $state,
				'postcode'  => $postcode,
				'city'      => $city,
				'tax_class' => $tax_class,
			) );

			$taxes    = WC_Tax::calc_tax( $item['line_total'], $rates, wc_prices_include_tax() );
			$item_tax = array_sum( $taxes );

			$line_taxes[ $key ] = $item_tax;
			$total_tax         += $item_tax;
		}

		return array(
			'total_tax'  => $total_tax,
			'line_taxes' => $line_taxes,
		);
	}

	/**
	 * Format line items for API response.
	 *
	 * @param array $line_items Line items.
	 * @param array $line_taxes Line taxes.
	 * @return array Formatted line items.
	 */
	private function format_line_items_for_response( array $line_items, array $line_taxes ): array {
		$formatted = array();

		foreach ( $line_items as $key => $item ) {
			$tax = isset( $line_taxes[ $key ] ) ? $line_taxes[ $key ] : 0;

			$formatted[] = array(
				'product_id'   => $item['product_id'],
				'variation_id' => $item['variation_id'],
				'name'         => $item['name'],
				'sku'          => $item['sku'],
				'quantity'     => $item['quantity'],
				'unit_price'   => $this->format_price( $item['unit_price'] ),
				'line_total'   => $this->format_price( $item['line_total'] ),
				'line_tax'     => $this->format_price( $tax ),
				'meta'         => $item['meta'],
			);
		}

		return $formatted;
	}

	/**
	 * Generate unique quote ID.
	 *
	 * @return string Quote ID.
	 */
	private function generate_quote_id(): string {
		return 'qt_' . wp_generate_password( 16, false );
	}

	/**
	 * Store quote in transient.
	 *
	 * @param string $quote_id Quote ID.
	 * @param array  $data     Quote data.
	 */
	private function store_quote( string $quote_id, array $data ) {
		// Remove WC_Product objects before storing (not serializable well).
		if ( isset( $data['line_items_raw'] ) ) {
			foreach ( $data['line_items_raw'] as &$item ) {
				unset( $item['product'] );
			}
		}

		set_transient( self::TRANSIENT_PREFIX . $quote_id, $data, self::QUOTE_TTL );
	}

	/**
	 * Retrieve quote from transient.
	 *
	 * @param string $quote_id Quote ID.
	 * @return array|null Quote data or null.
	 */
	private function retrieve_quote( string $quote_id ) {
		$data = get_transient( self::TRANSIENT_PREFIX . $quote_id );
		return $data ?: null;
	}

	/**
	 * Format price to minor units string.
	 *
	 * @param float $price Price value.
	 * @return string Price in minor units.
	 */
	private function format_price( float $price ): string {
		$decimals = wc_get_price_decimals();
		$multiplier = pow( 10, $decimals );
		return (string) round( $price * $multiplier );
	}
}
