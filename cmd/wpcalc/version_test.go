package main

import (
	"strings"
	"testing"
)

func TestCurrentBuildAlwaysReturnsSomething(t *testing.T) {
	// Note what this deliberately does not check: that VCS information is
	// present. Go does not stamp test binaries with it, so currentBuild()
	// legitimately reports "unknown" here while the real binary carries the
	// revision. Asserting it in a unit test would fail for a reason that has
	// nothing to do with the code — the check belongs where a real binary
	// exists, and lives in the standalone e2e suite.
	b := currentBuild()

	if b.Version == "" {
		t.Error("version is empty; it must always say something")
	}
	if b.Go == "" || b.Platform == "" {
		t.Errorf("incomplete build info: %+v", b)
	}
	if !strings.Contains(b.Platform, "/") {
		t.Errorf("platform %q is not os/arch", b.Platform)
	}
}

func TestDerivedVersionMarksUncommittedChanges(t *testing.T) {
	// A binary built from a dirty tree cannot be reproduced from its revision,
	// so a report naming only the commit would send someone to code that was
	// never built. The marker has to survive into the version string itself,
	// not just the detail block, because that is what gets pasted into tickets.
	clean := derivedVersion(buildInfo{Revision: "c30a3786b6592a0aff5dfa444c4445a634cf2aad"})
	dirty := derivedVersion(buildInfo{Revision: "c30a3786b6592a0aff5dfa444c4445a634cf2aad", Modified: true})

	if strings.Contains(clean, "dirty") {
		t.Errorf("a clean build was marked dirty: %s", clean)
	}
	if !strings.Contains(dirty, "dirty") {
		t.Errorf("a modified build is not marked: %s", dirty)
	}
	if !strings.Contains(clean, "c30a3786b659") {
		t.Errorf("version %q does not carry the revision", clean)
	}
	// Twelve hex characters: enough to be unambiguous, short enough to read.
	if strings.Contains(clean, "c30a3786b6592a0aff5dfa444c4445a634cf2aad") {
		t.Errorf("version %q carries the full 40-character revision", clean)
	}
}

func TestDerivedVersionSurvivesMissingVCS(t *testing.T) {
	// -buildvcs=false, or a build from outside a repository. It must degrade to
	// something honest rather than claiming a version it does not have.
	if got := derivedVersion(buildInfo{}); got != "unknown" {
		t.Errorf("with no revision: %q, want %q", got, "unknown")
	}
}

func TestStampedVersionWinsOverDerived(t *testing.T) {
	// Tagged releases stamp -X main.version. That must take precedence, or a
	// release would report itself by commit hash.
	original := version
	t.Cleanup(func() { version = original })

	version = "v1.2.3"
	if got := currentBuild().Version; got != "v1.2.3" {
		t.Errorf("stamped version ignored: got %q", got)
	}

	version = ""
	if got := currentBuild().Version; got == "v1.2.3" {
		t.Error("the stamped value leaked after being cleared")
	}
}

func TestDetailsAreReadableAndComplete(t *testing.T) {
	out := currentBuild().Details()
	for _, want := range []string{"wpcalc", "commit", "go "} {
		if !strings.Contains(out, want) {
			t.Errorf("details omit %q:\n%s", want, out)
		}
	}
	if !strings.HasSuffix(out, "\n") {
		t.Error("details do not end with a newline")
	}
}

func TestShortRevisionIsBounded(t *testing.T) {
	long := "c30a3786b6592a0aff5dfa444c4445a634cf2aad"
	if got := shortRevision(long); len(got) != 12 {
		t.Errorf("shortRevision gave %d characters: %q", len(got), got)
	}
	// A revision shorter than the cut is returned untouched, not padded.
	if got := shortRevision("abc"); got != "abc" {
		t.Errorf("shortRevision(%q) = %q", "abc", got)
	}
}
