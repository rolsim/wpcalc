package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"strconv"

	"github.com/rolsim/wpcalc/internal/store"
)

func cmdTenant(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("tenant", flag.ContinueOnError)
	dbPath := fs.String("db", defaultDBPath(), "path to the SQLite database")
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

	db, err := store.Open(ctx, *dbPath)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	switch action := arg(0); action {
	case "add":
		return tenantAdd(ctx, db, arg(1))
	case "list":
		return tenantList(ctx, db)
	case "rename":
		return tenantRename(ctx, db, arg(1), arg(2))
	default:
		return fmt.Errorf("tenant: unknown action %q (want add, list, or rename)", action)
	}
}

func tenantAdd(ctx context.Context, db *store.DB, name string) error {
	if name == "" {
		return errors.New("tenant add: name is required")
	}
	id, err := db.CreateTenant(ctx, name)
	if err != nil {
		return err
	}
	fmt.Printf("created tenant %d: %s\n", id, name)
	return nil
}

func tenantList(ctx context.Context, db *store.DB) error {
	tenants, err := db.Tenants(ctx)
	if err != nil {
		return err
	}
	if len(tenants) == 0 {
		fmt.Println("no tenants yet — create one with `wpcalc tenant add <name>`")
		return nil
	}
	for _, t := range tenants {
		fmt.Printf("%-4d %s\n", t.ID, t.Name)
	}
	return nil
}

func tenantRename(ctx context.Context, db *store.DB, idArg, name string) error {
	id, err := strconv.ParseInt(idArg, 10, 64)
	if err != nil {
		return fmt.Errorf("tenant rename: %q is not a valid tenant id", idArg)
	}
	if err := db.RenameTenant(ctx, id, name); err != nil {
		return err
	}
	fmt.Printf("tenant %d renamed to %s\n", id, name)
	return nil
}
