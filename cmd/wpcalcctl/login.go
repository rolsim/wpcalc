package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"strings"

	wpcalc "github.com/rolsim/wpcalc/sdk/go"
)

func cmdLogin(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("login", flag.ContinueOnError)
	server := fs.String("server", "", "server base URL, including /api/v1, e.g. http://localhost:8080/api/v1")
	accessToken := fs.String("access-token", "", "access token from `wpcalc token create` (wpat_...)")
	refreshToken := fs.String("refresh-token", "", "refresh token from `wpcalc token create` (wprt_...)")
	if _, err := parseArgs(fs, args); err != nil {
		return err
	}
	if *server == "" {
		return errors.New("login: --server is required")
	}
	if *accessToken == "" || *refreshToken == "" {
		return errors.New("login: --access-token and --refresh-token are required — get a pair with `wpcalc token create <name>` on the server")
	}

	creds := Credentials{
		Server: strings.TrimRight(*server, "/"),
		Tokens: wpcalc.TokenPair{AccessToken: *accessToken, RefreshToken: *refreshToken},
	}

	// Validate immediately rather than saving a credential that turns out
	// not to work — a session needs no particular permission to call
	// this, just a valid token, so it's a clean "does this even work" check.
	sess, err := wpcalc.New(creds.Server, creds.Tokens)
	if err != nil {
		return fmt.Errorf("login: %w", err)
	}
	resp, err := sess.ListAccessibleTenantsWithResponse(ctx)
	if err != nil {
		return fmt.Errorf("login: could not reach %s: %w", creds.Server, err)
	}
	if resp.JSON200 == nil {
		return apiError("login", resp.StatusCode(), resp.Body, resp.JSONDefault)
	}
	// Persist whatever pair is current now — Session may have already
	// rotated once during that validation call if the access token given
	// was already stale.
	creds.Tokens = sess.Tokens()

	if err := saveCredentials(creds); err != nil {
		return fmt.Errorf("login: %w", err)
	}
	path, _ := credentialsPath()
	fmt.Printf("logged in to %s (%d tenant(s) reachable) — credentials saved to %s\n",
		creds.Server, len(*resp.JSON200), path)
	return nil
}
