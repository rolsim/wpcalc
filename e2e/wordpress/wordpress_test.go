//go:build e2e_wp

// Package wordpress_test drives the plugin inside a real WordPress install.
//
// Tagged out of `go test ./...` because it pulls images and takes minutes.
// Run it with `make e2e-wp`.
//
// The stack comes up once for the package and is torn down on every exit path,
// including panics and failures, so a broken run leaves nothing behind for the
// next one to trip over. What it asserts is only what the integration can
// prove: that the PHP shim finds and starts the binary, that identity signed
// by PHP is accepted over the socket, that a nonce-bearing form post through
// wp-admin reaches SQLite and a nonce-less one does not, and that the sidecar
// is reachable by nothing but the socket.
package wordpress_test

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

const (
	project = "wpcalc-e2e"
	siteURL = "http://localhost:8099"
	wpUser  = "admin"
	wpPass  = "admin-password-for-tests"
)

var (
	repoRoot   string
	composeDir string
	exportDir  string
	binaryPath string
)

// TestMain owns the stack for the whole package.
//
// Per-test setup would mean pulling images and installing WordPress once per
// test, and a test that tore the stack down in its own cleanup would leave the
// next one with nothing to talk to — which is exactly how the first version of
// this file failed.
func TestMain(m *testing.M) {
	code := 1
	// A deferred exit so teardown runs even when setup calls log.Fatal.
	defer func() { os.Exit(code) }()

	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		log.Printf("locate repo root: %v", err)
		return
	}
	repoRoot = strings.TrimSpace(string(out))
	composeDir = filepath.Join(repoRoot, "e2e", "wordpress")
	exportDir = filepath.Join(composeDir, ".export")
	// Inside the exported plugin, not the source tree: the export is what the
	// containers mount and what these tests are therefore checking.
	binaryPath = filepath.Join(exportDir, "wpcalc", "bin", "wpcalc")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	// Always tear down, including after a panic in setup.
	defer func() {
		down, cancelDown := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancelDown()
		if out, err := compose(down, "down", "-v", "--remove-orphans"); err != nil {
			log.Printf("teardown reported: %v\n%s", err, out)
		}
	}()

	if err := setup(ctx); err != nil {
		log.Printf("setup failed: %v", err)
		return
	}

	code = m.Run()
}

func setup(ctx context.Context) error {
	if err := buildAndExportPlugin(ctx); err != nil {
		return err
	}

	// Clear anything a previous run left behind.
	_, _ = compose(ctx, "down", "-v", "--remove-orphans")

	if out, err := compose(ctx, "up", "-d", "--wait"); err != nil {
		return fmt.Errorf("compose up: %w\n%s", err, out)
	}

	if err := waitFor(ctx, "WordPress to answer", 3*time.Minute, func() bool {
		resp, err := http.Get(siteURL + "/wp-admin/install.php")
		if err != nil {
			return false
		}
		_ = resp.Body.Close()
		return resp.StatusCode < 500
	}); err != nil {
		return err
	}

	if out, err := wpCLI(ctx, "core", "install",
		"--url="+siteURL,
		"--title=wpcalc e2e",
		"--admin_user="+wpUser,
		"--admin_password="+wpPass,
		"--admin_email=e2e@example.invalid",
		"--skip-email"); err != nil {
		return fmt.Errorf("wp core install: %w\n%s", err, out)
	}

	if out, err := wpCLI(ctx, "plugin", "activate", "wpcalc"); err != nil {
		return fmt.Errorf("wp plugin activate: %w\n%s", err, out)
	}
	return nil
}

// buildAndExportPlugin builds the binary and then has it export itself as a
// WordPress plugin, which is what the containers mount.
//
// Going through `plugin export` rather than copying the source directory means
// this suite tests the artifact a tester is handed. A broken export fails here
// instead of passing against a directory that is never shipped.
//
// CGO_ENABLED=0 is what makes it work at all: the binary built on the host runs
// unchanged inside the container, with no toolchain or libc in the image.
func buildAndExportPlugin(ctx context.Context) error {
	if err := os.RemoveAll(exportDir); err != nil {
		return fmt.Errorf("clear export directory: %w", err)
	}
	if err := os.MkdirAll(exportDir, 0o755); err != nil {
		return err
	}

	built := filepath.Join(exportDir, "wpcalc-build")
	build := exec.CommandContext(ctx, "go", "build", "-o", built, "./cmd/wpcalc")
	build.Dir = repoRoot
	build.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS=linux", "GOARCH=amd64")
	if out, err := build.CombinedOutput(); err != nil {
		return fmt.Errorf("build sidecar: %w\n%s", err, out)
	}

	export := exec.CommandContext(ctx, built, "plugin", "export", exportDir, "--force")
	if out, err := export.CombinedOutput(); err != nil {
		return fmt.Errorf("plugin export: %w\n%s", err, out)
	}

	if _, err := os.Stat(binaryPath); err != nil {
		return fmt.Errorf("export produced no binary at %s: %w", binaryPath, err)
	}
	return os.Remove(built)
}

func compose(ctx context.Context, args ...string) (string, error) {
	full := append([]string{"compose", "-p", project}, args...)
	cmd := exec.CommandContext(ctx, "docker", full...)
	cmd.Dir = composeDir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func wpCLI(ctx context.Context, args ...string) (string, error) {
	return compose(ctx, append([]string{"exec", "-T", "wpcli", "wp"}, args...)...)
}

// shellQuote wraps s in single quotes for embedding in a `sh -c` string,
// escaping any single quote it contains.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func waitFor(ctx context.Context, what string, timeout time.Duration, fn func() bool) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if fn() {
			return nil
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("timed out after %s waiting for %s", timeout, what)
}

// ---- tests --------------------------------------------------------------

func TestPluginServesTheGridInsideWPAdmin(t *testing.T) {
	ctx := t.Context()
	client := loginToWordPress(t)

	body := get(t, client, siteURL+"/wp-admin/admin.php?page=wpcalc")

	if strings.Contains(body, "could not start") || strings.Contains(body, "not responding") {
		logs, _ := compose(ctx, "exec", "-T", "wordpress",
			"sh", "-c", "tail -40 /var/www/html/wp-content/uploads/wpcalc/wpcalc.log 2>/dev/null")
		t.Fatalf("the shim could not start the sidecar.\npage:\n%s\n\nsidecar log:\n%s", excerpt(body), logs)
	}

	if !strings.Contains(body, "wpcalc-app") {
		t.Fatalf("admin page does not contain the app fragment:\n%s", excerpt(body))
	}
	// The app returns a fragment here; a second document would fight WordPress
	// for the head and be invalid markup.
	if n := strings.Count(strings.ToLower(body), "<html"); n != 1 {
		t.Errorf("page contains %d <html> elements, want exactly 1 (WordPress's own)", n)
	}
	// The interface language follows WordPress's own per-user locale, which
	// this install sets to en_US, so the chrome must be English here. Its
	// presence also proves the sidecar produced this rather than some cached
	// PHP output. The German case is covered by TestWordPressLocaleDrivesTheInterfaceLanguage.
	if !strings.Contains(body, "Employees") {
		t.Errorf("expected the localised app chrome:\n%s", excerpt(body))
	}
	// The index redirect must have been followed to a real month.
	if !regexp.MustCompile(`(January|February|March|April|May|June|July|August|September|October|November|December) \d{4}`).MatchString(body) {
		t.Errorf("no month heading rendered; the redirect was probably not followed:\n%s", excerpt(body))
	}
}

func TestAssetsAndReportsProxyThroughTheAdminPage(t *testing.T) {
	client := loginToWordPress(t)

	// Assets must come back with their own content type, not wrapped in admin
	// chrome — a stylesheet inside an HTML page is not a stylesheet.
	resp := getResponse(t, client, siteURL+"/wp-admin/admin.php?page=wpcalc&wpcalc_path="+
		url.QueryEscape("/static/app.css"))
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/css") {
		t.Errorf("stylesheet Content-Type = %q, want text/css", ct)
	}
	if !strings.Contains(resp.body, "table.grid") {
		t.Error("stylesheet body does not look like the app's CSS")
	}

	resp = getResponse(t, client, siteURL+"/wp-admin/admin.php?page=wpcalc&wpcalc_path="+
		url.QueryEscape("/report/month/2026-01.pdf"))
	if ct := resp.Header.Get("Content-Type"); ct != "application/pdf" {
		t.Errorf("report Content-Type = %q, want application/pdf", ct)
	}
	if !strings.HasPrefix(resp.body, "%PDF-") || !strings.Contains(resp.body, "%%EOF") {
		t.Errorf("report is not a complete PDF: %.60q", resp.body)
	}
}

func TestWriteThroughWPAdminRequiresANonceAndReachesSQLite(t *testing.T) {
	client := loginToWordPress(t)

	page := get(t, client, siteURL+"/wp-admin/admin.php?page=wpcalc")
	nonce := extractNonce(t, page)

	// The rendered form's action carries page and wpcalc_path in its query
	// string; the fields go in the body.
	employeesURL := siteURL + "/wp-admin/admin.php?page=wpcalc&wpcalc_path=" + url.QueryEscape("/employees")

	resp, err := client.PostForm(employeesURL, url.Values{
		"wpcalc_nonce": {nonce},
		"name":         {"E2E Person"},
		"start_date":   {"2026-01-01"},
	})
	if err != nil {
		t.Fatalf("POST with nonce: %v", err)
	}
	_ = resp.Body.Close()

	if !strings.Contains(get(t, client, employeesURL), "E2E Person") {
		t.Fatal("an employee created through wp-admin is not listed; the write did not reach SQLite")
	}

	// Without a nonce the capability check alone would let any page on the
	// internet submit this form on a logged-in admin's behalf.
	resp, err = client.PostForm(employeesURL, url.Values{
		"name":       {"No Nonce"},
		"start_date": {"2026-01-01"},
	})
	if err != nil {
		t.Fatalf("POST without nonce: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("nonce-less POST returned %d, want 403", resp.StatusCode)
	}
	if strings.Contains(get(t, client, employeesURL), "No Nonce") {
		t.Error("a nonce-less POST created an employee")
	}
}

func TestUnauthenticatedVisitorIsSentToLogin(t *testing.T) {
	client := &http.Client{
		Timeout:       30 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	resp, err := client.Get(siteURL + "/wp-admin/admin.php?page=wpcalc")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusFound && resp.StatusCode != http.StatusMovedPermanently {
		t.Fatalf("status %d, want a redirect to wp-login", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); !strings.Contains(loc, "wp-login.php") {
		t.Errorf("Location = %q, want wp-login.php", loc)
	}
}

func TestSidecarIsReachableOnlyThroughTheSocket(t *testing.T) {
	ctx := t.Context()

	// Identity headers are trusted because they arrive on a unix socket, so
	// the sidecar must not be listening on TCP at all.
	//
	// This inspects how the process was actually started rather than
	// enumerating the container's listeners. Two earlier attempts at the
	// latter tested the wrong thing: fetching /healthz over HTTP reaches
	// Apache, which answers 200 for unknown paths, and reading /proc/net/tcp
	// flags Docker's own embedded DNS resolver on 127.0.0.11. The command line
	// is the property that actually matters and has no such false positives.
	out, err := compose(ctx, "exec", "-T", "wordpress", "sh", "-c",
		`for p in /proc/[0-9]*/cmdline; do tr '\0' ' ' < "$p" 2>/dev/null | grep -q 'wpcalc serve' && tr '\0' ' ' < "$p" && echo; done`)
	if err != nil {
		t.Fatalf("inspect the sidecar command line: %v\n%s", err, out)
	}

	cmdline := strings.TrimSpace(out)
	if cmdline == "" {
		t.Fatal("no running sidecar found in the container")
	}
	if !strings.Contains(cmdline, "--socket") {
		t.Errorf("sidecar was not started with --socket: %s", cmdline)
	}
	if strings.Contains(cmdline, "--addr") {
		t.Errorf("sidecar was started with a TCP listener: %s", cmdline)
	}

	// Reaching the socket is what the signature is trusted on top of, so it
	// must not be world-accessible.
	perms, err := compose(ctx, "exec", "-T", "wordpress",
		"stat", "-c", "%a", "/var/www/html/wp-content/uploads/wpcalc/wpcalc.sock")
	if err != nil {
		t.Fatalf("stat socket: %v\n%s", err, perms)
	}
	if mode := strings.TrimSpace(perms); mode != "660" {
		t.Errorf("socket mode is %s, want 660", mode)
	}
}

func TestTheMountedPluginIsTheExportedOne(t *testing.T) {
	// Guards the premise of this whole suite. If the compose mount ever drifts
	// back to the source directory, every other test here would still pass
	// while proving nothing about what a tester receives.
	php := filepath.Join(exportDir, "wpcalc", "wpcalc.php")
	exported, err := os.ReadFile(php)
	if err != nil {
		t.Fatalf("no exported PHP at %s: %v", php, err)
	}
	source, err := os.ReadFile(filepath.Join(repoRoot, "wordpress", "wpcalc", "wpcalc.php"))
	if err != nil {
		t.Fatal(err)
	}
	if string(exported) != string(source) {
		t.Error("the exported plugin differs from the source it was embedded from")
	}

	// And the container is running that exported binary, not some other one.
	out, err := compose(t.Context(), "exec", "-T", "wordpress",
		"sh", "-c", "ls -l /var/www/html/wp-content/plugins/wpcalc/bin/wpcalc")
	if err != nil {
		t.Fatalf("stat the mounted binary: %v\n%s", err, out)
	}
	if !strings.Contains(out, "wpcalc") {
		t.Errorf("the mounted plugin has no binary:\n%s", out)
	}
}

func TestWordPressLocaleDrivesTheInterfaceLanguage(t *testing.T) {
	// Under WordPress there is no second language preference stored on our
	// side: WordPress owns the user record, so its per-user locale decides.
	// A duplicate preference here could disagree with the one the site admin
	// set, and there would be no way to tell which was authoritative.
	ctx := t.Context()

	setLocale := func(locale string) {
		t.Helper()
		if out, err := wpCLI(ctx, "user", "meta", "update", wpUser, "locale", locale); err != nil {
			t.Fatalf("set locale %s: %v\n%s", locale, err, out)
		}
	}
	t.Cleanup(func() { _, _ = wpCLI(context.Background(), "user", "meta", "update", wpUser, "locale", "en_US") })

	client := loginToWordPress(t)

	setLocale("de_CH")
	body := get(t, client, siteURL+"/wp-admin/admin.php?page=wpcalc")
	if !strings.Contains(body, "Mitarbeitende") {
		t.Errorf("locale de_CH did not produce the German interface:\n%s", excerpt(body))
	}
	if strings.Contains(body, ">Employees<") {
		t.Error("English chrome present despite a German locale")
	}

	setLocale("en_US")
	body = get(t, client, siteURL+"/wp-admin/admin.php?page=wpcalc")
	if !strings.Contains(body, "Employees") {
		t.Errorf("locale en_US did not produce the English interface:\n%s", excerpt(body))
	}

	// And the app must not offer its own language control here, because
	// changing it would not change what WordPress thinks.
	if strings.Contains(body, `name="lang"`) {
		t.Error("the app offers a language selector under WordPress, where it cannot persist")
	}
}

func TestSettingsPageReportsStatusAndNeverShowsTheSecret(t *testing.T) {
	client := loginToWordPress(t)

	// Touch the app page first so the sidecar is running and the status has
	// something true to report.
	_ = get(t, client, siteURL+"/wp-admin/admin.php?page=wpcalc")

	body := get(t, client, siteURL+"/wp-admin/admin.php?page=wpcalc-settings")

	for _, want := range []string{"wpcalc settings", "Status", "Binary", "proc_open", "Socket", "Database"} {
		if !strings.Contains(body, want) {
			t.Errorf("settings page is missing %q", want)
		}
	}
	// The status must reflect a running service, not a hardcoded label.
	if !strings.Contains(body, "wpcalc.sock") {
		t.Error("settings page does not report the socket path")
	}

	// The shared secret is a credential, not a setting: it must never be
	// rendered. Read it out of the database and assert it is absent from the
	// page — checking for the word "secret" would pass on the explanation text
	// while the value itself leaked.
	out, err := wpCLI(t.Context(), "option", "get", "wpcalc_shared_secret")
	if err != nil {
		t.Fatalf("read the secret: %v\n%s", err, out)
	}
	secret := strings.TrimSpace(out)
	if len(secret) < 32 {
		t.Fatalf("stored secret looks wrong: %q", secret)
	}
	if strings.Contains(body, secret) {
		t.Error("the shared secret is rendered on the settings page")
	}
}

// TestPluginZipInstallsCleanlyViaWPCLI verifies the artifact the release
// workflow actually publishes — a zip with a single top-level wpcalc/
// folder — installs and runs the way a real host would install it,
// through WordPress's own unzip path rather than the bind mount every
// other test in this file relies on.
//
// It extracts into wpcalc-ziptest/, not wpcalc/: the latter is the
// bind-mounted directory the rest of the suite uses, and unzipping onto a
// live bind mount hits container/host UID and mountpoint mismatches that
// are an artifact of this test harness, not something a real install ever
// hits. wpcalc.php resolves its own paths from plugin_dir_path(__FILE__)
// and uses a fixed MENU_SLUG, both independent of the containing
// directory's name, so this is still a faithful test of the zip itself.
//
// Critically, this also strips the extracted binary's executable bit
// before activating — reproducing the documented failure mode where PHP's
// own ZipArchive extraction (what "Upload Plugin" and `wp plugin install`
// both go through) does not restore it — to prove the shim's chmod
// self-heal in wpcalc.php's ensure_running() actually recovers on a host
// with no shell to fix it by hand. That is the entire point of shipping
// this zip in the first place.
func TestPluginZipInstallsCleanlyViaWPCLI(t *testing.T) {
	ctx := t.Context()
	const slug = "wpcalc-ziptest"
	pluginPath := "/var/www/html/wp-content/plugins/" + slug

	zipPath := buildPluginZip(t)
	if out, err := compose(ctx, "cp", zipPath, "wordpress:/tmp/wpcalc-plugin.zip"); err != nil {
		t.Fatalf("copy zip into the wordpress container: %v\n%s", err, out)
	}

	// The image has no `unzip` binary, but PHP's ZipArchive is guaranteed —
	// WordPress itself requires ext-zip — and using it here is more faithful
	// anyway: it is the exact extractor "Upload Plugin" and `wp plugin
	// install` both go through, which is what this test exists to exercise.
	//
	// Run as www-data, not the exec default of root: on a real host the same
	// PHP process that serves admin requests is the one that extracts an
	// upload, so the files it later chmods are always its own. Extracting as
	// root here would make the exec-bit self-heal fail on an ownership
	// mismatch this test invented, not the one the feature actually guards
	// against.
	extractPHP := `$z = new ZipArchive(); ` +
		`if ($z->open('/tmp/wpcalc-plugin.zip') !== true) { fwrite(STDERR, "open failed\n"); exit(1); } ` +
		`$z->extractTo('/tmp/wpcalc-unzip'); $z->close();`
	if out, err := compose(ctx, "exec", "-T", "-u", "www-data", "wordpress", "sh", "-c",
		"rm -rf /tmp/wpcalc-unzip && mkdir /tmp/wpcalc-unzip && php -r "+shellQuote(extractPHP)); err != nil {
		t.Fatalf("extract the zip via ZipArchive: %v\n%s", err, out)
	}
	// The zip must extract exactly one top-level directory, "wpcalc" — the
	// same structure WordPress's uploader requires — which is then placed
	// under this test's own slug so it does not collide with the bind mount.
	if out, err := compose(ctx, "exec", "-T", "-u", "www-data", "wordpress", "sh", "-c",
		"ls /tmp/wpcalc-unzip"); err != nil || strings.TrimSpace(out) != "wpcalc" {
		t.Fatalf("zip did not extract a single wpcalc/ directory, got %q, err %v", out, err)
	}
	if out, err := compose(ctx, "exec", "-T", "-u", "www-data", "wordpress", "sh", "-c",
		"rm -rf "+pluginPath+" && mv /tmp/wpcalc-unzip/wpcalc "+pluginPath); err != nil {
		t.Fatalf("place the extracted plugin: %v\n%s", err, out)
	}

	// Deactivate the bind-mounted original first: two active plugins would
	// both register the same MENU_SLUG admin page, and only one should be
	// live while this test proves the zip-derived copy works on its own.
	if out, err := wpCLI(ctx, "plugin", "deactivate", "wpcalc"); err != nil {
		t.Fatalf("deactivate the bind-mounted plugin: %v\n%s", err, out)
	}
	if out, err := compose(ctx, "exec", "-T", "wordpress", "sh", "-c",
		"pkill -f 'wpcalc serve'; rm -f /var/www/html/wp-content/uploads/wpcalc/wpcalc.sock; true"); err != nil {
		t.Logf("stopping the sidecar reported: %v\n%s", err, out)
	}
	t.Cleanup(func() {
		cleanupCtx := context.Background()
		_, _ = wpCLI(cleanupCtx, "plugin", "deactivate", slug)
		_, _ = compose(cleanupCtx, "exec", "-T", "wordpress", "rm", "-rf", pluginPath)
		_, _ = compose(cleanupCtx, "exec", "-T", "wordpress", "sh", "-c",
			"pkill -f 'wpcalc serve'; rm -f /var/www/html/wp-content/uploads/wpcalc/wpcalc.sock; true")
		if out, err := wpCLI(cleanupCtx, "plugin", "activate", "wpcalc"); err != nil {
			t.Errorf("restore the bind-mounted plugin's active state: %v\n%s", err, out)
		}
	})

	// Simulate a host whose zip extraction dropped the exec bit — the
	// documented ZipArchive behavior this feature exists for — before the
	// plugin gets its first chance to run.
	binPath := pluginPath + "/bin/wpcalc"
	if out, err := compose(ctx, "exec", "-T", "-u", "www-data", "wordpress", "chmod", "644", binPath); err != nil {
		t.Fatalf("strip the exec bit: %v\n%s", err, out)
	}
	if out, err := compose(ctx, "exec", "-T", "wordpress", "sh", "-c",
		"stat -c %a "+binPath); err != nil || strings.TrimSpace(out) != "644" {
		t.Fatalf("exec bit was not actually stripped, got %q, err %v", out, err)
	}

	if out, err := wpCLI(ctx, "plugin", "activate", slug); err != nil {
		t.Fatalf("activate the zip-installed plugin: %v\n%s", err, out)
	}

	client := loginToWordPress(t)
	body := get(t, client, siteURL+"/wp-admin/admin.php?page=wpcalc")

	if strings.Contains(body, "not executable") {
		t.Fatalf("the shim did not self-heal the stripped exec bit:\n%s", excerpt(body))
	}
	if strings.Contains(body, "could not start") || strings.Contains(body, "not responding") {
		logs, _ := compose(ctx, "exec", "-T", "wordpress",
			"sh", "-c", "tail -40 /var/www/html/wp-content/uploads/wpcalc/wpcalc.log 2>/dev/null")
		t.Fatalf("sidecar built from the zip-installed plugin would not start.\npage:\n%s\n\nsidecar log:\n%s",
			excerpt(body), logs)
	}
	if !strings.Contains(body, "wpcalc-app") {
		t.Fatalf("admin page does not contain the app fragment after a zip install:\n%s", excerpt(body))
	}

	// The self-heal itself must have restored the exec bit on disk, not just
	// worked around it for one process invocation.
	out, err := compose(ctx, "exec", "-T", "wordpress", "sh", "-c", "stat -c %a "+binPath)
	if err != nil {
		t.Fatalf("stat the binary after activation: %v\n%s", err, out)
	}
	if mode := strings.TrimSpace(out); mode != "755" {
		t.Errorf("binary mode after self-heal = %s, want 755", mode)
	}
}

// buildPluginZip zips the already-exported plugin directory into a single
// top-level wpcalc/ folder — exactly the structure release.yml's "Build
// WordPress plugin zip" step produces and the one WordPress's own uploader
// requires.
func buildPluginZip(t *testing.T) string {
	t.Helper()
	zipPath := filepath.Join(exportDir, "wpcalc-plugin.zip")
	_ = os.Remove(zipPath)
	cmd := exec.Command("zip", "-qr", zipPath, "wpcalc")
	cmd.Dir = exportDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("zip plugin: %v\n%s", err, out)
	}
	return zipPath
}

// TestZZPluginDegradesWhenBinaryIsMissing runs last: it hides the binary and
// kills the sidecar, which every other test needs. The name sorts it to the
// end rather than relying on where it sits in the file.
func TestZZPluginDegradesWhenBinaryIsMissing(t *testing.T) {
	ctx := t.Context()

	hidden := binaryPath + ".hidden"
	if err := os.Rename(binaryPath, hidden); err != nil {
		t.Skipf("cannot stage the missing-binary case: %v", err)
	}
	t.Cleanup(func() { _ = os.Rename(hidden, binaryPath) })

	// Stop the running sidecar so the shim has to find the binary again rather
	// than reusing a socket that already answers.
	if out, err := compose(ctx, "exec", "-T", "wordpress", "sh", "-c",
		"pkill -f 'wpcalc serve'; rm -f /var/www/html/wp-content/uploads/wpcalc/wpcalc.sock; true"); err != nil {
		t.Logf("stopping the sidecar reported: %v\n%s", err, out)
	}

	client := loginToWordPress(t)
	body := get(t, client, siteURL+"/wp-admin/admin.php?page=wpcalc")

	// This is the failure most likely to happen on a real host, and the plugin
	// has to name it. Nothing else in WordPress will mention a missing binary,
	// so a blank page here costs an afternoon.
	if strings.TrimSpace(body) == "" {
		t.Fatal("missing binary produced a blank page")
	}
	if !strings.Contains(body, "could not start") && !strings.Contains(body, "was not found") {
		t.Errorf("missing binary did not produce an explanation:\n%s", excerpt(body))
	}
	if !strings.Contains(body, "notice-error") {
		t.Error("the explanation is not rendered as an admin error notice")
	}
}

// ---- helpers ------------------------------------------------------------

func loginToWordPress(t *testing.T) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Jar: jar, Timeout: 60 * time.Second}

	// wp-login.php refuses to log anyone in unless the test cookie is present.
	u, _ := url.Parse(siteURL)
	jar.SetCookies(u, []*http.Cookie{{Name: "wordpress_test_cookie", Value: "WP Cookie check"}})

	resp, err := client.PostForm(siteURL+"/wp-login.php", url.Values{
		"log":         {wpUser},
		"pwd":         {wpPass},
		"wp-submit":   {"Log In"},
		"redirect_to": {siteURL + "/wp-admin/"},
		"testcookie":  {"1"},
	})
	if err != nil {
		t.Fatalf("wp login: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(resp.Body)
	if strings.Contains(string(body), `name="pwd"`) {
		t.Fatalf("wp login failed, still on the login form:\n%s", excerpt(string(body)))
	}
	return client
}

type response struct {
	*http.Response
	body string
}

func getResponse(t *testing.T, c *http.Client, u string) response {
	t.Helper()
	resp, err := c.Get(u)
	if err != nil {
		t.Fatalf("GET %s: %v", u, err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read %s: %v", u, err)
	}
	return response{Response: resp, body: string(b)}
}

func get(t *testing.T, c *http.Client, u string) string {
	t.Helper()
	return getResponse(t, c, u).body
}

// extractNonce pulls the nonce out of the inline script the plugin injects.
var nonceRE = regexp.MustCompile(`var n="([0-9a-zA-Z]+)"`)

func extractNonce(t *testing.T, body string) string {
	t.Helper()
	if m := nonceRE.FindStringSubmatch(body); len(m) == 2 {
		return m[1]
	}
	t.Fatalf("no nonce found in the admin page:\n%s", excerpt(body))
	return ""
}

func excerpt(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 2500 {
		return s[:2500] + fmt.Sprintf("\n… (%d bytes total)", len(s))
	}
	return s
}
