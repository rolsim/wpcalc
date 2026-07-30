package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"strings"

	"github.com/rolsim/wpcalc/internal/domain"
	"github.com/rolsim/wpcalc/internal/store"
)

func cmdRole(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("role", flag.ContinueOnError)
	dbPath := fs.String("db", defaultDBPath(), "path to the SQLite database")
	name := fs.String("name", "", "display name for a new role (role add)")
	scope := fs.String("scope", "", "system, tenant, or employee (role add)")
	add := fs.String("add", "", "permission id to grant this role (role permissions)")
	remove := fs.String("remove", "", "permission id to revoke from this role (role permissions)")
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

	db, err := store.Open(ctx, *dbPath)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	switch action := arg(0); action {
	case "add":
		return roleAdd(ctx, db, arg(1), *name, *scope)
	case "list":
		return roleList(ctx, db)
	case "delete":
		return roleDelete(ctx, db, arg(1))
	case "permissions":
		return rolePermissions(ctx, db, arg(1), *add, *remove)
	default:
		return fmt.Errorf("role: unknown action %q (want add, list, delete, or permissions)", action)
	}
}

func roleAdd(ctx context.Context, db *store.DB, id, name, scope string) error {
	if id == "" {
		return errors.New("role add: id is required")
	}
	if name == "" {
		return errors.New("role add: -name is required")
	}
	if err := db.CreateRole(ctx, id, name, domain.Scope(scope)); err != nil {
		return err
	}
	fmt.Printf("created role %s (%s, scope: %s)\n", id, name, scope)
	return nil
}

func roleList(ctx context.Context, db *store.DB) error {
	roles, err := db.Roles(ctx)
	if err != nil {
		return err
	}
	if len(roles) == 0 {
		fmt.Println("no roles — this should not happen; migration 00004 seeds five")
		return nil
	}
	roleIDs := make([]string, len(roles))
	for i, ro := range roles {
		roleIDs[i] = ro.ID
	}
	perms, err := db.RolePermissionsFor(ctx, roleIDs)
	if err != nil {
		return err
	}
	for _, ro := range roles {
		fmt.Printf("%-16s %-16s %-10s %s\n", ro.ID, ro.Name, ro.Scope, strings.Join(perms[ro.ID], ","))
	}
	return nil
}

func roleDelete(ctx context.Context, db *store.DB, id string) error {
	if id == "" {
		return errors.New("role delete: id is required")
	}
	if err := db.DeleteRole(ctx, id); err != nil {
		return err
	}
	fmt.Printf("deleted role %s\n", id)
	return nil
}

func rolePermissions(ctx context.Context, db *store.DB, roleID, add, remove string) error {
	if roleID == "" {
		return errors.New("role permissions: role id is required")
	}
	switch {
	case add != "" && remove != "":
		return errors.New("role permissions: use one of -add or -remove, not both")
	case add != "":
		if err := db.AddRolePermission(ctx, roleID, add); err != nil {
			return err
		}
		fmt.Printf("%s now holds %s\n", roleID, add)
	case remove != "":
		if err := db.RemoveRolePermission(ctx, roleID, remove); err != nil {
			return err
		}
		fmt.Printf("%s no longer holds %s\n", roleID, remove)
	default:
		return errors.New("role permissions: -add or -remove is required")
	}
	return nil
}

func cmdPermission(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("permission", flag.ContinueOnError)
	dbPath := fs.String("db", defaultDBPath(), "path to the SQLite database")
	positional, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(positional) == 0 || positional[0] != "list" {
		return errors.New("permission: want list")
	}

	db, err := store.Open(ctx, *dbPath)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	perms, err := db.Permissions(ctx)
	if err != nil {
		return err
	}
	for _, p := range perms {
		fmt.Printf("%-20s min scope: %s\n", p.ID, p.MinScope)
	}
	return nil
}
