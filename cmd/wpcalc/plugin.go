package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"

	wpcalc "source.simonet.internal/rolsim/wpcalc"
)

// cmdPlugin writes the WordPress plugin out of the binary.
//
// The point is that one file is enough to hand someone. Before this, testing
// the WordPress side meant shipping the binary *and* fetching the PHP from the
// source tree, and the two could drift — a plugin from one version talking to
// a sidecar from another.
//
// The binary copies itself in alongside, because a plugin without one does not
// run: the shim's whole job is to spawn it. Exporting only the PHP would
// produce a directory that looks complete and is not.
func cmdPlugin(_ context.Context, args []string) error {
	flags := flag.NewFlagSet("plugin", flag.ContinueOnError)
	force := flags.Bool("force", false, "overwrite an existing plugin directory")
	phpOnly := flags.Bool("php-only", false, "write the PHP only, without the binary")

	positional, err := parseArgs(flags, args)
	if err != nil {
		return err
	}
	if len(positional) == 0 || positional[0] != "export" {
		return errors.New("plugin: want `plugin export <directory>`")
	}
	if len(positional) < 2 {
		return errors.New("plugin export: a target directory is required, " +
			"e.g. `wpcalc plugin export /var/www/html/wp-content/plugins`")
	}

	// The argument names the plugins directory, and wpcalc/ is created inside
	// it — which is how someone thinks about this ("export into my plugins
	// folder"), and it keeps the directory name matching the plugin.
	parent := positional[1]
	dest := filepath.Join(parent, wpcalc.PluginDirName)

	if info, err := os.Stat(dest); err == nil {
		if !*force {
			return fmt.Errorf("plugin export: %s already exists; pass --force to overwrite it", dest)
		}
		if !info.IsDir() {
			return fmt.Errorf("plugin export: %s exists and is not a directory", dest)
		}
	}

	if err := os.MkdirAll(dest, 0o755); err != nil {
		return fmt.Errorf("plugin export: create %s: %w", dest, err)
	}

	written, err := writePluginSources(dest)
	if err != nil {
		return err
	}

	if *phpOnly {
		fmt.Printf("wrote %d file(s) to %s (no binary: --php-only)\n", written, dest)
		fmt.Println("the plugin will not start until bin/wpcalc is placed alongside it")
		return nil
	}

	binPath, err := copySelf(dest)
	if err != nil {
		return err
	}

	fmt.Printf("wrote the plugin to %s\n", dest)
	fmt.Printf("  %d PHP file(s) and %s\n", written, binPath)
	fmt.Println("activate it in WordPress, then open “Arbeitszeiten” in the admin menu")
	return nil
}

// writePluginSources copies the embedded PHP into dest.
func writePluginSources(dest string) (int, error) {
	entries, err := fs.Glob(wpcalc.Plugin, path.Join(wpcalc.PluginRoot, "*.php"))
	if err != nil {
		return 0, fmt.Errorf("plugin export: list sources: %w", err)
	}
	if len(entries) == 0 {
		return 0, errors.New("plugin export: no PHP sources are embedded in this binary")
	}

	for _, src := range entries {
		body, err := wpcalc.Plugin.ReadFile(src)
		if err != nil {
			return 0, fmt.Errorf("plugin export: read %s: %w", src, err)
		}
		target := filepath.Join(dest, path.Base(src))
		if err := os.WriteFile(target, body, 0o644); err != nil {
			return 0, fmt.Errorf("plugin export: write %s: %w", target, err)
		}
	}
	return len(entries), nil
}

// copySelf places a copy of the running binary at dest/bin/wpcalc.
//
// Copying rather than hard-linking or symlinking: the destination is usually
// on a different filesystem, often a container mount, and a symlink into
// whatever path the exporting binary happened to occupy would break the moment
// that path changed.
func copySelf(dest string) (string, error) {
	self, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("plugin export: locate this binary: %w", err)
	}
	self, err = filepath.EvalSymlinks(self)
	if err != nil {
		return "", fmt.Errorf("plugin export: resolve this binary: %w", err)
	}

	binDir := filepath.Join(dest, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return "", fmt.Errorf("plugin export: create %s: %w", binDir, err)
	}
	target := filepath.Join(binDir, "wpcalc")

	// Copying a file onto itself truncates it to nothing. Reachable by
	// exporting into the directory the binary is already installed in.
	if same, err := sameFile(self, target); err != nil {
		return "", err
	} else if same {
		return target, nil
	}

	in, err := os.Open(self)
	if err != nil {
		return "", fmt.Errorf("plugin export: open this binary: %w", err)
	}
	defer func() { _ = in.Close() }()

	// Write to a temporary name and rename into place, so an interrupted
	// export cannot leave a half-written binary that WordPress will try to run.
	tmp, err := os.CreateTemp(binDir, ".wpcalc-*")
	if err != nil {
		return "", fmt.Errorf("plugin export: create temporary file: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()

	if _, err := io.Copy(tmp, in); err != nil {
		_ = tmp.Close()
		return "", fmt.Errorf("plugin export: copy binary: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return "", fmt.Errorf("plugin export: close temporary file: %w", err)
	}
	if err := os.Chmod(tmpName, 0o755); err != nil {
		return "", fmt.Errorf("plugin export: make the binary executable: %w", err)
	}
	if err := os.Rename(tmpName, target); err != nil {
		return "", fmt.Errorf("plugin export: place the binary: %w", err)
	}
	return target, nil
}

func sameFile(a, b string) (bool, error) {
	ai, err := os.Stat(a)
	if err != nil {
		return false, fmt.Errorf("plugin export: stat %s: %w", a, err)
	}
	bi, err := os.Stat(b)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("plugin export: stat %s: %w", b, err)
	}
	return os.SameFile(ai, bi), nil
}
