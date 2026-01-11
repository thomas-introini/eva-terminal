<?php
/**
 * Order Handler - manages order creation from quotes.
 *
 * @package EVA_Checkout
 */

defined( 'ABSPATH' ) || exit;

/**
 * Order Handler class.
 */
class EVA_Order_Handler {

	/**
	 * Idempotency key meta key.
	 *
	 * @var string
	 */
	const IDEMPOTENCY_META_KEY = '_eva_idempotency_key';

	/**
	 * Quote ID meta key.
	 *
	 * @var string
	 */
	const QUOTE_ID_META_KEY = '_eva_quote_id';

	/**
	 * Quote handler instance.
	 *
	 * @var EVA_Quote_Handler
	 */
	private $quote_handler;

	/**
	 * Constructor.
	 *
	 * @param EVA_Quote_Handler $quote_handler Quote handler instance.
	 */
	public function __construct( EVA_Quote_Handler $quote_handler ) {
		$this->quote_handler = $quote_handler;
	}

	/**
	 * Create an order from a quote.
	 *
	 * @param array $data Order request data.
	 * @return array|WP_Error Order response or error.
	 */
	public function create_order( array $data ) {
		// Validate required fields.
		$required_fields = array( 'quote_id', 'billing_address', 'payment_method' );
		foreach ( $required_fields as $field ) {
			if ( empty( $data[ $field ] ) ) {
				return new WP_Error(
					'eva_missing_field',
					/* translators: %s: field name */
					sprintf( __( 'Field "%s" is required.', 'eva-checkout' ), $field ),
					array( 'status' => 400 )
				);
			}
		}

		// Check idempotency.
		$idempotency_key = isset( $data['idempotency_key'] ) ? sanitize_text_field( $data['idempotency_key'] ) : '';
		if ( $idempotency_key ) {
			$existing_order = $this->find_order_by_idempotency_key( $idempotency_key );
			if ( $existing_order ) {
				return $this->format_order_response( $existing_order, 'existing' );
			}
		}

		// Retrieve quote.
		$quote_data = $this->quote_handler->get_quote_data( $data['quote_id'] );
		if ( ! $quote_data ) {
			return new WP_Error(
				'eva_quote_expired',
				__( 'Quote has expired or does not exist.', 'eva-checkout' ),
				array( 'status' => 400 )
			);
		}

		// Validate shipping rate if needed.
		$shipping_rate_id = isset( $data['shipping_rate_id'] ) ? sanitize_text_field( $data['shipping_rate_id'] ) : '';
		$selected_rate    = null;

		if ( ! empty( $quote_data['quote']['shipping_rates'] ) ) {
			if ( empty( $shipping_rate_id ) ) {
				return new WP_Error(
					'eva_missing_shipping',
					__( 'Shipping rate selection is required.', 'eva-checkout' ),
					array( 'status' => 400 )
				);
			}

			$selected_rate = $this->find_shipping_rate( $quote_data['quote']['shipping_rates'], $shipping_rate_id );
			if ( ! $selected_rate ) {
				return new WP_Error(
					'eva_invalid_shipping_rate',
					__( 'Selected shipping rate is not available.', 'eva-checkout' ),
					array( 'status' => 400 )
				);
			}
		}

		// Re-validate stock before creating order.
		$stock_check = $this->validate_stock( $quote_data['line_items_raw'] );
		if ( is_wp_error( $stock_check ) ) {
			return $stock_check;
		}

		// Create the WooCommerce order.
		try {
			$order = $this->create_wc_order( $data, $quote_data, $selected_rate, $idempotency_key );
		} catch ( Exception $e ) {
			return new WP_Error(
				'eva_order_creation_failed',
				/* translators: %s: error message */
				sprintf( __( 'Failed to create order: %s', 'eva-checkout' ), $e->getMessage() ),
				array( 'status' => 500 )
			);
		}

		// Delete the quote after successful order creation.
		$this->quote_handler->delete_quote( $data['quote_id'] );

		return $this->format_order_response( $order, 'created' );
	}

	/**
	 * Create WooCommerce order.
	 *
	 * @param array       $data            Request data.
	 * @param array       $quote_data      Quote data.
	 * @param array|null  $selected_rate   Selected shipping rate.
	 * @param string      $idempotency_key Idempotency key.
	 * @return WC_Order Order object.
	 * @throws Exception If order creation fails.
	 */
	private function create_wc_order( array $data, array $quote_data, $selected_rate, string $idempotency_key ): WC_Order {
		$order = wc_create_order( array(
			'status'      => 'pending',
			'customer_id' => $quote_data['customer_id'] ?? 0,
		) );

		if ( is_wp_error( $order ) ) {
			throw new Exception( $order->get_error_message() );
		}

		// Add line items.
		foreach ( $quote_data['line_items_raw'] as $item ) {
			$product = $item['variation_id'] ? wc_get_product( $item['variation_id'] ) : wc_get_product( $item['product_id'] );

			if ( ! $product ) {
				throw new Exception( sprintf( 'Product %d not found.', $item['product_id'] ) );
			}

			$order_item_id = $order->add_product( $product, $item['quantity'] );

			// Add item meta.
			if ( ! empty( $item['meta'] ) && $order_item_id ) {
				foreach ( $item['meta'] as $key => $value ) {
					wc_add_order_item_meta( $order_item_id, $key, $value );
				}
			}
		}

		// Add shipping.
		if ( $selected_rate ) {
			$shipping_item = new WC_Order_Item_Shipping();
			$shipping_item->set_method_title( $selected_rate['label'] );
			$shipping_item->set_method_id( $selected_rate['method_id'] );
			$shipping_item->set_instance_id( $selected_rate['instance_id'] );
			$shipping_item->set_total( $this->parse_price( $selected_rate['cost'] ) );

			$order->add_item( $shipping_item );
		}

		// Apply coupons.
		if ( ! empty( $quote_data['coupon_codes'] ) ) {
			foreach ( $quote_data['coupon_codes'] as $coupon_code ) {
				$order->apply_coupon( $coupon_code );
			}
		}

		// Set addresses.
		$billing_address = $this->sanitize_address( $data['billing_address'] );
		$order->set_address( $billing_address, 'billing' );

		$shipping_address = isset( $data['shipping_address'] )
			? $this->sanitize_address( $data['shipping_address'] )
			: $billing_address;
		$order->set_address( $shipping_address, 'shipping' );

		// Set customer email.
		if ( ! empty( $data['customer_email'] ) ) {
			$order->set_billing_email( sanitize_email( $data['customer_email'] ) );
		}

		// Set payment method.
		$payment_method = sanitize_text_field( $data['payment_method'] );
		$order->set_payment_method( $payment_method );

		// Get payment gateway title.
		$gateways = WC()->payment_gateways()->payment_gateways();
		if ( isset( $gateways[ $payment_method ] ) ) {
			$order->set_payment_method_title( $gateways[ $payment_method ]->get_title() );
		}

		// Set customer note.
		if ( ! empty( $data['customer_note'] ) ) {
			$order->set_customer_note( sanitize_textarea_field( $data['customer_note'] ) );
		}

		// Calculate totals.
		$order->calculate_totals();

		// Store meta data.
		$order->update_meta_data( self::QUOTE_ID_META_KEY, $data['quote_id'] );
		if ( $idempotency_key ) {
			$order->update_meta_data( self::IDEMPOTENCY_META_KEY, $idempotency_key );
		}
		$order->update_meta_data( '_eva_created_via', 'eva-checkout-api' );

		// Determine initial status based on payment method.
		$initial_status = $this->get_initial_order_status( $payment_method, $data );
		$order->set_status( $initial_status );

		// Save order.
		$order->save();

		// Reduce stock levels.
		wc_reduce_stock_levels( $order->get_id() );

		// Trigger order created action.
		do_action( 'woocommerce_new_order', $order->get_id(), $order );

		return $order;
	}

	/**
	 * Get initial order status based on payment method.
	 *
	 * @param string $payment_method Payment method ID.
	 * @param array  $data           Request data.
	 * @return string Order status.
	 */
	private function get_initial_order_status( string $payment_method, array $data ): string {
		// Check if marked as paid externally.
		if ( ! empty( $data['set_paid'] ) && $data['set_paid'] === true ) {
			return 'processing';
		}

		// Payment methods that don't require online payment.
		$offline_methods = array( 'bacs', 'cheque', 'cod' );

		if ( in_array( $payment_method, $offline_methods, true ) ) {
			return 'cod' === $payment_method ? 'processing' : 'on-hold';
		}

		// Default to pending for online payment methods.
		return 'pending';
	}

	/**
	 * Find order by idempotency key.
	 *
	 * @param string $idempotency_key Idempotency key.
	 * @return WC_Order|null Order or null.
	 */
	private function find_order_by_idempotency_key( string $idempotency_key ) {
		$orders = wc_get_orders( array(
			'limit'      => 1,
			'meta_key'   => self::IDEMPOTENCY_META_KEY,
			'meta_value' => $idempotency_key,
		) );

		return ! empty( $orders ) ? $orders[0] : null;
	}

	/**
	 * Find shipping rate by ID.
	 *
	 * @param array  $rates   Available rates.
	 * @param string $rate_id Rate ID to find.
	 * @return array|null Rate data or null.
	 */
	private function find_shipping_rate( array $rates, string $rate_id ) {
		foreach ( $rates as $rate ) {
			if ( $rate['rate_id'] === $rate_id ) {
				return $rate;
			}
		}
		return null;
	}

	/**
	 * Validate stock for all items.
	 *
	 * @param array $line_items Line items.
	 * @return true|WP_Error True if valid, error otherwise.
	 */
	private function validate_stock( array $line_items ) {
		foreach ( $line_items as $item ) {
			$product = $item['variation_id']
				? wc_get_product( $item['variation_id'] )
				: wc_get_product( $item['product_id'] );

			if ( ! $product ) {
				return new WP_Error(
					'eva_product_not_found',
					/* translators: %d: product ID */
					sprintf( __( 'Product %d no longer exists.', 'eva-checkout' ), $item['product_id'] ),
					array( 'status' => 400 )
				);
			}

			if ( ! $product->is_in_stock() ) {
				return new WP_Error(
					'eva_out_of_stock',
					/* translators: %s: product name */
					sprintf( __( '"%s" is out of stock.', 'eva-checkout' ), $product->get_name() ),
					array( 'status' => 400 )
				);
			}

			if ( $product->managing_stock() ) {
				$stock = $product->get_stock_quantity();
				if ( $stock !== null && $stock < $item['quantity'] && ! $product->backorders_allowed() ) {
					return new WP_Error(
						'eva_insufficient_stock',
						/* translators: 1: product name, 2: available quantity */
						sprintf(
							__( 'Not enough stock for "%1$s". Only %2$d available.', 'eva-checkout' ),
							$product->get_name(),
							$stock
						),
						array( 'status' => 400 )
					);
				}
			}
		}

		return true;
	}

	/**
	 * Sanitize address data.
	 *
	 * @param array $address Address data.
	 * @return array Sanitized address.
	 */
	private function sanitize_address( array $address ): array {
		return array(
			'first_name' => isset( $address['first_name'] ) ? sanitize_text_field( $address['first_name'] ) : '',
			'last_name'  => isset( $address['last_name'] ) ? sanitize_text_field( $address['last_name'] ) : '',
			'company'    => isset( $address['company'] ) ? sanitize_text_field( $address['company'] ) : '',
			'address_1'  => isset( $address['address_1'] ) ? sanitize_text_field( $address['address_1'] ) : '',
			'address_2'  => isset( $address['address_2'] ) ? sanitize_text_field( $address['address_2'] ) : '',
			'city'       => isset( $address['city'] ) ? sanitize_text_field( $address['city'] ) : '',
			'state'      => isset( $address['state'] ) ? sanitize_text_field( $address['state'] ) : '',
			'postcode'   => isset( $address['postcode'] ) ? sanitize_text_field( $address['postcode'] ) : '',
			'country'    => isset( $address['country'] ) ? sanitize_text_field( $address['country'] ) : '',
			'email'      => isset( $address['email'] ) ? sanitize_email( $address['email'] ) : '',
			'phone'      => isset( $address['phone'] ) ? sanitize_text_field( $address['phone'] ) : '',
		);
	}

	/**
	 * Format order response.
	 *
	 * @param WC_Order $order  Order object.
	 * @param string   $action Action (created or existing).
	 * @return array Response data.
	 */
	private function format_order_response( WC_Order $order, string $action ): array {
		$decimals   = wc_get_price_decimals();
		$multiplier = pow( 10, $decimals );

		return array(
			'order_id'    => $order->get_id(),
			'order_key'   => $order->get_order_key(),
			'status'      => $order->get_status(),
			'totals'      => array(
				'subtotal' => (string) round( (float) $order->get_subtotal() * $multiplier ),
				'shipping' => (string) round( (float) $order->get_shipping_total() * $multiplier ),
				'discount' => (string) round( (float) $order->get_discount_total() * $multiplier ),
				'tax'      => (string) round( (float) $order->get_total_tax() * $multiplier ),
				'total'    => (string) round( (float) $order->get_total() * $multiplier ),
			),
			'payment_url' => $order->get_checkout_payment_url(),
			'next_action' => $this->determine_next_action( $order ),
			'created'     => 'created' === $action,
		);
	}

	/**
	 * Determine next action for the order.
	 *
	 * @param WC_Order $order Order object.
	 * @return string Next action identifier.
	 */
	private function determine_next_action( WC_Order $order ): string {
		$status = $order->get_status();

		switch ( $status ) {
			case 'pending':
				return 'await_payment';
			case 'on-hold':
				return 'await_payment_confirmation';
			case 'processing':
				return 'order_confirmed';
			case 'completed':
				return 'order_complete';
			default:
				return 'unknown';
		}
	}

	/**
	 * Parse price from minor units string.
	 *
	 * @param string $price_string Price in minor units.
	 * @return float Price value.
	 */
	private function parse_price( string $price_string ): float {
		$decimals   = wc_get_price_decimals();
		$multiplier = pow( 10, $decimals );
		return (float) $price_string / $multiplier;
	}
}
