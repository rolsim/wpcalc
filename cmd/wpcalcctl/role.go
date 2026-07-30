package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"strings"

	wpcalc "github.com/rolsim/wpcalc/sdk/go"
)

func cmdRole(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("role", flag.ContinueOnError)
	name := fs.String("name", "", "role display name (role add)")
	scope := fs.String("scope", "", "role scope: system, tenant, or employee (role add)")
	add := fs.String("add", "", "permission id to grant (role permissions)")
	remove := fs.String("remove", "", "permission id to revoke (role permissions)")
	positional, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(positional) == 0 {
		return errors.New("role: want one of add, list, delete, permissions")
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
		return roleAdd(ctx, sess, arg(1), *name, *scope)
	case "list":
		return roleList(ctx, sess)
	case "delete":
		return roleDelete(ctx, sess, arg(1))
	case "permissions":
		return rolePermissions(ctx, sess, arg(1), *add, *remove)
	default:
		return fmt.Errorf("role: unknown action %q (want add, list, delete, or permissions)", action)
	}
}

func roleAdd(ctx context.Context, sess *wpcalc.Session, id, name, scope string) error {
	if id == "" {
		return errors.New("role add: id is required")
	}
	if name == "" {
		return errors.New("role add: -name is required")
	}
	if scope == "" {
		return errors.New("role add: -scope is required (system, tenant, or employee)")
	}
	resp, err := sess.CreateRoleWithResponse(ctx, wpcalc.CreateRoleJSONRequestBody{
		Id: id, Name: name, Scope: wpcalc.Scope(scope),
	})
	if err != nil {
		return fmt.Errorf("role add: %w", err)
	}
	if resp.JSON201 == nil {
		return apiError("role add", resp.StatusCode(), resp.Body, resp.JSONDefault)
	}
	fmt.Printf("created role %s (%s, %s)\n", resp.JSON201.Id, resp.JSON201.Name, resp.JSON201.Scope)
	return nil
}

func roleList(ctx context.Context, sess *wpcalc.Session) error {
	resp, err := sess.ListRolesWithResponse(ctx)
	if err != nil {
		return fmt.Errorf("role list: %w", err)
	}
	if resp.JSON200 == nil {
		return apiError("role list", resp.StatusCode(), resp.Body, resp.JSONDefault)
	}
	for _, r := range *resp.JSON200 {
		perms := make([]string, 0, len(r.Permissions))
		for _, p := range r.Permissions {
			perms = append(perms, string(p))
		}
		fmt.Printf("%-16s %-8s %-20s %s\n", r.Id, r.Scope, r.Name, strings.Join(perms, ","))
	}
	return nil
}

func roleDelete(ctx context.Context, sess *wpcalc.Session, id string) error {
	if id == "" {
		return errors.New("role delete: id is required")
	}
	resp, err := sess.DeleteRoleWithResponse(ctx, id)
	if err != nil {
		return fmt.Errorf("role delete: %w", err)
	}
	if resp.StatusCode() != 204 {
		return apiError("role delete", resp.StatusCode(), resp.Body, resp.JSONDefault)
	}
	fmt.Printf("role %s deleted\n", id)
	return nil
}

func rolePermissions(ctx context.Context, sess *wpcalc.Session, id, add, remove string) error {
	if id == "" {
		return errors.New("role permissions: id is required")
	}
	switch {
	case add != "" && remove != "":
		return errors.New("role permissions: -add and -remove are mutually exclusive")
	case add != "":
		resp, err := sess.AddRolePermissionWithResponse(ctx, id, wpcalc.AddRolePermissionJSONRequestBody{
			PermissionId: wpcalc.PermissionKey(add),
		})
		if err != nil {
			return fmt.Errorf("role permissions: %w", err)
		}
		if resp.StatusCode() != 204 {
			return apiError("role permissions -add", resp.StatusCode(), resp.Body, resp.JSONDefault)
		}
		fmt.Printf("%s granted %s\n", id, add)
		return nil
	case remove != "":
		resp, err := sess.RemoveRolePermissionWithResponse(ctx, id, wpcalc.PermissionKey(remove))
		if err != nil {
			return fmt.Errorf("role permissions: %w", err)
		}
		if resp.StatusCode() != 204 {
			return apiError("role permissions -remove", resp.StatusCode(), resp.Body, resp.JSONDefault)
		}
		fmt.Printf("%s revoked from %s\n", remove, id)
		return nil
	default:
		return errors.New("role permissions: one of -add or -remove is required")
	}
}

func cmdPermission(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("permission", flag.ContinueOnError)
	positional, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(positional) == 0 || positional[0] != "list" {
		return errors.New("permission: want list")
	}

	sess, err := newSession()
	if err != nil {
		return err
	}
	resp, err := sess.ListPermissionsWithResponse(ctx)
	if err != nil {
		return fmt.Errorf("permission list: %w", err)
	}
	if resp.JSON200 == nil {
		return apiError("permission list", resp.StatusCode(), resp.Body, resp.JSONDefault)
	}
	for _, p := range *resp.JSON200 {
		fmt.Printf("%-18s min scope: %s\n", p.Id, p.MinScope)
	}
	return nil
}
