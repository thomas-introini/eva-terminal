<?php
/**
 * Plugin Name: EVA Checkout API
 * Plugin URI: https://github.com/thomas/eva-terminal-go
 * Description: Stateless quote-based checkout REST API for headless WooCommerce integrations.
 * Version: 1.0.0
 * Author: Thomas
 * Author URI: https://github.com/thomas
 * License: GPL-2.0-or-later
 * License URI: https://www.gnu.org/licenses/gpl-2.0.html
 * Text Domain: eva-checkout
 * Domain Path: /languages
 * Requires at least: 6.0
 * Requires PHP: 7.4
 * WC requires at least: 7.0
 * WC tested up to: 8.5
 *
 * @package EVA_Checkout
 */

defined( 'ABSPATH' ) || exit;

// Plugin constants.
define( 'EVA_CHECKOUT_VERSION', '1.0.0' );
define( 'EVA_CHECKOUT_PLUGIN_FILE', __FILE__ );
define( 'EVA_CHECKOUT_PLUGIN_DIR', plugin_dir_path( __FILE__ ) );
define( 'EVA_CHECKOUT_PLUGIN_URL', plugin_dir_url( __FILE__ ) );

/**
 * Main plugin class.
 */
final class EVA_Checkout {

	/**
	 * Single instance of the class.
	 *
	 * @var EVA_Checkout|null
	 */
	private static $instance = null;

	/**
	 * Quote handler instance.
	 *
	 * @var EVA_Quote_Handler|null
	 */
	public $quote_handler = null;

	/**
	 * Order handler instance.
	 *
	 * @var EVA_Order_Handler|null
	 */
	public $order_handler = null;

	/**
	 * Coupon handler instance.
	 *
	 * @var EVA_Coupon_Handler|null
	 */
	public $coupon_handler = null;

	/**
	 * REST controller instance.
	 *
	 * @var EVA_REST_Controller|null
	 */
	public $rest_controller = null;

	/**
	 * Get the single instance.
	 *
	 * @return EVA_Checkout
	 */
	public static function instance() {
		if ( is_null( self::$instance ) ) {
			self::$instance = new self();
		}
		return self::$instance;
	}

	/**
	 * Constructor.
	 */
	private function __construct() {
		$this->check_dependencies();
		$this->includes();
		$this->init_hooks();
	}

	/**
	 * Check if WooCommerce is active.
	 */
	private function check_dependencies() {
		if ( ! class_exists( 'WooCommerce' ) ) {
			add_action( 'admin_notices', array( $this, 'woocommerce_missing_notice' ) );
			return;
		}
	}

	/**
	 * Display admin notice if WooCommerce is not active.
	 */
	public function woocommerce_missing_notice() {
		?>
		<div class="notice notice-error">
			<p><?php esc_html_e( 'EVA Checkout requires WooCommerce to be installed and active.', 'eva-checkout' ); ?></p>
		</div>
		<?php
	}

	/**
	 * Include required files.
	 */
	private function includes() {
		require_once EVA_CHECKOUT_PLUGIN_DIR . 'includes/class-quote-handler.php';
		require_once EVA_CHECKOUT_PLUGIN_DIR . 'includes/class-order-handler.php';
		require_once EVA_CHECKOUT_PLUGIN_DIR . 'includes/class-coupon-handler.php';
		require_once EVA_CHECKOUT_PLUGIN_DIR . 'includes/class-rest-controller.php';
	}

	/**
	 * Initialize hooks.
	 */
	private function init_hooks() {
		add_action( 'plugins_loaded', array( $this, 'on_plugins_loaded' ), 20 );
		add_action( 'before_woocommerce_init', array( $this, 'declare_hpos_compatibility' ) );
	}

	/**
	 * Initialize plugin after all plugins are loaded.
	 */
	public function on_plugins_loaded() {
		if ( ! class_exists( 'WooCommerce' ) ) {
			return;
		}

		// Initialize handlers.
		$this->coupon_handler  = new EVA_Coupon_Handler();
		$this->quote_handler   = new EVA_Quote_Handler( $this->coupon_handler );
		$this->order_handler   = new EVA_Order_Handler( $this->quote_handler );
		$this->rest_controller = new EVA_REST_Controller(
			$this->quote_handler,
			$this->order_handler,
			$this->coupon_handler
		);

		// Register REST routes.
		add_action( 'rest_api_init', array( $this->rest_controller, 'register_routes' ) );
	}

	/**
	 * Declare HPOS compatibility.
	 */
	public function declare_hpos_compatibility() {
		if ( class_exists( '\Automattic\WooCommerce\Utilities\FeaturesUtil' ) ) {
			\Automattic\WooCommerce\Utilities\FeaturesUtil::declare_compatibility(
				'custom_order_tables',
				EVA_CHECKOUT_PLUGIN_FILE,
				true
			);
		}
	}
}

/**
 * Get the main plugin instance.
 *
 * @return EVA_Checkout
 */
function eva_checkout() {
	return EVA_Checkout::instance();
}

// Initialize plugin.
eva_checkout();
