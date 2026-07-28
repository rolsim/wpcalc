package main

import "flag"

// parseArgs parses flags that may appear anywhere, returning the positional
// arguments in order.
//
// Go's flag package stops at the first non-flag token, so `user add alice
// --db /path` silently leaves --db at its default. For this program that is
// not a cosmetic annoyance: it wrote the new account into a different database
// file than the one the operator named, then the server — correctly pointed at
// the intended file — reported that no accounts existed. A flag that is
// ignored rather than rejected is the worst of both worlds, so flags are
// accepted in either position.
func parseArgs(fs *flag.FlagSet, args []string) ([]string, error) {
	var positional []string
	rest := args
	for {
		if err := fs.Parse(rest); err != nil {
			return nil, err
		}
		if fs.NArg() == 0 {
			return positional, nil
		}
		positional = append(positional, fs.Arg(0))
		rest = fs.Args()[1:]
	}
}
