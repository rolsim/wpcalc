package apiv1

import (
	"context"
	"errors"

	"github.com/rolsim/wpcalc/internal/auth"
	"github.com/rolsim/wpcalc/internal/domain"
	"github.com/rolsim/wpcalc/internal/store"
)

func toAPIToken(t domain.APIToken) ApiToken {
	return ApiToken{
		Id:         t.ID,
		Name:       t.Name,
		CreatedAt:  t.CreatedAt,
		ExpiresAt:  t.ExpiresAt,
		LastUsedAt: t.LastUsedAt,
		RevokedAt:  t.RevokedAt,
	}
}

// ListTokens, CreateToken, RevokeToken, and RevokeAllTokens are
// self-service, scoped to the calling token's own account — there is no
// "list/revoke another account's tokens" here, unlike the CLI, which has
// direct database access and is not scoped this way.
func (a *API) ListTokens(ctx context.Context, _ ListTokensRequestObject) (ListTokensResponseObject, error) {
	id, ok := auth.IdentityFrom(ctx)
	if !ok {
		return ListTokensdefaultJSONResponse{Body: Error{Error: codeUnauthorized}, StatusCode: 401}, nil
	}
	tokens, err := a.db.APITokens(ctx, id.UserID)
	if err != nil {
		status, code := mapStoreErr(err)
		return ListTokensdefaultJSONResponse{Body: Error{Error: code}, StatusCode: status}, nil
	}
	out := make([]ApiToken, 0, len(tokens))
	for _, t := range tokens {
		out = append(out, toAPIToken(t))
	}
	return ListTokens200JSONResponse(out), nil
}

// CreateToken mints an access/refresh pair together — the two are always
// issued and rotated as a unit, sharing one name, so a user never ends up
// with an access token that has no way to renew itself.
func (a *API) CreateToken(ctx context.Context, request CreateTokenRequestObject) (CreateTokenResponseObject, error) {
	id, ok := auth.IdentityFrom(ctx)
	if !ok {
		return CreateTokendefaultJSONResponse{Body: Error{Error: codeUnauthorized}, StatusCode: 401}, nil
	}
	if request.Body == nil {
		return CreateTokendefaultJSONResponse{Body: Error{Error: codeBadRequest}, StatusCode: 400}, nil
	}
	pair, err := a.issueTokenPair(ctx, id.UserID, request.Body.Name)
	if err != nil {
		status, code := mapStoreErr(err)
		return CreateTokendefaultJSONResponse{Body: Error{Error: code}, StatusCode: status}, nil
	}
	return CreateToken201JSONResponse(pair), nil
}

func (a *API) issueTokenPair(ctx context.Context, userID int64, name string) (TokenPair, error) {
	accessToken, accessID, accessExpiry, err := a.db.CreateAPIToken(ctx, userID, name)
	if err != nil {
		return TokenPair{}, err
	}
	refreshToken, _, refreshExpiry, err := a.db.CreateRefreshToken(ctx, userID, name)
	if err != nil {
		return TokenPair{}, err
	}
	return TokenPair{
		AccessTokenId:         accessID,
		AccessToken:           accessToken,
		AccessTokenExpiresAt:  accessExpiry,
		RefreshToken:          refreshToken,
		RefreshTokenExpiresAt: refreshExpiry,
		Name:                  name,
	}, nil
}

// RevokeToken checks ownership itself (list the caller's own tokens, look
// for the id) rather than trusting RevokeAPIToken to do it — that store
// method is also used by the CLI, which is intentionally not scoped to one
// account, so the ownership boundary belongs here, at the API layer.
func (a *API) RevokeToken(ctx context.Context, request RevokeTokenRequestObject) (RevokeTokenResponseObject, error) {
	id, ok := auth.IdentityFrom(ctx)
	if !ok {
		return RevokeTokendefaultJSONResponse{Body: Error{Error: codeUnauthorized}, StatusCode: 401}, nil
	}
	tokens, err := a.db.APITokens(ctx, id.UserID)
	if err != nil {
		status, code := mapStoreErr(err)
		return RevokeTokendefaultJSONResponse{Body: Error{Error: code}, StatusCode: status}, nil
	}
	owned := false
	for _, t := range tokens {
		if t.ID == request.TokenId {
			owned = true
			break
		}
	}
	if !owned {
		return RevokeTokendefaultJSONResponse{Body: Error{Error: codeNotFound}, StatusCode: 404}, nil
	}
	if err := a.db.RevokeAPIToken(ctx, request.TokenId); err != nil {
		status, code := mapStoreErr(err)
		return RevokeTokendefaultJSONResponse{Body: Error{Error: code}, StatusCode: status}, nil
	}
	return RevokeToken204Response{}, nil
}

func (a *API) RevokeAllTokens(ctx context.Context, _ RevokeAllTokensRequestObject) (RevokeAllTokensResponseObject, error) {
	id, ok := auth.IdentityFrom(ctx)
	if !ok {
		return RevokeAllTokensdefaultJSONResponse{Body: Error{Error: codeUnauthorized}, StatusCode: 401}, nil
	}
	if err := a.db.RevokeAllUserTokens(ctx, id.UserID); err != nil {
		status, code := mapStoreErr(err)
		return RevokeAllTokensdefaultJSONResponse{Body: Error{Error: code}, StatusCode: status}, nil
	}
	return RevokeAllTokens204Response{}, nil
}

// RefreshToken is public (no bearer credential required to call it — see
// the spec's security: [] override) precisely so it's reachable once an
// access token has already expired; the refresh token itself, presented
// in the body, is the credential here. An invalid, expired, revoked, or
// already-used one is deliberately reported identically (401,
// "unauthenticated") — the store layer distinguishes "used" from
// "not found" (store.ErrRefreshTokenUsed) for its own callers/logging, but
// that distinction is not surfaced here, so a response can never be used
// to probe whether a given secret was ever valid.
func (a *API) RefreshToken(ctx context.Context, request RefreshTokenRequestObject) (RefreshTokenResponseObject, error) {
	if request.Body == nil {
		return RefreshTokendefaultJSONResponse{Body: Error{Error: codeBadRequest}, StatusCode: 400}, nil
	}
	exchange, err := a.db.ExchangeRefreshToken(ctx, request.Body.RefreshToken)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) || errors.Is(err, store.ErrRefreshTokenUsed) {
			return RefreshTokendefaultJSONResponse{Body: Error{Error: codeUnauthorized}, StatusCode: 401}, nil
		}
		status, code := mapStoreErr(err)
		return RefreshTokendefaultJSONResponse{Body: Error{Error: code}, StatusCode: status}, nil
	}
	return RefreshToken201JSONResponse{
		AccessTokenId:         exchange.AccessTokenID,
		AccessToken:           exchange.AccessToken,
		AccessTokenExpiresAt:  exchange.AccessTokenExpiresAt,
		RefreshToken:          exchange.RefreshToken,
		RefreshTokenExpiresAt: exchange.RefreshTokenExpiresAt,
		Name:                  exchange.Name,
	}, nil
}
