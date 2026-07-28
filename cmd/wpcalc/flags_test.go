package main

import (
	"flag"
	"io"
	"slices"
	"testing"
)

func TestParseArgsAcceptsFlagsAfterPositionals(t *testing.T) {
	// The regression this guards is not cosmetic. With plain flag.Parse,
	// `user add alice --db /tmp/x.db` left --db at its default, so the account
	// was written into a different database than the operator named — and the
	// server, correctly pointed at the intended file, then reported that no
	// accounts existed.
	cases := []struct {
		name    string
		args    []string
		wantPos []string
		wantDB  string
		wantRol string
	}{
		{
			name:    "flags after positionals",
			args:    []string{"add", "alice", "--db", "/tmp/x.db", "-role", "admin"},
			wantPos: []string{"add", "alice"},
			wantDB:  "/tmp/x.db",
			wantRol: "admin",
		},
		{
			name:    "flags before positionals",
			args:    []string{"--db", "/tmp/x.db", "-role", "admin", "add", "alice"},
			wantPos: []string{"add", "alice"},
			wantDB:  "/tmp/x.db",
			wantRol: "admin",
		},
		{
			name:    "flags interleaved",
			args:    []string{"add", "--db", "/tmp/x.db", "alice", "-role", "admin"},
			wantPos: []string{"add", "alice"},
			wantDB:  "/tmp/x.db",
			wantRol: "admin",
		},
		{
			name:    "no flags at all",
			args:    []string{"list"},
			wantPos: []string{"list"},
			wantDB:  "default.db",
			wantRol: "user",
		},
		{
			name:    "no arguments at all",
			args:    nil,
			wantPos: nil,
			wantDB:  "default.db",
			wantRol: "user",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fs := flag.NewFlagSet("test", flag.ContinueOnError)
			fs.SetOutput(io.Discard)
			db := fs.String("db", "default.db", "")
			role := fs.String("role", "user", "")

			got, err := parseArgs(fs, c.args)
			if err != nil {
				t.Fatalf("parseArgs: %v", err)
			}
			if !slices.Equal(got, c.wantPos) {
				t.Errorf("positionals = %v, want %v", got, c.wantPos)
			}
			if *db != c.wantDB {
				t.Errorf("--db = %q, want %q", *db, c.wantDB)
			}
			if *role != c.wantRol {
				t.Errorf("-role = %q, want %q", *role, c.wantRol)
			}
		})
	}
}

func TestParseArgsReportsUnknownFlags(t *testing.T) {
	// An unrecognised flag must be an error, not something quietly collected
	// as a positional argument.
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.String("db", "default.db", "")

	if _, err := parseArgs(fs, []string{"add", "alice", "--nope", "x"}); err == nil {
		t.Error("unknown flag accepted")
	}
}
