// Command wpcalc serves a monthly employee working-hours grid.
//
// It runs in two modes over an identical handler tree: standalone on a TCP
// address, or on a unix socket as the sidecar behind the WordPress plugin shim.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "wpcalc: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		usage(os.Stderr)
		return errors.New("no subcommand given")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	switch args[0] {
	case "serve":
		return cmdServe(ctx, args[1:])
	case "migrate":
		return cmdMigrate(ctx, args[1:])
	case "user":
		return cmdUser(ctx, args[1:])
	case "tenant":
		return cmdTenant(ctx, args[1:])
	case "role":
		return cmdRole(ctx, args[1:])
	case "permission":
		return cmdPermission(ctx, args[1:])
	case "sample-employees":
		return cmdSampleEmployees(ctx, args[1:])
	case "manual":
		return cmdManual(ctx, args[1:])
	case "plugin":
		return cmdPlugin(ctx, args[1:])
	case "version":
		return cmdVersion(ctx, args[1:])
	case "help", "-h", "--help":
		usage(os.Stdout)
		return nil
	default:
		usage(os.Stderr)
		return fmt.Errorf("unknown subcommand %q", args[0])
	}
}

func usage(w *os.File) {
	fmt.Fprint(w, `wpcalc — monthly employee working-hours grid

Usage:
  wpcalc serve [--addr :8080 | --socket PATH] [--db PATH]
  wpcalc migrate [up|down|status] [--db PATH]
  wpcalc user add|passwd|lang|roles|list [--db PATH] [-lang de-CH|en]
  wpcalc user grant|revoke <name> [-system|-tenant ID|-employee ID] [-role ID]
  wpcalc tenant add|list|rename [--db PATH]
  wpcalc role add|list|delete|permissions [--db PATH] [-name N] [-scope S] [-add|-remove PERM]
  wpcalc permission list [--db PATH]
  wpcalc sample-employees [--db PATH] [--month YYYY-MM] [--tenant ID]
  wpcalc manual [user|admin|testing] [--lang de-CH|en] [--raw] [--list]
  wpcalc plugin export DIR [--force] [--php-only]
  wpcalc version [--short]

Exactly one of --addr or --socket must be given to serve.
`)
}
