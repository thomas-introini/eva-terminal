// Package auth handles SSH public key authentication via allowlist.
package auth

import (
	"bufio"
	"bytes"
	"errors"
	"os"
	"strings"

	"golang.org/x/crypto/ssh"
)

// ErrAllowlistNotFound is returned when the allowlist file doesn't exist.
var ErrAllowlistNotFound = errors.New("allowlist file not found")

// LoadAllowlist reads an OpenSSH authorized_keys format file and returns
// the parsed public keys. It skips empty lines and comments.
func LoadAllowlist(path string) ([]ssh.PublicKey, error) {
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrAllowlistNotFound
		}
		return nil, err
	}
	defer file.Close()

	var keys []ssh.PublicKey
	scanner := bufio.NewScanner(file)
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())

		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Parse the public key (authorized_keys format)
		pubKey, _, _, _, err := ssh.ParseAuthorizedKey([]byte(line))
		if err != nil {
			// Skip invalid lines but continue processing
			continue
		}

		keys = append(keys, pubKey)
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return keys, nil
}

// IsKeyAllowed checks if the given public key is in the allowlist.
// It compares the marshaled key bytes for equality.
func IsKeyAllowed(key ssh.PublicKey, allowlist []ssh.PublicKey) bool {
	if key == nil {
		return false
	}

	keyBytes := key.Marshal()
	for _, allowed := range allowlist {
		if bytes.Equal(keyBytes, allowed.Marshal()) {
			return true
		}
	}
	return false
}

// CreateEmptyAllowlist creates an empty allowlist file with a helpful comment.
func CreateEmptyAllowlist(path string) error {
	content := `# SSH Public Key Allowlist
# Add one public key per line in OpenSSH authorized_keys format.
# Example:
# ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIExample... user@host
`
	return os.WriteFile(path, []byte(content), 0644)
}



