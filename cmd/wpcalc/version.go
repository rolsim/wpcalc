package main

import (
	"context"
	"flag"
	"fmt"
	"runtime"
	"runtime/debug"
	"strings"
)

// version is stamped for tagged releases with
// -ldflags "-X main.version=v1.2.3". It is deliberately empty otherwise.
//
// An untagged build does not need it: since Go 1.18 the toolchain embeds the
// revision, the commit time and whether the tree was dirty, and buildVersion
// derives a usable identifier from those. That matters more than it sounds —
// it means a binary handed to a tester identifies itself even though nobody
// remembered to pass a flag, and "which build is that?" has an answer.
var version = ""

// buildInfo is the resolved identity of this binary.
type buildInfo struct {
	Version  string // v1.2.3, or a revision-derived identifier
	Revision string
	Time     string
	Modified bool
	Go       string
	Platform string
}

func currentBuild() buildInfo {
	b := buildInfo{
		Version:  version,
		Go:       runtime.Version(),
		Platform: runtime.GOOS + "/" + runtime.GOARCH,
	}

	if bi, ok := debug.ReadBuildInfo(); ok {
		for _, s := range bi.Settings {
			switch s.Key {
			case "vcs.revision":
				b.Revision = s.Value
			case "vcs.time":
				b.Time = s.Value
			case "vcs.modified":
				b.Modified = s.Value == "true"
			}
		}
	}

	if b.Version == "" {
		b.Version = derivedVersion(b)
	}
	return b
}

// derivedVersion builds an identifier when no release version was stamped.
//
// The dirty marker is not cosmetic: a binary built from uncommitted changes
// cannot be reproduced from the revision alone, and a bug report naming only
// the commit would send someone looking at code that was never built.
func derivedVersion(b buildInfo) string {
	if b.Revision == "" {
		// -buildvcs=false, or built outside a repository.
		return "unknown"
	}
	v := "0.0.0-" + shortRevision(b.Revision)
	if b.Modified {
		v += "-dirty"
	}
	return v
}

func shortRevision(rev string) string {
	if len(rev) > 12 {
		return rev[:12]
	}
	return rev
}

// String is the one-line form, for logs and scripts.
func (b buildInfo) String() string { return b.Version }

// Details is the human-readable block.
func (b buildInfo) Details() string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "wpcalc %s\n", b.Version)

	rev := b.Revision
	if rev == "" {
		rev = "unknown (built without VCS information)"
	} else {
		rev = shortRevision(rev)
		if b.Modified {
			rev += " (uncommitted changes)"
		}
	}
	fmt.Fprintf(&sb, "  commit    %s\n", rev)

	if b.Time != "" {
		fmt.Fprintf(&sb, "  committed %s\n", b.Time)
	}
	fmt.Fprintf(&sb, "  go        %s %s\n", b.Go, b.Platform)
	return sb.String()
}

func cmdVersion(_ context.Context, args []string) error {
	flags := flag.NewFlagSet("version", flag.ContinueOnError)
	short := flags.Bool("short", false, "print only the version, for scripts")
	if _, err := parseArgs(flags, args); err != nil {
		return err
	}

	b := currentBuild()
	if *short {
		fmt.Println(b.Version)
		return nil
	}
	fmt.Print(b.Details())
	return nil
}
