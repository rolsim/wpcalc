package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"source.simonet.internal/rolsim/wpcalc/internal/auth"
	"source.simonet.internal/rolsim/wpcalc/internal/httpx"
	"source.simonet.internal/rolsim/wpcalc/internal/i18n"
	"source.simonet.internal/rolsim/wpcalc/internal/store"
)

func cmdServe(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	addr := fs.String("addr", "", "TCP address to listen on, e.g. :8080")
	socket := fs.String("socket", "", "unix socket path to listen on (WordPress sidecar mode)")
	dbPath := fs.String("db", defaultDBPath(), "path to the SQLite database")
	basePath := fs.String("base-path", os.Getenv("WPCALC_BASE_PATH"), "URL prefix the app is mounted under")
	secureCookies := fs.Bool("secure-cookies", false, "mark session cookies Secure (requires HTTPS)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	// Exactly one listener. Defaulting to TCP when both are empty would be a
	// trap: the WordPress shim passes --socket, and silently opening a port
	// instead would expose the app on a host that never meant to publish it.
	switch {
	case *addr == "" && *socket == "":
		return errors.New("serve: one of --addr or --socket is required")
	case *addr != "" && *socket != "":
		return errors.New("serve: --addr and --socket are mutually exclusive")
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	db, err := store.Open(ctx, *dbPath)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	bundle, err := i18n.New()
	if err != nil {
		return err
	}

	authn, connKind, err := buildAuthenticator(*socket != "", *secureCookies)
	if err != nil {
		return err
	}

	srv, err := httpx.New(httpx.Config{
		DB:       db,
		Bundle:   bundle,
		Auth:     authn,
		Logger:   logger,
		ConnKind: connKind,
		BasePath: *basePath,
	})
	if err != nil {
		return err
	}

	ln, err := listen(*addr, *socket)
	if err != nil {
		return err
	}
	defer func() { _ = ln.Close() }()

	httpSrv := &http.Server{
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	logger.Info("serving",
		"listener", ln.Addr().String(),
		"mode", string(connKind),
		"db", db.Path())

	errCh := make(chan error, 1)
	go func() {
		err := httpSrv.Serve(ln)
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		errCh <- err
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		logger.Info("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		if err := httpSrv.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("serve: shutdown: %w", err)
		}
		return <-errCh
	}
}

// buildAuthenticator picks the identity source from the listener kind.
//
// The two modes are mutually exclusive on purpose. Offering the standalone
// login form while running behind WordPress would be a second, weaker door
// into the same data, bypassing whatever access control the site already has.
func buildAuthenticator(isSocket, secureCookies bool) (auth.Authenticator, auth.ConnKind, error) {
	if isSocket {
		secret := os.Getenv("WPCALC_SECRET")
		a, err := auth.NewWordPress(secret)
		if err != nil {
			return nil, "", fmt.Errorf("serve: socket mode needs WPCALC_SECRET: %w", err)
		}
		return a, auth.ConnUnix, nil
	}

	a, err := auth.NewPassword(os.Getenv("WPCALC_PASSWORD"))
	if err != nil {
		return nil, "", fmt.Errorf("serve: %w", err)
	}
	a.SetSecureCookies(secureCookies)
	return a, auth.ConnTCP, nil
}

// listen binds the requested listener.
func listen(addr, socket string) (net.Listener, error) {
	if addr != "" {
		ln, err := net.Listen("tcp", addr)
		if err != nil {
			return nil, fmt.Errorf("serve: listen on %s: %w", addr, err)
		}
		return ln, nil
	}

	if err := os.MkdirAll(filepath.Dir(socket), 0o755); err != nil {
		return nil, fmt.Errorf("serve: create socket directory: %w", err)
	}

	// A socket left behind by a killed process blocks the bind. Removing it is
	// safe here because two sidecars sharing one socket path is already broken;
	// the plugin serialises startup with a PID file.
	if err := os.Remove(socket); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("serve: remove stale socket: %w", err)
	}

	ln, err := net.Listen("unix", socket)
	if err != nil {
		return nil, fmt.Errorf("serve: listen on %s: %w", socket, err)
	}

	// The socket is the trust boundary for header-asserted identity, so only
	// the web server user may talk to it. World-writable here would undo the
	// unix-socket condition the WordPress authenticator relies on.
	if err := os.Chmod(socket, 0o660); err != nil {
		_ = ln.Close()
		return nil, fmt.Errorf("serve: secure socket: %w", err)
	}
	return ln, nil
}
