<?php
/**
 * REST Controller - registers and handles REST API endpoints.
 *
 * @package EVA_Checkout
 */

defined( 'ABSPATH' ) || exit;

/**
 * REST Controller class.
 */
class EVA_REST_Controller {

	/**
	 * REST namespace.
	 *
	 * @var string
	 */
	const NAMESPACE = 'eva/v1';

	/**
	 * Quote handler instance.
	 *
	 * @var EVA_Quote_Handler
	 */
	private $quote_handler;

	/**
	 * Order handler instance.
	 *
	 * @var EVA_Order_Handler
	 */
	private $order_handler;

	/**
	 * Coupon handler instance.
	 *
	 * @var EVA_Coupon_Handler
	 */
	private $coupon_handler;

	/**
	 * Constructor.
	 *
	 * @param EVA_Quote_Handler  $quote_handler  Quote handler instance.
	 * @param EVA_Order_Handler  $order_handler  Order handler instance.
	 * @param EVA_Coupon_Handler $coupon_handler Coupon handler instance.
	 */
	public function __construct(
		EVA_Quote_Handler $quote_handler,
		EVA_Order_Handler $order_handler,
		EVA_Coupon_Handler $coupon_handler
	) {
		$this->quote_handler  = $quote_handler;
		$this->order_handler  = $order_handler;
		$this->coupon_handler = $coupon_handler;
	}

	/**
	 * Register REST routes.
	 */
	public function register_routes() {
		// Quote endpoints.
		register_rest_route( self::NAMESPACE, '/quote', array(
			array(
				'methods'             => WP_REST_Server::CREATABLE,
				'callback'            => array( $this, 'create_quote' ),
				'permission_callback' => array( $this, 'check_api_permission' ),
				'args'                => $this->get_quote_args(),
			),
		) );

		register_rest_route( self::NAMESPACE, '/quote/(?P<id>[a-zA-Z0-9_]+)', array(
			array(
				'methods'             => WP_REST_Server::READABLE,
				'callback'            => array( $this, 'get_quote' ),
				'permission_callback' => array( $this, 'check_api_permission' ),
				'args'                => array(
					'id' => array(
						'description' => __( 'Quote ID.', 'eva-checkout' ),
						'type'        => 'string',
						'required'    => true,
					),
				),
			),
		) );

		// Coupon validation endpoint.
		register_rest_route( self::NAMESPACE, '/coupon/validate', array(
			array(
				'methods'             => WP_REST_Server::CREATABLE,
				'callback'            => array( $this, 'validate_coupon' ),
				'permission_callback' => array( $this, 'check_api_permission' ),
				'args'                => $this->get_coupon_args(),
			),
		) );

		// Order endpoint.
		register_rest_route( self::NAMESPACE, '/order', array(
			array(
				'methods'             => WP_REST_Server::CREATABLE,
				'callback'            => array( $this, 'create_order' ),
				'permission_callback' => array( $this, 'check_api_permission' ),
				'args'                => $this->get_order_args(),
			),
		) );

		// Health check endpoint (public).
		register_rest_route( self::NAMESPACE, '/health', array(
			array(
				'methods'             => WP_REST_Server::READABLE,
				'callback'            => array( $this, 'health_check' ),
				'permission_callback' => '__return_true',
			),
		) );
	}

	/**
	 * Check API permission.
	 *
	 * Uses WooCommerce API key authentication.
	 *
	 * @param WP_REST_Request $request Request object.
	 * @return bool|WP_Error True if permitted, error otherwise.
	 */
	public function check_api_permission( WP_REST_Request $request ) {
		// Check for WooCommerce API authentication.
		$consumer_key    = '';
		$consumer_secret = '';

		// Check Authorization header.
		$auth_header = $request->get_header( 'Authorization' );
		if ( $auth_header && 0 === strpos( $auth_header, 'Basic ' ) ) {
			$credentials = base64_decode( substr( $auth_header, 6 ) );
			if ( $credentials && strpos( $credentials, ':' ) !== false ) {
				list( $consumer_key, $consumer_secret ) = explode( ':', $credentials, 2 );
			}
		}

		// Check query parameters (for compatibility).
		if ( empty( $consumer_key ) ) {
			$consumer_key    = $request->get_param( 'consumer_key' );
			$consumer_secret = $request->get_param( 'consumer_secret' );
		}

		if ( empty( $consumer_key ) || empty( $consumer_secret ) ) {
			return new WP_Error(
				'eva_unauthorized',
				__( 'API authentication required.', 'eva-checkout' ),
				array( 'status' => 401 )
			);
		}

		// Validate WooCommerce API keys.
		global $wpdb;

		$key = $wpdb->get_row(
			$wpdb->prepare(
				"SELECT key_id, user_id, permissions, consumer_secret
				FROM {$wpdb->prefix}woocommerce_api_keys
				WHERE consumer_key = %s",
				wc_api_hash( sanitize_text_field( $consumer_key ) )
			)
		);

		if ( ! $key ) {
			return new WP_Error(
				'eva_invalid_api_key',
				__( 'Invalid API key.', 'eva-checkout' ),
				array( 'status' => 401 )
			);
		}

		// Verify secret.
		if ( ! hash_equals( $key->consumer_secret, $consumer_secret ) ) {
			return new WP_Error(
				'eva_invalid_api_secret',
				__( 'Invalid API secret.', 'eva-checkout' ),
				array( 'status' => 401 )
			);
		}

		// Check permissions.
		if ( 'read' === $key->permissions ) {
			$method = $request->get_method();
			if ( ! in_array( $method, array( 'GET', 'HEAD', 'OPTIONS' ), true ) ) {
				return new WP_Error(
					'eva_insufficient_permissions',
					__( 'API key does not have write permissions.', 'eva-checkout' ),
					array( 'status' => 403 )
				);
			}
		}

		// Set current user for the request.
		wp_set_current_user( $key->user_id );

		return true;
	}

	/**
	 * Create quote endpoint.
	 *
	 * @param WP_REST_Request $request Request object.
	 * @return WP_REST_Response|WP_Error Response or error.
	 */
	public function create_quote( WP_REST_Request $request ) {
		$data = array(
			'items'            => $request->get_param( 'items' ),
			'coupons'          => $request->get_param( 'coupons' ) ?? array(),
			'shipping_address' => $request->get_param( 'shipping_address' ) ?? array(),
			'customer_id'      => $request->get_param( 'customer_id' ) ?? 0,
		);

		$result = $this->quote_handler->create_quote( $data );

		if ( is_wp_error( $result ) ) {
			return $result;
		}

		return new WP_REST_Response( $result, 201 );
	}

	/**
	 * Get quote endpoint.
	 *
	 * @param WP_REST_Request $request Request object.
	 * @return WP_REST_Response|WP_Error Response or error.
	 */
	public function get_quote( WP_REST_Request $request ) {
		$quote_id = sanitize_text_field( $request->get_param( 'id' ) );
		$result   = $this->quote_handler->get_quote( $quote_id );

		if ( is_wp_error( $result ) ) {
			return $result;
		}

		return new WP_REST_Response( $result, 200 );
	}

	/**
	 * Validate coupon endpoint.
	 *
	 * @param WP_REST_Request $request Request object.
	 * @return WP_REST_Response|WP_Error Response or error.
	 */
	public function validate_coupon( WP_REST_Request $request ) {
		$data = array(
			'code'  => $request->get_param( 'code' ),
			'items' => $request->get_param( 'items' ) ?? array(),
		);

		$result = $this->coupon_handler->validate_coupon_request( $data );

		if ( is_wp_error( $result ) ) {
			return $result;
		}

		return new WP_REST_Response( $result, 200 );
	}

	/**
	 * Create order endpoint.
	 *
	 * @param WP_REST_Request $request Request object.
	 * @return WP_REST_Response|WP_Error Response or error.
	 */
	public function create_order( WP_REST_Request $request ) {
		$data = array(
			'quote_id'         => $request->get_param( 'quote_id' ),
			'idempotency_key'  => $request->get_param( 'idempotency_key' ) ?? '',
			'shipping_rate_id' => $request->get_param( 'shipping_rate_id' ) ?? '',
			'billing_address'  => $request->get_param( 'billing_address' ),
			'shipping_address' => $request->get_param( 'shipping_address' ) ?? array(),
			'customer_email'   => $request->get_param( 'customer_email' ) ?? '',
			'payment_method'   => $request->get_param( 'payment_method' ),
			'customer_note'    => $request->get_param( 'customer_note' ) ?? '',
			'set_paid'         => $request->get_param( 'set_paid' ) ?? false,
		);

		$result = $this->order_handler->create_order( $data );

		if ( is_wp_error( $result ) ) {
			return $result;
		}

		$status = $result['created'] ? 201 : 200;
		return new WP_REST_Response( $result, $status );
	}

	/**
	 * Health check endpoint.
	 *
	 * @return WP_REST_Response Response.
	 */
	public function health_check() {
		return new WP_REST_Response( array(
			'status'  => 'ok',
			'version' => EVA_CHECKOUT_VERSION,
			'wc'      => defined( 'WC_VERSION' ) ? WC_VERSION : 'not_installed',
		), 200 );
	}

	/**
	 * Get quote endpoint arguments.
	 *
	 * @return array Arguments.
	 */
	private function get_quote_args(): array {
		return array(
			'items'            => array(
				'description' => __( 'Array of cart items.', 'eva-checkout' ),
				'type'        => 'array',
				'required'    => true,
				'items'       => array(
					'type'       => 'object',
					'properties' => array(
						'product_id'   => array(
							'type'     => 'integer',
							'required' => true,
						),
						'variation_id' => array(
							'type'    => 'integer',
							'default' => 0,
						),
						'quantity'     => array(
							'type'    => 'integer',
							'default' => 1,
							'minimum' => 1,
						),
						'meta'         => array(
							'type'    => 'object',
							'default' => array(),
						),
					),
				),
			),
			'coupons'          => array(
				'description' => __( 'Array of coupon codes.', 'eva-checkout' ),
				'type'        => 'array',
				'default'     => array(),
				'items'       => array(
					'type' => 'string',
				),
			),
			'shipping_address' => array(
				'description' => __( 'Shipping address for rate calculation.', 'eva-checkout' ),
				'type'        => 'object',
				'default'     => array(),
				'properties'  => array(
					'country'   => array( 'type' => 'string' ),
					'state'     => array( 'type' => 'string' ),
					'postcode'  => array( 'type' => 'string' ),
					'city'      => array( 'type' => 'string' ),
					'address_1' => array( 'type' => 'string' ),
					'address_2' => array( 'type' => 'string' ),
				),
			),
			'customer_id'      => array(
				'description' => __( 'Customer ID for logged-in users.', 'eva-checkout' ),
				'type'        => 'integer',
				'default'     => 0,
			),
		);
	}

	/**
	 * Get coupon validation arguments.
	 *
	 * @return array Arguments.
	 */
	private function get_coupon_args(): array {
		return array(
			'code'  => array(
				'description' => __( 'Coupon code to validate.', 'eva-checkout' ),
				'type'        => 'string',
				'required'    => true,
			),
			'items' => array(
				'description' => __( 'Cart items for validation context.', 'eva-checkout' ),
				'type'        => 'array',
				'default'     => array(),
				'items'       => array(
					'type'       => 'object',
					'properties' => array(
						'product_id'   => array( 'type' => 'integer' ),
						'variation_id' => array( 'type' => 'integer' ),
						'quantity'     => array( 'type' => 'integer' ),
					),
				),
			),
		);
	}

	/**
	 * Get order creation arguments.
	 *
	 * @return array Arguments.
	 */
	private function get_order_args(): array {
		return array(
			'quote_id'         => array(
				'description' => __( 'Quote ID from previous quote request.', 'eva-checkout' ),
				'type'        => 'string',
				'required'    => true,
			),
			'idempotency_key'  => array(
				'description' => __( 'Unique key for idempotent order creation.', 'eva-checkout' ),
				'type'        => 'string',
				'default'     => '',
			),
			'shipping_rate_id' => array(
				'description' => __( 'Selected shipping rate ID.', 'eva-checkout' ),
				'type'        => 'string',
				'default'     => '',
			),
			'billing_address'  => array(
				'description' => __( 'Billing address.', 'eva-checkout' ),
				'type'        => 'object',
				'required'    => true,
				'properties'  => $this->get_address_properties(),
			),
			'shipping_address' => array(
				'description' => __( 'Shipping address.', 'eva-checkout' ),
				'type'        => 'object',
				'default'     => array(),
				'properties'  => $this->get_address_properties(),
			),
			'customer_email'   => array(
				'description' => __( 'Customer email address.', 'eva-checkout' ),
				'type'        => 'string',
				'format'      => 'email',
			),
			'payment_method'   => array(
				'description' => __( 'Payment method ID.', 'eva-checkout' ),
				'type'        => 'string',
				'required'    => true,
			),
			'customer_note'    => array(
				'description' => __( 'Customer order note.', 'eva-checkout' ),
				'type'        => 'string',
				'default'     => '',
			),
			'set_paid'         => array(
				'description' => __( 'Mark order as paid (for external payment).', 'eva-checkout' ),
				'type'        => 'boolean',
				'default'     => false,
			),
		);
	}

	/**
	 * Get address properties schema.
	 *
	 * @return array Properties.
	 */
	private function get_address_properties(): array {
		return array(
			'first_name' => array( 'type' => 'string' ),
			'last_name'  => array( 'type' => 'string' ),
			'company'    => array( 'type' => 'string' ),
			'address_1'  => array( 'type' => 'string' ),
			'address_2'  => array( 'type' => 'string' ),
			'city'       => array( 'type' => 'string' ),
			'state'      => array( 'type' => 'string' ),
			'postcode'   => array( 'type' => 'string' ),
			'country'    => array( 'type' => 'string' ),
			'email'      => array( 'type' => 'string', 'format' => 'email' ),
			'phone'      => array( 'type' => 'string' ),
		);
	}
}
