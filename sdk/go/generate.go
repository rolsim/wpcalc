// Package wpcalc is a typed Go client for wpcalc's /api/v1 JSON API,
// generated from the same internal/apiv1/openapi.yaml the server itself
// generates internal/apiv1/server.gen.go from — the client can never
// describe an operation, field, or shape the server doesn't actually
// implement, because both come from one source of truth.
//
// client.gen.go is generated — do not hand-edit it. Everything else in
// this package is the hand-written layer on top: constructing a Client,
// carrying a bearer token, and transparently refreshing it when it
// expires (see client.go).
package wpcalc

//go:generate go tool oapi-codegen -config oapi-codegen.yaml ../../internal/apiv1/openapi.yaml
