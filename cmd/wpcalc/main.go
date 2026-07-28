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

// version is overridden at build time via -ldflags "-X main.version=...".
var version = "dev"

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
	case "demo-seed":
		return cmdDemoSeed(ctx, args[1:])
	case "version":
		fmt.Println(version)
		return nil
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
  wpcalc user add|passwd|list [--db PATH]
  wpcalc demo-seed [--db PATH]
  wpcalc version

Exactly one of --addr or --socket must be given to serve.
`)
}
