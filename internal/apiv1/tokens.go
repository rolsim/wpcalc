package apiv1

import (
	"context"

	"github.com/rolsim/wpcalc/internal/auth"
	"github.com/rolsim/wpcalc/internal/domain"
)

func toAPIToken(t domain.APIToken) ApiToken {
	return ApiToken{
		Id:         t.ID,
		Name:       t.Name,
		CreatedAt:  t.CreatedAt,
		LastUsedAt: t.LastUsedAt,
		RevokedAt:  t.RevokedAt,
	}
}

// ListTokens, CreateToken, and RevokeToken are self-service, scoped to the
// calling token's own account — there is no "list/revoke another
// account's tokens" here, unlike the CLI, which has direct database
// access and is not scoped this way.
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

func (a *API) CreateToken(ctx context.Context, request CreateTokenRequestObject) (CreateTokenResponseObject, error) {
	id, ok := auth.IdentityFrom(ctx)
	if !ok {
		return CreateTokendefaultJSONResponse{Body: Error{Error: codeUnauthorized}, StatusCode: 401}, nil
	}
	if request.Body == nil {
		return CreateTokendefaultJSONResponse{Body: Error{Error: codeBadRequest}, StatusCode: 400}, nil
	}
	token, tokenID, err := a.db.CreateAPIToken(ctx, id.UserID, request.Body.Name)
	if err != nil {
		status, code := mapStoreErr(err)
		return CreateTokendefaultJSONResponse{Body: Error{Error: code}, StatusCode: status}, nil
	}
	return CreateToken201JSONResponse{Id: tokenID, Name: request.Body.Name, Token: token}, nil
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
