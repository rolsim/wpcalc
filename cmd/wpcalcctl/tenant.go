package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"strconv"

	wpcalc "github.com/rolsim/wpcalc/sdk/go"
)

func cmdTenant(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("tenant", flag.ContinueOnError)
	positional, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(positional) == 0 {
		return errors.New("tenant: want one of add, list, rename")
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
	case "add":
		return tenantAdd(ctx, sess, arg(1))
	case "list":
		return tenantList(ctx, sess)
	case "rename":
		return tenantRename(ctx, sess, arg(1), arg(2))
	default:
		return fmt.Errorf("tenant: unknown action %q (want add, list, or rename)", action)
	}
}

func tenantAdd(ctx context.Context, sess *wpcalc.Session, name string) error {
	if name == "" {
		return errors.New("tenant add: name is required")
	}
	resp, err := sess.CreateTenantWithResponse(ctx, wpcalc.CreateTenantJSONRequestBody{Name: name})
	if err != nil {
		return fmt.Errorf("tenant add: %w", err)
	}
	if resp.JSON201 == nil {
		return apiError("tenant add", resp.StatusCode(), resp.Body, resp.JSONDefault)
	}
	fmt.Printf("created tenant %d: %s\n", resp.JSON201.Id, resp.JSON201.Name)
	return nil
}

func tenantList(ctx context.Context, sess *wpcalc.Session) error {
	resp, err := sess.ListTenantsWithResponse(ctx)
	if err != nil {
		return fmt.Errorf("tenant list: %w", err)
	}
	if resp.JSON200 == nil {
		return apiError("tenant list", resp.StatusCode(), resp.Body, resp.JSONDefault)
	}
	if len(*resp.JSON200) == 0 {
		fmt.Println("no tenants yet — create one with `wpcalcctl tenant add <name>`")
		return nil
	}
	for _, t := range *resp.JSON200 {
		fmt.Printf("%-4d %s\n", t.Id, t.Name)
	}
	return nil
}

func tenantRename(ctx context.Context, sess *wpcalc.Session, idArg, name string) error {
	id, err := strconv.ParseInt(idArg, 10, 64)
	if err != nil {
		return fmt.Errorf("tenant rename: %q is not a valid tenant id", idArg)
	}
	if name == "" {
		return errors.New("tenant rename: name is required")
	}
	resp, err := sess.UpdateTenantWithResponse(ctx, id, wpcalc.UpdateTenantJSONRequestBody{Name: name})
	if err != nil {
		return fmt.Errorf("tenant rename: %w", err)
	}
	if resp.JSON200 == nil {
		return apiError("tenant rename", resp.StatusCode(), resp.Body, resp.JSONDefault)
	}
	fmt.Printf("tenant %d renamed to %s\n", resp.JSON200.Id, resp.JSON200.Name)
	return nil
}
