// Package main implements the SSH server that serves the WooCommerce TUI.
package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/ssh"
	"github.com/charmbracelet/wish"
	"github.com/charmbracelet/wish/bubbletea"
	gossh "golang.org/x/crypto/ssh"

	"github.com/thomas/eva-terminal-go/internal/auth"
	"github.com/thomas/eva-terminal-go/internal/cache"
	"github.com/thomas/eva-terminal-go/internal/config"
	"github.com/thomas/eva-terminal-go/internal/tui"
	"github.com/thomas/eva-terminal-go/internal/woo"
)

func main() {
	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Ensure host key exists
	if err := ensureHostKey(cfg.SSHHostKeyPath); err != nil {
		log.Fatalf("Failed to ensure host key: %v", err)
	}

	// Load allowlist if in allowlist mode
	var allowlist []gossh.PublicKey
	if cfg.SSHAuthMode == config.AuthModeAllowlist {
		allowlist, err = auth.LoadAllowlist(cfg.AllowlistPath)
		if err != nil {
			if errors.Is(err, auth.ErrAllowlistNotFound) {
				log.Printf("Creating empty allowlist at %s", cfg.AllowlistPath)
				if err := auth.CreateEmptyAllowlist(cfg.AllowlistPath); err != nil {
					log.Fatalf("Failed to create allowlist: %v", err)
				}
				log.Printf("Please add your SSH public key to the allowlist and restart")
				os.Exit(1)
			}
			log.Fatalf("Failed to load allowlist: %v", err)
		}
		if len(allowlist) == 0 {
			log.Printf("WARNING: Allowlist is empty. No connections will be accepted.")
			log.Printf("Add your SSH public key to %s and restart", cfg.AllowlistPath)
		}
		log.Printf("Loaded %d public keys from allowlist", len(allowlist))
	} else {
		log.Printf("WARNING: Running in PUBLIC mode - anyone can connect!")
		log.Printf("This is NOT safe for internet-facing servers.")
	}

	// Create WooCommerce client
	clientOpts := []woo.ClientOption{}
	if cfg.WooConsumerKey != "" && cfg.WooConsumerSecret != "" {
		clientOpts = append(clientOpts, woo.WithCredentials(cfg.WooConsumerKey, cfg.WooConsumerSecret))
	}
	wooClient := woo.NewClient(cfg.WooBaseURL, clientOpts...)

	// Create caches
	productsCache := cache.New[tui.ProductListCacheKey, []woo.Product](cfg.CacheTTL)
	variationsCache := cache.New[int, []woo.Variation](cfg.CacheTTL)

	// Create SSH server options
	opts := []ssh.Option{
		wish.WithAddress(cfg.SSHAddr),
		wish.WithHostKeyPath(cfg.SSHHostKeyPath),
		wish.WithMiddleware(
			bubbletea.Middleware(func(s ssh.Session) (tea.Model, []tea.ProgramOption) {
				return tui.NewModel(wooClient, productsCache, variationsCache), []tea.ProgramOption{tea.WithAltScreen()}
			}),
		),
	}

	// Add authentication based on mode
	if cfg.SSHAuthMode == config.AuthModeAllowlist {
		opts = append(opts, wish.WithPublicKeyAuth(func(ctx ssh.Context, key ssh.PublicKey) bool {
			return auth.IsKeyAllowed(key, allowlist)
		}))
	} else {
		// Public mode - accept any public key
		opts = append(opts, wish.WithPublicKeyAuth(func(ctx ssh.Context, key ssh.PublicKey) bool {
			return true
		}))
	}

	// Always disable password auth
	opts = append(opts, wish.WithPasswordAuth(func(ctx ssh.Context, password string) bool {
		return false
	}))

	// Create SSH server
	server, err := wish.NewServer(opts...)
	if err != nil {
		log.Fatalf("Failed to create SSH server: %v", err)
	}

	// Handle shutdown
	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)

	log.Printf("Starting SSH server on %s", cfg.SSHAddr)
	log.Printf("WooCommerce API: %s", cfg.WooBaseURL)
	log.Printf("Auth mode: %s", cfg.SSHAuthMode)
	log.Printf("Connect with: ssh -p %s localhost", cfg.SSHAddr[1:])

	go func() {
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, ssh.ErrServerClosed) {
			log.Fatalf("Server error: %v", err)
		}
	}()

	<-done
	log.Println("Shutting down...")

	ctx, cancel := context.WithTimeout(context.Background(), 5)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		log.Fatalf("Shutdown error: %v", err)
	}
}

// ensureHostKey generates an ED25519 host key if it doesn't exist.
func ensureHostKey(path string) error {
	// Check if key exists
	if _, err := os.Stat(path); err == nil {
		return nil // Key exists
	}

	log.Printf("Generating new ED25519 host key at %s", path)

	// Generate ED25519 key pair
	pubKey, privKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return fmt.Errorf("generating key: %w", err)
	}

	// Convert to OpenSSH format
	sshPrivKey, err := gossh.MarshalPrivateKey(privKey, "")
	if err != nil {
		return fmt.Errorf("marshaling private key: %w", err)
	}

	// Write private key
	if err := os.WriteFile(path, pem.EncodeToMemory(sshPrivKey), 0600); err != nil {
		return fmt.Errorf("writing private key: %w", err)
	}

	// Write public key
	sshPubKey, err := gossh.NewPublicKey(pubKey)
	if err != nil {
		return fmt.Errorf("creating public key: %w", err)
	}

	pubKeyBytes := gossh.MarshalAuthorizedKey(sshPubKey)
	if err := os.WriteFile(path+".pub", pubKeyBytes, 0644); err != nil {
		return fmt.Errorf("writing public key: %w", err)
	}

	return nil
}

// Dummy usage to prevent import errors
var _ = list.Model{}



