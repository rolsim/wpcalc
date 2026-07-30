package main

import (
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"

	wpcalc "github.com/rolsim/wpcalc"
)

func TestPluginSourcesAreEmbedded(t *testing.T) {
	entries, err := wpcalc.Plugin.ReadDir(wpcalc.PluginRoot)
	if err != nil {
		t.Fatalf("no embedded plugin at %s: %v", wpcalc.PluginRoot, err)
	}

	var php int
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".php") {
			php++
		}
		// The embed pattern is *.php precisely so bin/ stays out; a directory
		// here would mean the binary had been embedded inside itself.
		if e.IsDir() {
			t.Errorf("embedded plugin contains directory %q; the pattern must stay *.php", e.Name())
		}
	}
	if php == 0 {
		t.Fatal("no PHP embedded")
	}

	body, err := wpcalc.Plugin.ReadFile(path.Join(wpcalc.PluginRoot, "wpcalc.php"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"<?php", "Plugin Name:", "WPCalc_Plugin"} {
		if !strings.Contains(string(body), want) {
			t.Errorf("embedded wpcalc.php does not contain %q", want)
		}
	}
}

func TestWritePluginSourcesMatchesTheEmbeddedCopy(t *testing.T) {
	dest := t.TempDir()
	n, err := writePluginSources(dest)
	if err != nil {
		t.Fatalf("writePluginSources: %v", err)
	}
	if n == 0 {
		t.Fatal("wrote no files")
	}

	want, err := wpcalc.Plugin.ReadFile(path.Join(wpcalc.PluginRoot, "wpcalc.php"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dest, "wpcalc.php"))
	if err != nil {
		t.Fatalf("wpcalc.php was not written: %v", err)
	}
	if string(got) != string(want) {
		t.Error("the written PHP differs from the embedded copy")
	}
}

func TestCopySelfProducesARunnableBinary(t *testing.T) {
	// A plugin without a binary looks complete and does not run, so the export
	// carries one. It has to come out executable and byte-identical.
	dest := t.TempDir()
	target, err := copySelf(dest)
	if err != nil {
		t.Fatalf("copySelf: %v", err)
	}

	info, err := os.Stat(target)
	if err != nil {
		t.Fatalf("no binary at %s: %v", target, err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Errorf("exported binary is not executable: %v", info.Mode())
	}

	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	selfInfo, err := os.Stat(self)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() != selfInfo.Size() {
		t.Errorf("exported %d bytes, running binary is %d", info.Size(), selfInfo.Size())
	}

	// It lands under bin/, which is where the shim looks by default.
	if filepath.Base(filepath.Dir(target)) != "bin" {
		t.Errorf("binary went to %s; the shim expects bin/wpcalc", target)
	}
}

func TestCopySelfOntoItselfDoesNotTruncate(t *testing.T) {
	// Reachable by exporting into the directory the binary already occupies.
	// Copying a file onto itself truncates it to nothing, which would destroy
	// the very binary doing the export.
	dest := t.TempDir()
	first, err := copySelf(dest)
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(first)
	if err != nil {
		t.Fatal(err)
	}

	// Second export into the same place must be a no-op, not a truncation.
	if _, err := copySelf(dest); err != nil {
		t.Fatalf("second copySelf: %v", err)
	}
	after, err := os.Stat(first)
	if err != nil {
		t.Fatal(err)
	}
	if after.Size() != before.Size() {
		t.Errorf("re-exporting changed the binary from %d to %d bytes", before.Size(), after.Size())
	}
	if after.Size() == 0 {
		t.Fatal("the exported binary was truncated to nothing")
	}
}

func TestPluginExportRequiresATargetAndRefusesToClobber(t *testing.T) {
	ctx := t.Context()

	if err := cmdPlugin(ctx, nil); err == nil {
		t.Error("no arguments was accepted")
	}
	if err := cmdPlugin(ctx, []string{"export"}); err == nil {
		t.Error("export with no directory was accepted")
	}
	if err := cmdPlugin(ctx, []string{"nonsense", "/tmp"}); err == nil {
		t.Error("an unknown subcommand was accepted")
	}

	parent := t.TempDir()
	if err := cmdPlugin(ctx, []string{"export", parent, "--php-only"}); err != nil {
		t.Fatalf("first export: %v", err)
	}
	// A second export must not quietly overwrite a plugin someone may have
	// adjusted in place.
	if err := cmdPlugin(ctx, []string{"export", parent, "--php-only"}); err == nil {
		t.Error("a second export overwrote without --force")
	}
	if err := cmdPlugin(ctx, []string{"export", parent, "--php-only", "--force"}); err != nil {
		t.Errorf("--force was refused: %v", err)
	}
}

func TestPhpOnlyExportOmitsTheBinary(t *testing.T) {
	parent := t.TempDir()
	if err := cmdPlugin(t.Context(), []string{"export", parent, "--php-only"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(parent, "wpcalc", "wpcalc.php")); err != nil {
		t.Errorf("PHP missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(parent, "wpcalc", "bin", "wpcalc")); err == nil {
		t.Error("--php-only wrote a binary anyway")
	}
}
