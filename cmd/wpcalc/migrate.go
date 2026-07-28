package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"source.simonet.internal/rolsim/wpcalc/internal/store"
)

// defaultDBPath resolves where the database lives, in precedence order:
// explicit flag, then WPCALC_DB, then the working directory. The WordPress
// shim sets the environment variable so the sidecar lands under uploads/
// rather than wherever PHP happened to be started from.
func defaultDBPath() string {
	if p := os.Getenv("WPCALC_DB"); p != "" {
		return p
	}
	return "wpcalc.db"
}

func cmdMigrate(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("migrate", flag.ContinueOnError)
	dbPath := fs.String("db", defaultDBPath(), "path to the SQLite database")
	positional, err := parseArgs(fs, args)
	if err != nil {
		return err
	}

	action := "up"
	if len(positional) > 0 {
		action = positional[0]
	}

	// Open already migrates up, which is what the server does on every start.
	db, err := store.Open(ctx, *dbPath)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	switch action {
	case "up":
		fmt.Printf("migrated %s\n", db.Path())
		return nil
	case "down":
		if err := db.MigrateDown(ctx); err != nil {
			return err
		}
		fmt.Printf("rolled back one migration on %s\n", db.Path())
		return nil
	case "status":
		lines, err := db.MigrationStatus(ctx)
		if err != nil {
			return err
		}
		for _, l := range lines {
			fmt.Println(l)
		}
		return nil
	default:
		return fmt.Errorf("migrate: unknown action %q (want up, down, or status)", action)
	}
}
