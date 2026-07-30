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
	case "token":
		return cmdToken(ctx, args[1:])
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

This binary is the server: it runs the service and holds the two
bootstrap primitives no API client can perform on its own (the first
account, and its first role grant + token). Everything else —
tenants, roles, permissions, day-to-day user and token management —
is administered remotely via /api/v1, through the separate wpcalcctl
tool. See docs/en/admin.md.

Usage:
  wpcalc serve [--addr :8080 | --socket PATH] [--db PATH]
  wpcalc migrate [up|down|status] [--db PATH]
  wpcalc user add [--db PATH] [-lang de-CH|en]
  wpcalc user grant|revoke <name> [-system|-tenant ID|-employee ID] [-role ID]
  wpcalc token create <name> [--db PATH] [-name N]
  wpcalc sample-employees [--db PATH] [--month YYYY-MM] [--tenant ID]
  wpcalc manual [user|admin|testing] [--lang de-CH|en] [--raw] [--list]
  wpcalc plugin export DIR [--force] [--php-only]
  wpcalc version [--short]

Exactly one of --addr or --socket must be given to serve.
`)
}
