package main

import "flag"

// parseArgs parses flags that may appear anywhere, returning the
// positional arguments in order — Go's flag package otherwise stops at
// the first non-flag token, so e.g. `role add auditor -scope tenant`
// would silently leave -scope at its default. Duplicated from
// cmd/wpcalc/flags.go: this tool is deliberately a separate module with
// no dependency on the server's own packages, so sharing the ~15 lines
// isn't worth adding one.
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
