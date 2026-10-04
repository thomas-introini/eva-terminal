package storefront

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/thomas/eva-terminal-go/internal/storeapi"
)

type Intent struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	Grind    string `json:"grind,omitempty"`
	Quantity int    `json:"quantity"`
}

type SessionState struct {
	Version         int                             `json:"version"`
	Store           string                          `json:"store"`
	CustomerRef     string                          `json:"customer_ref"`
	CartToken       string                          `json:"cart_token"`
	Cart            storeapi.Cart                   `json:"cart"`
	Desired         []Intent                        `json:"desired"`
	Revision        uint64                          `json:"revision"`
	Pending         bool                            `json:"pending"`
	Attempt         *storeapi.Attempt               `json:"attempt,omitempty"`
	Checkout        *storeapi.BridgeCheckoutRequest `json:"checkout,omitempty"`
	Billing         *storeapi.CustomerAddress       `json:"billing,omitempty"`
	ShippingAddress *storeapi.CustomerAddress       `json:"shipping_address,omitempty"`
	Error           string                          `json:"error,omitempty"`
}

type Sessions struct {
	mu              sync.Mutex
	ctx             context.Context
	stateDir, store string
	newClient       func(string) *storeapi.Client
	byKey           map[string]*Session
}

func NewSessions(ctx context.Context, dir, store string, factory func(string) *storeapi.Client) *Sessions {
	return &Sessions{ctx: ctx, stateDir: dir, store: store, newClient: factory, byKey: make(map[string]*Session)}
}

// Open accepts only the fingerprint of the key verified by the SSH handshake.
func (m *Sessions) Open(fingerprint string) (*Session, error) {
	if fingerprint == "" {
		return nil, errors.New("a verified SSH key is required")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	key := digest(m.store + "\x00" + fingerprint)
	if s := m.byKey[key]; s != nil {
		return s, nil
	}
	s := &Session{ctx: m.ctx, path: filepath.Join(m.stateDir, "sessions", key+".json"), wake: make(chan struct{}, 1), subscribers: make(map[chan SessionState]struct{})}
	if err := readJSON(s.path, &s.state); err != nil {
		return nil, err
	}
	if s.state.Version != 0 && (s.state.Version != 1 || s.state.Store != m.store || s.state.CustomerRef != key) {
		return nil, errors.New("unsupported shopper state")
	}
	if s.state.Version == 0 {
		s.state = SessionState{Version: 1, Store: m.store, CustomerRef: key}
	}
	s.client = m.newClient(s.state.CartToken)
	m.byKey[key] = s
	go s.run()
	return s, nil
}

type Session struct {
	mu          sync.Mutex
	opMu        sync.Mutex
	ctx         context.Context
	client      *storeapi.Client
	path        string
	state       SessionState
	wake        chan struct{}
	subscribers map[chan SessionState]struct{}
}

func (s *Session) Snapshot() SessionState { s.mu.Lock(); defer s.mu.Unlock(); return clone(s.state) }

func (s *Session) Subscribe(ctx context.Context) <-chan SessionState {
	ch := make(chan SessionState, 1)
	s.mu.Lock()
	s.subscribers[ch] = struct{}{}
	ch <- clone(s.state)
	s.mu.Unlock()
	go func() { <-ctx.Done(); s.mu.Lock(); delete(s.subscribers, ch); close(ch); s.mu.Unlock() }()
	return ch
}

// publishLocked persists before telling connected terminals that a change was accepted.
func (s *Session) publishLocked() error {
	s.state.CartToken, _ = s.client.SessionState()
	err := atomicJSON(s.path, s.state)
	if err != nil {
		s.state.Error = "Could not persist shopper state; checkout is unavailable"
		return err
	}
	for ch := range s.subscribers {
		select {
		case <-ch:
		default:
		}
		ch <- clone(s.state)
	}
	return err
}

func (s *Session) changed(fn func(*SessionState) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	before := clone(s.state)
	if err := fn(&s.state); err != nil {
		return err
	}
	s.state.Error = ""
	if err := s.publishLocked(); err != nil {
		s.state = before
		return err
	}
	select {
	case s.wake <- struct{}{}:
	default:
	}
	return nil
}

func (s *Session) Add(item Intent) error {
	if item.ID <= 0 || item.Quantity <= 0 {
		return errors.New("invalid product or quantity")
	}
	return s.changed(func(st *SessionState) error {
		if st.Attempt != nil && st.Attempt.Active() {
			return errors.New("cancel the pending payment before editing this cart")
		}
		for i := range st.Desired {
			if sameIntent(st.Desired[i], item) {
				st.Desired[i].Quantity += item.Quantity
				st.Revision++
				st.Pending = true
				return nil
			}
		}
		st.Desired = append(st.Desired, item)
		st.Revision++
		st.Pending = true
		return nil
	})
}

func (s *Session) SetQuantity(id int, grind string, quantity int) error {
	if quantity < 0 {
		return errors.New("invalid quantity")
	}
	return s.quantity(id, grind, func(int) int { return quantity })
}

func (s *Session) AdjustQuantity(id int, grind string, delta int) error {
	return s.quantity(id, grind, func(current int) int { return max(1, current+delta) })
}

func (s *Session) quantity(id int, grind string, value func(int) int) error {
	return s.changed(func(st *SessionState) error {
		if st.Attempt != nil && st.Attempt.Active() {
			return errors.New("cancel the pending payment before editing this cart")
		}
		for i, item := range st.Desired {
			if item.ID == id && item.Grind == grind {
				quantity := value(item.Quantity)
				if quantity == 0 {
					st.Desired = append(st.Desired[:i], st.Desired[i+1:]...)
				} else {
					st.Desired[i].Quantity = quantity
				}
				st.Revision++
				st.Pending = true
				return nil
			}
		}
		return errors.New("cart item no longer exists")
	})
}

func sameIntent(a, b Intent) bool { return a.ID == b.ID && a.Grind == b.Grind }

func cartIntent(item storeapi.CartItem) Intent {
	var ext struct {
		Grind string `json:"grind"`
	}
	_ = json.Unmarshal(item.Extensions["eva_terminal"], &ext)
	return Intent{ID: item.ID, Name: item.Name, Grind: ext.Grind, Quantity: item.Quantity}
}

func intents(cart storeapi.Cart) []Intent {
	items := make([]Intent, 0, len(cart.Items))
	for _, item := range cart.Items {
		items = append(items, cartIntent(item))
	}
	return items
}

func (s *Session) run() {
	// Recover an order before touching a cart that native checkout may have emptied.
	if s.Snapshot().Attempt != nil {
		_ = s.Recover(s.ctx)
	}
	if st := s.Snapshot(); st.Attempt == nil || !st.Attempt.Active() {
		_ = s.Sync(s.ctx)
	}
	retry := time.NewTicker(5 * time.Second)
	defer retry.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-retry.C:
			if s.Snapshot().Pending {
				_ = s.Sync(s.ctx)
			}
		case <-s.wake:
			timer := time.NewTimer(250 * time.Millisecond)
		debounce:
			for {
				select {
				case <-s.ctx.Done():
					timer.Stop()
					return
				case <-s.wake:
					if !timer.Stop() {
						select {
						case <-timer.C:
						default:
						}
					}
					timer.Reset(250 * time.Millisecond)
				case <-timer.C:
					break debounce
				}
			}
			_ = s.Sync(s.ctx)
		}
	}
}

// Sync serializes every Woo write and reconciles GET before retrying ambiguous writes.
func (s *Session) Sync(ctx context.Context) error {
	s.opMu.Lock()
	defer s.opMu.Unlock()
	return s.sync(ctx)
}

func (s *Session) sync(ctx context.Context) error {
	st := s.Snapshot()
	savedCoupons := append([]storeapi.CartCoupon(nil), st.Cart.Coupons...)
	if st.Attempt != nil && st.Attempt.Active() {
		return errors.New("payment is pending")
	}
	cart, err := s.client.GetCart(ctx)
	expired := false
	if err != nil {
		var api *storeapi.StoreError
		if errors.As(err, &api) && strings.Contains(api.Code, "token") && (api.StatusCode == 401 || api.StatusCode == 403) {
			s.client.ResetSession()
			expired = true
			cart, err = s.client.GetCart(ctx)
		}
	}
	if err != nil {
		return s.failed(err, st.Revision, false)
	}
	if expired && st.Billing == nil && st.Cart.BillingAddress.Email != "" {
		s.mu.Lock()
		s.state.Billing = &st.Cart.BillingAddress
		s.state.ShippingAddress = &st.Cart.ShippingAddress
		s.mu.Unlock()
		st = s.Snapshot()
	}
	if st.Billing != nil && st.ShippingAddress != nil && (*st.Billing != cart.BillingAddress || *st.ShippingAddress != cart.ShippingAddress) {
		cart, err = s.client.UpdateCustomer(ctx, storeapi.UpdateCustomerRequest{BillingAddress: st.Billing, ShippingAddress: st.ShippingAddress})
		if err != nil {
			if recovered, e := s.client.GetCart(ctx); e == nil {
				_ = s.confirm(*recovered)
			}
			var api *storeapi.StoreError
			if errors.As(err, &api) && api.StatusCode >= 400 && api.StatusCode < 500 && api.StatusCode != 408 && api.StatusCode != 429 {
				s.mu.Lock()
				s.state.Billing = &s.state.Cart.BillingAddress
				s.state.ShippingAddress = &s.state.Cart.ShippingAddress
				s.mu.Unlock()
			}
			return s.failed(err, st.Revision, false)
		}
		s.mu.Lock()
		s.state.Billing = &cart.BillingAddress
		s.state.ShippingAddress = &cart.ShippingAddress
		s.mu.Unlock()
	}
	// Persist the result of every successful call; an interrupted add is discovered by the next GET.
	if err = s.confirm(*cart); err != nil {
		return err
	}
	for {
		st = s.Snapshot()
		var next *storeapi.Cart
		changed := false
		for _, actual := range cart.Items {
			wanted := 0
			for _, item := range st.Desired {
				if sameIntent(cartIntent(actual), item) {
					wanted = item.Quantity
					break
				}
			}
			if wanted == actual.Quantity {
				continue
			}
			changed = true
			limits := actual.QuantityLimits
			if wanted > 0 && ((limits.Maximum > 0 && wanted > limits.Maximum) || wanted < limits.Minimum || (limits.MultipleOf > 0 && wanted%limits.MultipleOf != 0)) {
				return s.failed(errors.New("quantity is outside WooCommerce limits"), st.Revision, true)
			}
			if wanted == 0 {
				next, err = s.client.RemoveItem(ctx, storeapi.RemoveItemRequest{Key: actual.Key})
			} else {
				next, err = s.client.UpdateItem(ctx, storeapi.UpdateItemRequest{Key: actual.Key, Quantity: wanted})
			}
			break
		}
		if !changed {

			for _, wanted := range st.Desired {
				found := false
				for _, actual := range cart.Items {
					if sameIntent(cartIntent(actual), wanted) {
						found = true
						break
					}
				}
				if found {
					continue
				}
				changed = true
				req := storeapi.AddItemRequest{ID: wanted.ID, Quantity: wanted.Quantity}
				if wanted.Grind != "" {
					req.Extensions = map[string]any{"eva_terminal": map[string]string{"grind": wanted.Grind}}
				}
				next, err = s.client.AddItem(ctx, req)
				break
			}
		}
		if !changed {
			if expired {
				expired = false
				for _, coupon := range savedCoupons {
					next, e := s.client.ApplyCoupon(ctx, storeapi.CouponRequest{Code: coupon.Code})
					if e != nil {
						return s.failed(e, st.Revision, false)
					}
					cart = next
					if e = s.confirm(*cart); e != nil {
						return e
					}
				}
			}
			s.mu.Lock()
			if s.state.Revision == st.Revision {
				s.state.Pending = false
				s.state.Error = ""
			}
			err = s.publishLocked()
			s.mu.Unlock()
			return err
		}
		if err != nil {
			var api *storeapi.StoreError
			validation := errors.As(err, &api) && api.StatusCode >= 400 && api.StatusCode < 500 && api.StatusCode != 408 && api.StatusCode != 429
			// Even a validation response can contain changes from extensions; fetch the authoritative cart.
			if recovered, e := s.client.GetCart(ctx); e == nil {
				_ = s.confirm(*recovered)
			}
			return s.failed(err, st.Revision, validation)
		}
		if bytes.Equal(mustJSON(cart.Items), mustJSON(next.Items)) {
			_ = s.confirm(*next)
			return s.failed(errors.New("WooCommerce could not apply the requested quantity"), st.Revision, true)
		}
		cart = next
		if err = s.confirm(*cart); err != nil {
			return err
		}
	}
}

func mustJSON(value any) []byte { data, _ := json.Marshal(value); return data }

func (s *Session) confirm(cart storeapi.Cart) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.Cart = cart
	return s.publishLocked()
}

func (s *Session) failed(err error, revision uint64, reject bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.Error = err.Error()
	if reject && s.state.Revision == revision {
		s.state.Desired = intents(s.state.Cart)
		s.state.Pending = false
	}
	_ = s.publishLocked()
	return err
}

func (s *Session) cartOperation(ctx context.Context, fn func() (*storeapi.Cart, error)) error {
	s.opMu.Lock()
	defer s.opMu.Unlock()
	if err := s.sync(ctx); err != nil {
		return err
	}
	cart, err := fn()
	if err != nil {
		return s.failed(err, s.Snapshot().Revision, false)
	}
	return s.confirm(*cart)
}

func (s *Session) Address(ctx context.Context, billing, shipping storeapi.CustomerAddress) error {
	s.opMu.Lock()
	defer s.opMu.Unlock()
	if st := s.Snapshot(); st.Attempt != nil && st.Attempt.Active() {
		return errors.New("payment is pending")
	}
	shipping.Email = ""
	s.mu.Lock()
	s.state.Billing = &billing
	s.state.ShippingAddress = &shipping
	s.state.Pending = true
	s.state.Revision++
	err := s.publishLocked()
	s.mu.Unlock()
	if err != nil {
		return err
	}
	return s.sync(ctx)
}
func (s *Session) Coupon(ctx context.Context, code string, remove bool) error {
	return s.cartOperation(ctx, func() (*storeapi.Cart, error) {
		cart := s.Snapshot().Cart
		found := false
		for _, coupon := range cart.Coupons {
			if strings.EqualFold(coupon.Code, code) {
				found = true
				break
			}
		}
		if (found && !remove) || (!found && remove) {
			return &cart, nil
		}
		if remove {
			return s.client.RemoveCoupon(ctx, storeapi.CouponRequest{Code: code})
		}
		return s.client.ApplyCoupon(ctx, storeapi.CouponRequest{Code: code})
	})
}
func (s *Session) Shipping(ctx context.Context, packageID int, rate string) error {
	return s.cartOperation(ctx, func() (*storeapi.Cart, error) {
		return s.client.SelectShippingRate(ctx, storeapi.SelectShippingRateRequest{PackageID: packageID, RateID: rate})
	})
}
func (s *Session) Prepare(ctx context.Context) error {
	return s.cartOperation(ctx, func() (*storeapi.Cart, error) { return s.client.PrepareCart(ctx) })
}

func QuoteFromCart(cart storeapi.Cart) (storeapi.Quote, error) {
	if len(cart.Errors) > 0 {
		messages := make([]string, 0, len(cart.Errors))
		for _, issue := range cart.Errors {
			messages = append(messages, issue.Message)
		}
		return storeapi.Quote{}, errors.New(strings.Join(messages, "; "))
	}
	data, err := json.Marshal(cart.Extensions["eva_terminal"])
	if err != nil {
		return storeapi.Quote{}, err
	}
	var ext struct {
		Quote storeapi.Quote `json:"quote"`
	}
	if err = json.Unmarshal(data, &ext); err != nil {
		return ext.Quote, err
	}
	if ext.Quote.Fingerprint == "" {
		return ext.Quote, errors.New("WooCommerce terminal gateway is not configured")
	}
	return ext.Quote, nil
}

func (s *Session) Checkout(ctx context.Context, accepted storeapi.Quote, fields storeapi.CheckoutRequest) error {
	s.opMu.Lock()
	defer s.opMu.Unlock()
	st := s.Snapshot()
	if st.Attempt != nil && st.Attempt.Active() {
		return s.recover(ctx)
	}
	if err := s.sync(ctx); err != nil {
		return err
	}
	st = s.Snapshot()
	current, err := QuoteFromCart(s.Snapshot().Cart)
	if err != nil {
		return err
	}
	if current != accepted {
		return errors.New("WooCommerce quote changed; review and confirm the updated total")
	}
	var random [16]byte
	if _, err = rand.Read(random[:]); err != nil {
		return err
	}
	id := hex.EncodeToString(random[:])
	fields.PaymentMethod = storeapi.GatewayID
	req := storeapi.BridgeCheckoutRequest{AttemptID: id, CustomerRef: st.CustomerRef, AcceptedQuote: accepted, Checkout: fields}
	s.mu.Lock()
	if s.state.Pending || s.state.Revision != st.Revision {
		s.mu.Unlock()
		return errors.New("cart changed; wait for synchronization and confirm again")
	}
	before := clone(s.state)
	s.state.Checkout = &req
	s.state.Attempt = &storeapi.Attempt{AttemptID: id, PaymentState: "submitting", Total: accepted.Total, Currency: accepted.Currency}
	err = s.publishLocked()
	if err != nil {
		s.state = before
	}
	s.mu.Unlock()
	if err != nil {
		return err
	}
	a, err := s.client.BridgeCheckout(ctx, req)
	if err != nil {
		var api *storeapi.StoreError
		if errors.As(err, &api) && (api.StatusCode == 400 || api.StatusCode == 409) {
			// A rejection before order creation can safely unlock editing only after the server confirms no attempt exists.
			if _, lookupErr := s.client.GetAttempt(ctx, id, st.CustomerRef); errors.As(lookupErr, &api) && api.StatusCode == 404 {
				_ = s.acceptAttempt(storeapi.Attempt{AttemptID: id, PaymentState: "failed"})
				return s.failed(err, st.Revision, false)
			}
		}
		// Never allocate a new ID after an unknown response; reconnect recovers this exact request.
		s.mu.Lock()
		s.state.Attempt.PaymentState = "resolving"
		s.state.Error = err.Error()
		_ = s.publishLocked()
		s.mu.Unlock()
		return err
	}
	return s.acceptAttempt(*a)
}

func (s *Session) acceptAttempt(a storeapi.Attempt) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.Attempt = &a
	s.state.Error = ""
	if a.PaymentState == "paid" {
		s.state.Desired = nil
		s.state.Pending = false
		s.state.Cart.Items = nil
		s.state.Cart.Coupons = nil
		s.state.Cart.ShippingRates = nil
		s.state.Cart.Extensions = nil
		s.state.Cart.Errors = nil
		s.state.Cart.ItemsCount = 0
		s.state.Cart.Totals = storeapi.CartTotals{CurrencyCode: a.Currency, CurrencyMinorUnit: 2, TotalItems: "0", TotalPrice: "0", TotalShipping: "0", TotalFees: "0", TotalDiscount: "0", TotalTax: "0"}
	}
	if a.PaymentState == "cancelled" || a.PaymentState == "expired" {
		s.state.Pending = true
	}
	return s.publishLocked()
}

func (s *Session) Recover(ctx context.Context) error {
	s.opMu.Lock()
	defer s.opMu.Unlock()
	return s.recover(ctx)
}

func (s *Session) recover(ctx context.Context) error {
	st := s.Snapshot()
	if st.Attempt == nil {
		return nil
	}
	a, err := s.client.GetAttempt(ctx, st.Attempt.AttemptID, st.CustomerRef)
	if err != nil {
		var api *storeapi.StoreError
		if errors.As(err, &api) && api.StatusCode == 404 && st.Checkout != nil {
			a, err = s.client.BridgeCheckout(ctx, *st.Checkout)
		}
		if err != nil {
			return s.failed(err, st.Revision, false)
		}
	}
	if a.PaymentState == "resolving" && st.Checkout != nil {
		a, err = s.client.BridgeCheckout(ctx, *st.Checkout)
		if err != nil {
			return s.failed(err, st.Revision, false)
		}
	}
	if err = s.acceptAttempt(*a); err != nil {
		return err
	}
	if a.PaymentState == "cancelled" || a.PaymentState == "expired" {
		return s.sync(ctx)
	}
	return nil
}

func (s *Session) Cancel(ctx context.Context) error {
	s.opMu.Lock()
	defer s.opMu.Unlock()
	st := s.Snapshot()
	if st.Attempt == nil || !st.Attempt.Active() {
		return nil
	}
	a, err := s.client.CancelAttempt(ctx, st.Attempt.AttemptID, st.CustomerRef)
	if err != nil {
		return s.failed(err, st.Revision, false)
	}
	if err = s.acceptAttempt(*a); err != nil {
		return err
	}
	if a.PaymentState == "paid" {
		return fmt.Errorf("payment already completed; order #%d is paid", a.OrderID)
	}
	return s.sync(ctx)
}
