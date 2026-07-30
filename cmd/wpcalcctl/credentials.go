package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	wpcalc "github.com/rolsim/wpcalc/sdk/go"
)

// Credentials is everything a command needs to talk to a server: where it
// is, and the token pair `wpcalcctl login` obtained (originally from
// `wpcalc token create` on the server, since that is the one thing this
// tool cannot bootstrap on its own).
type Credentials struct {
	Server string           `json:"server"`
	Tokens wpcalc.TokenPair `json:"tokens"`
}

// credentialsPath is overridable via WPCALCCTL_CREDENTIALS for tests and
// for anyone running against more than one server who wants separate
// credential files.
func credentialsPath() (string, error) {
	if p := os.Getenv("WPCALCCTL_CREDENTIALS"); p != "" {
		return p, nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("locate config directory: %w", err)
	}
	return filepath.Join(dir, "wpcalcctl", "credentials.json"), nil
}

func loadCredentials() (Credentials, error) {
	path, err := credentialsPath()
	if err != nil {
		return Credentials{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Credentials{}, fmt.Errorf("not logged in — run `wpcalcctl login` first (looked in %s)", path)
		}
		return Credentials{}, fmt.Errorf("read credentials: %w", err)
	}
	var c Credentials
	if err := json.Unmarshal(data, &c); err != nil {
		return Credentials{}, fmt.Errorf("parse credentials at %s: %w", path, err)
	}
	return c, nil
}

// saveCredentials writes with mode 0600: this file holds a live bearer
// secret, the same trust level as an SSH private key.
func saveCredentials(c Credentials) error {
	path, err := credentialsPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("encode credentials: %w", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write credentials: %w", err)
	}
	return nil
}
