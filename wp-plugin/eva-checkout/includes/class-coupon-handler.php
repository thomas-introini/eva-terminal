<?php
/**
 * Coupon Handler - validates coupons against WooCommerce rules.
 *
 * @package EVA_Checkout
 */

defined( 'ABSPATH' ) || exit;

/**
 * Coupon Handler class.
 */
class EVA_Coupon_Handler {

	/**
	 * Validate a coupon code.
	 *
	 * @param string $code       Coupon code.
	 * @param array  $line_items Line items to validate against.
	 * @param float  $cart_total Cart total for minimum spend checks.
	 * @return array Validation result.
	 */
	public function validate_coupon( string $code, array $line_items = array(), float $cart_total = 0 ): array {
		$code   = wc_format_coupon_code( $code );
		$coupon = new WC_Coupon( $code );

		// Check if coupon exists.
		if ( ! $coupon->get_id() ) {
			return $this->invalid_result( $code, 'coupon_not_found', __( 'Coupon does not exist.', 'eva-checkout' ) );
		}

		// Check if coupon is enabled.
		if ( 'publish' !== get_post_status( $coupon->get_id() ) ) {
			return $this->invalid_result( $code, 'coupon_disabled', __( 'Coupon is not active.', 'eva-checkout' ) );
		}

		// Check expiry date.
		$expiry_date = $coupon->get_date_expires();
		if ( $expiry_date && $expiry_date->getTimestamp() < time() ) {
			return $this->invalid_result( $code, 'coupon_expired', __( 'Coupon has expired.', 'eva-checkout' ) );
		}

		// Check usage limit.
		$usage_limit = $coupon->get_usage_limit();
		$usage_count = $coupon->get_usage_count();
		if ( $usage_limit > 0 && $usage_count >= $usage_limit ) {
			return $this->invalid_result( $code, 'coupon_usage_limit', __( 'Coupon usage limit reached.', 'eva-checkout' ) );
		}

		// Check minimum spend.
		$minimum_amount = $coupon->get_minimum_amount();
		if ( $minimum_amount > 0 && $cart_total < $minimum_amount ) {
			return $this->invalid_result(
				$code,
				'coupon_min_spend',
				/* translators: %s: minimum amount */
				sprintf( __( 'Minimum spend of %s required.', 'eva-checkout' ), wc_price( $minimum_amount ) )
			);
		}

		// Check maximum spend.
		$maximum_amount = $coupon->get_maximum_amount();
		if ( $maximum_amount > 0 && $cart_total > $maximum_amount ) {
			return $this->invalid_result(
				$code,
				'coupon_max_spend',
				/* translators: %s: maximum amount */
				sprintf( __( 'Maximum spend of %s exceeded.', 'eva-checkout' ), wc_price( $maximum_amount ) )
			);
		}

		// Check product restrictions.
		$product_check = $this->check_product_restrictions( $coupon, $line_items );
		if ( is_array( $product_check ) ) {
			return $product_check;
		}

		// Calculate discount.
		$discount = $this->calculate_discount( $coupon, $line_items, $cart_total );

		return $this->valid_result( $code, $coupon, $discount );
	}

	/**
	 * Validate coupon for a dedicated validation request.
	 *
	 * @param array $data Validation request data.
	 * @return array|WP_Error Validation result or error.
	 */
	public function validate_coupon_request( array $data ) {
		if ( empty( $data['code'] ) ) {
			return new WP_Error(
				'eva_missing_code',
				__( 'Coupon code is required.', 'eva-checkout' ),
				array( 'status' => 400 )
			);
		}

		$code       = sanitize_text_field( $data['code'] );
		$line_items = isset( $data['items'] ) ? (array) $data['items'] : array();
		$cart_total = 0;

		// Calculate cart total from items if provided.
		foreach ( $line_items as $item ) {
			$product_id = isset( $item['product_id'] ) ? absint( $item['product_id'] ) : 0;
			$variation_id = isset( $item['variation_id'] ) ? absint( $item['variation_id'] ) : 0;
			$quantity = isset( $item['quantity'] ) ? absint( $item['quantity'] ) : 1;

			$product = $variation_id ? wc_get_product( $variation_id ) : wc_get_product( $product_id );
			if ( $product ) {
				$cart_total += (float) $product->get_price() * $quantity;
			}
		}

		return $this->validate_coupon( $code, $line_items, $cart_total );
	}

	/**
	 * Check product restrictions for a coupon.
	 *
	 * @param WC_Coupon $coupon     Coupon object.
	 * @param array     $line_items Line items.
	 * @return array|true Array with error if invalid, true if valid.
	 */
	private function check_product_restrictions( WC_Coupon $coupon, array $line_items ) {
		$product_ids         = $coupon->get_product_ids();
		$excluded_product_ids = $coupon->get_excluded_product_ids();
		$product_categories  = $coupon->get_product_categories();
		$excluded_categories = $coupon->get_excluded_product_categories();

		// If no restrictions, coupon is valid.
		if ( empty( $product_ids ) && empty( $product_categories ) ) {
			return true;
		}

		$has_valid_product = empty( $product_ids ) && empty( $product_categories );

		foreach ( $line_items as $item ) {
			$product_id   = isset( $item['product_id'] ) ? absint( $item['product_id'] ) : 0;
			$variation_id = isset( $item['variation_id'] ) ? absint( $item['variation_id'] ) : 0;

			if ( ! $product_id ) {
				continue;
			}

			// Check excluded products.
			if ( ! empty( $excluded_product_ids ) ) {
				if ( in_array( $product_id, $excluded_product_ids, true ) || in_array( $variation_id, $excluded_product_ids, true ) ) {
					continue; // Skip excluded products.
				}
			}

			// Check excluded categories.
			if ( ! empty( $excluded_categories ) ) {
				$product_cats = wc_get_product_cat_ids( $product_id );
				if ( array_intersect( $product_cats, $excluded_categories ) ) {
					continue; // Skip products in excluded categories.
				}
			}

			// Check if product is in allowed products.
			if ( ! empty( $product_ids ) ) {
				if ( in_array( $product_id, $product_ids, true ) || in_array( $variation_id, $product_ids, true ) ) {
					$has_valid_product = true;
				}
			}

			// Check if product is in allowed categories.
			if ( ! empty( $product_categories ) ) {
				$product_cats = wc_get_product_cat_ids( $product_id );
				if ( array_intersect( $product_cats, $product_categories ) ) {
					$has_valid_product = true;
				}
			}
		}

		if ( ! $has_valid_product && ( ! empty( $product_ids ) || ! empty( $product_categories ) ) ) {
			return $this->invalid_result(
				$coupon->get_code(),
				'coupon_not_applicable',
				__( 'Coupon is not valid for these products.', 'eva-checkout' )
			);
		}

		return true;
	}

	/**
	 * Calculate discount amount.
	 *
	 * @param WC_Coupon $coupon     Coupon object.
	 * @param array     $line_items Line items.
	 * @param float     $cart_total Cart total.
	 * @return float Discount amount.
	 */
	private function calculate_discount( WC_Coupon $coupon, array $line_items, float $cart_total ): float {
		$discount_type = $coupon->get_discount_type();
		$amount        = (float) $coupon->get_amount();
		$discount      = 0;

		switch ( $discount_type ) {
			case 'percent':
				$discount = $cart_total * ( $amount / 100 );
				break;

			case 'fixed_cart':
				$discount = min( $amount, $cart_total );
				break;

			case 'fixed_product':
				$discount = $this->calculate_fixed_product_discount( $coupon, $line_items );
				break;

			default:
				// Allow extensions to calculate custom discount types.
				$discount = apply_filters( 'eva_checkout_coupon_discount', 0, $coupon, $line_items, $cart_total );
				break;
		}

		// Apply maximum discount cap if set.
		$max_discount = $coupon->get_maximum_amount();
		if ( $max_discount > 0 && $discount > $max_discount ) {
			$discount = $max_discount;
		}

		return round( $discount, wc_get_price_decimals() );
	}

	/**
	 * Calculate fixed product discount.
	 *
	 * @param WC_Coupon $coupon     Coupon object.
	 * @param array     $line_items Line items.
	 * @return float Discount amount.
	 */
	private function calculate_fixed_product_discount( WC_Coupon $coupon, array $line_items ): float {
		$amount      = (float) $coupon->get_amount();
		$product_ids = $coupon->get_product_ids();
		$excluded_ids = $coupon->get_excluded_product_ids();
		$discount    = 0;

		foreach ( $line_items as $item ) {
			$product_id   = isset( $item['product_id'] ) ? absint( $item['product_id'] ) : 0;
			$variation_id = isset( $item['variation_id'] ) ? absint( $item['variation_id'] ) : 0;
			$quantity     = isset( $item['quantity'] ) ? absint( $item['quantity'] ) : 1;

			// Check exclusions.
			if ( in_array( $product_id, $excluded_ids, true ) || in_array( $variation_id, $excluded_ids, true ) ) {
				continue;
			}

			// If product IDs specified, check if this product is included.
			if ( ! empty( $product_ids ) ) {
				if ( ! in_array( $product_id, $product_ids, true ) && ! in_array( $variation_id, $product_ids, true ) ) {
					continue;
				}
			}

			$discount += $amount * $quantity;
		}

		return $discount;
	}

	/**
	 * Build invalid result array.
	 *
	 * @param string $code    Coupon code.
	 * @param string $reason  Error reason code.
	 * @param string $message Error message.
	 * @return array Result array.
	 */
	private function invalid_result( string $code, string $reason, string $message ): array {
		return array(
			'code'     => $code,
			'valid'    => false,
			'discount' => '0',
			'reason'   => $reason,
			'message'  => $message,
		);
	}

	/**
	 * Build valid result array.
	 *
	 * @param string    $code     Coupon code.
	 * @param WC_Coupon $coupon   Coupon object.
	 * @param float     $discount Calculated discount.
	 * @return array Result array.
	 */
	private function valid_result( string $code, WC_Coupon $coupon, float $discount ): array {
		$decimals   = wc_get_price_decimals();
		$multiplier = pow( 10, $decimals );

		return array(
			'code'          => $code,
			'valid'         => true,
			'discount'      => (string) round( $discount * $multiplier ),
			'discount_type' => $coupon->get_discount_type(),
			'amount'        => $coupon->get_amount(),
			'free_shipping' => $coupon->get_free_shipping(),
			'description'   => $coupon->get_description(),
		);
	}
}
