// Command wpcalcctl administers a wpcalc server remotely, entirely over
// /api/v1 — it has no direct database access and depends on nothing but
// github.com/rolsim/wpcalc/sdk/go and the standard library, by design: it
// can run against a server whose filesystem it never sees.
//
// The one thing it cannot do is bootstrap a server with zero accounts —
// that needs `wpcalc user add`, `wpcalc user grant`, and `wpcalc token
// create` on the server itself. Once you hold a token pair from that,
// `wpcalcctl login` is the bridge into everything below.
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
		fmt.Fprintf(os.Stderr, "wpcalcctl: %v\n", err)
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
	case "login":
		return cmdLogin(ctx, args[1:])
	case "tenant":
		return cmdTenant(ctx, args[1:])
	case "role":
		return cmdRole(ctx, args[1:])
	case "permission":
		return cmdPermission(ctx, args[1:])
	case "user":
		return cmdUser(ctx, args[1:])
	case "token":
		return cmdToken(ctx, args[1:])
	case "help", "-h", "--help":
		usage(os.Stdout)
		return nil
	default:
		usage(os.Stderr)
		return fmt.Errorf("unknown subcommand %q", args[0])
	}
}

func usage(w *os.File) {
	fmt.Fprint(w, `wpcalcctl — administer a wpcalc server over /api/v1

Usage:
  wpcalcctl login --server URL --access-token T --refresh-token T [-name N]
  wpcalcctl tenant add|list|rename [ARGS]
  wpcalcctl role add|list|delete|permissions [ARGS]
  wpcalcctl permission list
  wpcalcctl user add|passwd|lang|roles|list|grant|revoke [ARGS]
  wpcalcctl token create|list|revoke|revoke-all [ARGS]

Credentials come from 'wpcalcctl login' and are stored (mode 0600) at
$XDG_CONFIG_HOME/wpcalcctl/credentials.json, or WPCALCCTL_CREDENTIALS
if set. Access tokens are refreshed automatically and the new pair is
saved back before any command returns.

Get a token pair with 'wpcalc token create <name>' on the server itself
— see 'wpcalc help' there.
`)
}
