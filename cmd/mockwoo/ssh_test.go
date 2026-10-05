package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"io"
	"net"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/ssh"
	"charm.land/wish/v2"
	"charm.land/wish/v2/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/thomas/eva-terminal-go/internal/storeapi"
	"github.com/thomas/eva-terminal-go/internal/storefront"
	"github.com/thomas/eva-terminal-go/internal/tui"
	gossh "golang.org/x/crypto/ssh"
)

// Observe the actual Tea view while driving the app through a real SSH PTY.
// This avoids brittle assertions against the terminal renderer's ANSI diffs.
type sshView struct {
	mu    sync.Mutex
	text  string
	state tui.ViewState
}
type observedModel struct {
	tea.Model
	view *sshView
}

func (m observedModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	updated, cmd := m.Model.Update(msg)
	m.Model = updated
	return m, cmd
}
func (m observedModel) View() tea.View {
	v := m.Model.View()
	m.view.mu.Lock()
	m.view.text = ansi.Strip(v.Content)
	m.view.state = m.Model.(tui.Model).GetViewState()
	m.view.mu.Unlock()
	return v
}
func (v *sshView) snapshot() (string, tui.ViewState) {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.text, v.state
}
func (v *sshView) wait(t *testing.T, state tui.ViewState, text string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		content, current := v.snapshot()
		if current == state && strings.Contains(content, text) && (!strings.Contains(content, "EVA · Help") || strings.Contains(text, "EVA · Help")) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	content, current := v.snapshot()
	t.Fatalf("SSH expected screen %d containing %q, got %d:\n%s", state, text, current, content)
}

func TestMockSSHShoppingWalkthrough(t *testing.T) {
	t.Setenv("EVA_BRIDGE_KEY", "test-bridge-key-with-at-least-32-characters")
	t.Setenv("MOCK_PAYMENT_SECONDS", "0")
	t.Setenv("MOCK_API_DELAY_MS", "10")
	store := newMockStore()
	backend := httptest.NewServer(store)
	defer backend.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	dir := t.TempDir()
	factory := func(token string) *storeapi.Client {
		return storeapi.NewClient(backend.URL, storeapi.WithSessionTokens(token, ""), storeapi.WithBridgeKey("test-bridge-key-with-at-least-32-characters"))
	}
	catalog, err := storefront.NewCatalog(factory(""), dir, backend.URL)
	if err != nil {
		t.Fatal(err)
	}
	if err = catalog.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	shoppers := storefront.NewSessions(ctx, dir, backend.URL, factory)
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	key, err := gossh.MarshalPrivateKey(private, "")
	if err != nil {
		t.Fatal(err)
	}
	signer, err := gossh.NewSignerFromKey(private)
	if err != nil {
		t.Fatal(err)
	}
	var view *sshView
	views := make(chan *sshView, 1)
	server, err := wish.NewServer(wish.WithHostKeyPEM(pem.EncodeToMemory(key)), wish.WithPublicKeyAuth(func(ssh.Context, ssh.PublicKey) bool { return true }), wish.WithMiddleware(bubbletea.Middleware(func(s ssh.Session) (tea.Model, []tea.ProgramOption) {
		shopper, openErr := shoppers.Open(gossh.FingerprintSHA256(s.PublicKey()))
		if openErr != nil {
			t.Error(openErr)
			return tui.NewErrorModel("cannot restore"), nil
		}
		currentView := &sshView{}
		views <- currentView
		return observedModel{tui.NewStorefrontModel(s.Context(), catalog, shopper, true), currentView}, nil
	})))
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go server.Serve(listener)
	defer server.Close()
	client, err := gossh.Dial("tcp", listener.Addr().String(), &gossh.ClientConfig{User: "test", Auth: []gossh.AuthMethod{gossh.PublicKeys(signer)}, HostKeyCallback: gossh.InsecureIgnoreHostKey(), Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	connect := func() (*gossh.Session, io.WriteCloser) {
		t.Helper()
		session, connectErr := client.NewSession()
		if connectErr != nil {
			t.Fatal(connectErr)
		}
		if connectErr = session.RequestPty("xterm-256color", 24, 80, gossh.TerminalModes{}); connectErr != nil {
			t.Fatal(connectErr)
		}
		input, connectErr := session.StdinPipe()
		if connectErr != nil {
			t.Fatal(connectErr)
		}
		output, connectErr := session.StdoutPipe()
		if connectErr != nil {
			t.Fatal(connectErr)
		}
		go io.Copy(io.Discard, output)
		if connectErr = session.Shell(); connectErr != nil {
			t.Fatal(connectErr)
		}
		select {
		case view = <-views:
		case <-time.After(5 * time.Second):
			t.Fatal("SSH model did not start")
		}
		return session, input
	}
	session, input := connect()
	defer session.Close()
	send := func(keys string) {
		t.Helper()
		if _, writeErr := io.WriteString(input, keys); writeErr != nil {
			t.Fatal(writeErr)
		}
	}
	view.wait(t, tui.ViewProductList, "results")
	send("/Ethiopian")
	view.wait(t, tui.ViewProductList, "1 results")
	send("\r")
	view.wait(t, tui.ViewProductList, `search: "Ethiopian"`)
	view.wait(t, tui.ViewProductList, "Grind")
	send("\x1b[C")
	view.wait(t, tui.ViewProductList, "Espresso")
	send("\r")
	view.wait(t, tui.ViewCart, "Added to cart")
	view.wait(t, tui.ViewCart, "Ready for checkout")
	send("s")
	view.wait(t, tui.ViewProductList, "Espresso")
	send("/")
	view.wait(t, tui.ViewProductList, "Search:")
	send("\x1b")
	view.wait(t, tui.ViewProductList, "all coffee")
	send("/House Blend\r")
	view.wait(t, tui.ViewProductList, "250g")
	send("\x1b[C")
	view.wait(t, tui.ViewProductList, "1kg")
	send("\t\x1b[C+")
	view.wait(t, tui.ViewProductList, "Qty: < 2 >")
	view.wait(t, tui.ViewProductList, "Espresso")
	send("\r")
	view.wait(t, tui.ViewCart, "House Blend Signature (1kg)")
	view.wait(t, tui.ViewCart, "Ready for checkout")
	send("pCOFFEE10\r")
	view.wait(t, tui.ViewCart, "Coupon updated")
	send("?")
	view.wait(t, tui.ViewCart, "EVA · Help")
	send("\x1b")
	view.wait(t, tui.ViewCart, "Shopping cart")
	if err = session.WindowChange(18, 60); err != nil {
		t.Fatal(err)
	}
	view.wait(t, tui.ViewCart, "enter checkout")
	send("\r")
	view.wait(t, tui.ViewAddress, "First name *")
	fields := []struct{ value, next string }{{"Ada", "Last name *"}, {"Lovelace", "Email *"}, {"ada@example.com", "Street address *"}, {"Via Roma 1", "City *"}, {"Rome", "Country *"}, {"", "Postcode *"}, {"00100", "State / province code"}, {"RM", "Phone"}}
	for _, field := range fields {
		send(field.value + "\r")
		view.wait(t, tui.ViewAddress, field.next)
	}
	send("+39061234567\r")
	view.wait(t, tui.ViewShipping, "choose delivery")
	send("\x1b[B\r")
	view.wait(t, tui.ViewReview, "Ready · confirm")
	send("h")
	view.wait(t, tui.ViewShipping, "choose delivery")
	send("\r")
	view.wait(t, tui.ViewReview, "Ready · confirm")
	shopper, err := shoppers.Open(gossh.FingerprintSHA256(signer.PublicKey()))
	if err != nil {
		t.Fatal(err)
	}
	if err = shopper.Coupon(ctx, "COFFEE10", true); err != nil {
		t.Fatal(err)
	}
	total := shopper.Snapshot().Cart.Totals.TotalPrice
	view.wait(t, tui.ViewReview, "Total: EUR "+storeapi.FormatMinor(total, 2))
	send("\r")
	view.wait(t, tui.ViewReview, "quote changed")
	send("\r\r")
	view.wait(t, tui.ViewOrderConfirmation, "Order #")
	store.mu.Lock()
	attempts := len(store.attempts)
	store.mu.Unlock()
	if attempts != 1 {
		t.Fatalf("repeated SSH confirmation made %d attempts", attempts)
	}
	view.wait(t, tui.ViewOrderConfirmation, "Awaiting payment confirmation")
	send("z")
	time.Sleep(100 * time.Millisecond)
	if _, state := view.snapshot(); state != tui.ViewOrderConfirmation {
		t.Fatal("unrelated key closed payment over SSH")
	}
	send("y")
	view.wait(t, tui.ViewOrderConfirmation, "Copy requested")
	// Same verified key restores the active payment on reconnect.
	session.Close()
	session, input = connect()
	defer session.Close()
	view.wait(t, tui.ViewOrderConfirmation, "Awaiting payment confirmation")
	send("x")
	view.wait(t, tui.ViewOrderConfirmation, "Cancel this unpaid payment?")
	send("\x1b")
	view.wait(t, tui.ViewOrderConfirmation, "Awaiting payment confirmation")
	send("x\r")
	view.wait(t, tui.ViewOrderConfirmation, "Cancelled · cart available")
	send("c")
	view.wait(t, tui.ViewCart, "Ready for checkout")
	if err = session.WindowChange(12, 40); err != nil {
		t.Fatal(err)
	}
	view.wait(t, tui.ViewCart, "Resize to at least")
	if err = session.WindowChange(24, 80); err != nil {
		t.Fatal(err)
	}
	view.wait(t, tui.ViewCart, "enter checkout")
	send("s")
	view.wait(t, tui.ViewProductList, "results")
	send("q")
}
