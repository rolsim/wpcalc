package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"strconv"
	"time"

	wpcalc "github.com/rolsim/wpcalc/sdk/go"
)

// cmdToken manages the logged-in account's own tokens via /api/v1's
// self-service endpoints — scoped to that one account, unlike the
// server's own `wpcalc token create`, which can mint a pair for any
// account and needs no existing credential at all.
func cmdToken(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("token", flag.ContinueOnError)
	name := fs.String("name", "", "a label for the new token pair (token create)")
	positional, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(positional) == 0 {
		return errors.New("token: want one of create, list, revoke, revoke-all")
	}
	arg := func(i int) string {
		if i < len(positional) {
			return positional[i]
		}
		return ""
	}

	sess, err := newSession()
	if err != nil {
		return err
	}

	switch action := arg(0); action {
	case "create":
		return tokenCreate(ctx, sess, *name)
	case "list":
		return tokenList(ctx, sess)
	case "revoke":
		return tokenRevoke(ctx, sess, arg(1))
	case "revoke-all":
		return tokenRevokeAll(ctx, sess)
	default:
		return fmt.Errorf("token: unknown action %q (want create, list, revoke, or revoke-all)", action)
	}
}

func tokenCreate(ctx context.Context, sess *wpcalc.Session, name string) error {
	if name == "" {
		return errors.New("token create: -name is required")
	}
	resp, err := sess.CreateTokenWithResponse(ctx, wpcalc.CreateTokenJSONRequestBody{Name: name})
	if err != nil {
		return fmt.Errorf("token create: %w", err)
	}
	if resp.JSON201 == nil {
		return apiError("token create", resp.StatusCode(), resp.Body, resp.JSONDefault)
	}
	p := resp.JSON201
	fmt.Printf(`token %d created (%s)

access token (expires %s):
  %s

refresh token (expires %s, single-use):
  %s

Both are shown once — store them now, e.g. for use with 'wpcalcctl login'
on another machine.
`,
		p.AccessTokenId, p.Name,
		p.AccessTokenExpiresAt.Format(time.RFC3339), p.AccessToken,
		p.RefreshTokenExpiresAt.Format(time.RFC3339), p.RefreshToken)
	return nil
}

func tokenList(ctx context.Context, sess *wpcalc.Session) error {
	resp, err := sess.ListTokensWithResponse(ctx)
	if err != nil {
		return fmt.Errorf("token list: %w", err)
	}
	if resp.JSON200 == nil {
		return apiError("token list", resp.StatusCode(), resp.Body, resp.JSONDefault)
	}
	if len(*resp.JSON200) == 0 {
		fmt.Println("no tokens — create one with `wpcalcctl token create -name N`")
		return nil
	}
	now := time.Now()
	for _, t := range *resp.JSON200 {
		status := "active"
		switch {
		case t.RevokedAt != nil:
			status = "revoked " + t.RevokedAt.Format("2006-01-02")
		case now.After(t.ExpiresAt):
			status = "expired " + t.ExpiresAt.Format("2006-01-02 15:04")
		}
		lastUsed := "never used"
		if t.LastUsedAt != nil {
			lastUsed = "last used " + t.LastUsedAt.Format("2006-01-02")
		}
		fmt.Printf("%-4d %-20s created %s, %s, %s\n",
			t.Id, t.Name, t.CreatedAt.Format("2006-01-02"), lastUsed, status)
	}
	return nil
}

func tokenRevoke(ctx context.Context, sess *wpcalc.Session, idArg string) error {
	id, err := strconv.ParseInt(idArg, 10, 64)
	if err != nil {
		return fmt.Errorf("token revoke: %q is not a valid token id", idArg)
	}
	resp, err := sess.RevokeTokenWithResponse(ctx, id)
	if err != nil {
		return fmt.Errorf("token revoke: %w", err)
	}
	if resp.StatusCode() != 204 {
		return apiError("token revoke", resp.StatusCode(), resp.Body, resp.JSONDefault)
	}
	fmt.Printf("token %d revoked\n", id)
	return nil
}

func tokenRevokeAll(ctx context.Context, sess *wpcalc.Session) error {
	resp, err := sess.RevokeAllTokensWithResponse(ctx)
	if err != nil {
		return fmt.Errorf("token revoke-all: %w", err)
	}
	if resp.StatusCode() != 204 {
		return apiError("token revoke-all", resp.StatusCode(), resp.Body, resp.JSONDefault)
	}
	fmt.Println("every token for this account revoked — including the one used to make this call")
	return nil
}
