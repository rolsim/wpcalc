//go:build e2e

// Package standalone_test drives the standalone server in a real browser.
//
// Tagged out of `go test ./...` because it needs Docker. Run with `make e2e`.
//
// Chrome runs in a container on the host network, so "localhost" means the
// same thing to the browser and to the test. This is the only place the
// JavaScript enhancement is exercised at all: every handler test covers the
// no-JS path, which is the one that must work, but nothing else proves that
// app.js updates the totals it claims to.
package standalone_test

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

const (
	chromeImage     = "chromedp/headless-shell:latest"
	chromeContainer = "wpcalc-e2e-chrome"
	chromePort      = 9222
	adminUser       = "e2e"
	adminPass       = "an-e2e-password-long"
)

func projectRoot(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		t.Fatalf("locate repo root: %v", err)
	}
	return strings.TrimSpace(string(out))
}

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	return ln.Addr().(*net.TCPAddr).Port
}

// startChrome runs headless Chrome in a container and returns its DevTools URL.
//
// Host networking is what lets the browser reach a server bound on the host's
// loopback; with the default bridge, "localhost" inside the container is the
// container itself and every navigation would fail with a connection error
// that looks like the app is broken.
func startChrome(t *testing.T) string {
	t.Helper()

	_ = exec.Command("docker", "rm", "-f", chromeContainer).Run()
	t.Cleanup(func() { _ = exec.Command("docker", "rm", "-f", chromeContainer).Run() })

	// No --remote-debugging-* flags here. The image's entrypoint already runs
	// Chrome on 9223 and forwards 9222 to it with socat; overriding the port
	// puts Chrome where socat is not looking, and the only symptom is a
	// minute of "connection refused" from a proxy nobody mentioned.
	cmd := exec.Command("docker", "run", "-d", "--rm",
		"--name", chromeContainer,
		"--network", "host",
		"--shm-size", "1g",
		chromeImage)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("cannot start %s (%v); skipping browser e2e\n%s", chromeImage, err, out)
	}

	devtools := fmt.Sprintf("http://127.0.0.1:%d/json/version", chromePort)
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		if resp, err := http.Get(devtools); err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == 200 {
				return fmt.Sprintf("ws://127.0.0.1:%d/", chromePort)
			}
		}
		time.Sleep(time.Second)
	}
	logs, _ := exec.Command("docker", "logs", chromeContainer).CombinedOutput()
	t.Fatalf("Chrome did not expose DevTools within 60s\n%s", logs)
	return ""
}

// startServer builds and runs wpcalc with a seeded database.
func startServer(t *testing.T, root string) string {
	t.Helper()

	dir := t.TempDir()
	bin := filepath.Join(dir, "wpcalc")
	db := filepath.Join(dir, "e2e.db")

	build := exec.Command("go", "build", "-o", bin, "./cmd/wpcalc")
	build.Dir = root
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}

	seed := exec.Command(bin, "sample-employees", "--db", db, "--month", "2026-07")
	if out, err := seed.CombinedOutput(); err != nil {
		t.Fatalf("sample-employees: %v\n%s", err, out)
	}

	add := exec.Command(bin, "user", "add", adminUser, "-role", "admin", "--db", db)
	add.Stdin = strings.NewReader(adminPass + "\n")
	if out, err := add.CombinedOutput(); err != nil {
		t.Fatalf("user add: %v\n%s", err, out)
	}

	port := freePort(t)
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	srv := exec.Command(bin, "serve", "--addr", addr, "--db", db)
	srv.Stdout, srv.Stderr = os.Stderr, os.Stderr
	if err := srv.Start(); err != nil {
		t.Fatalf("serve: %v", err)
	}
	t.Cleanup(func() {
		_ = srv.Process.Kill()
		_, _ = srv.Process.Wait()
	})

	base := "http://" + addr
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if resp, err := http.Get(base + "/healthz"); err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == 200 {
				return base
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatal("server did not become healthy")
	return ""
}

func TestBrowserEntersHoursAndTotalsUpdate(t *testing.T) {
	root := projectRoot(t)
	wsURL := startChrome(t)
	base := startServer(t, root)

	allocCtx, cancelAlloc := chromedp.NewRemoteAllocator(context.Background(), wsURL)
	defer cancelAlloc()
	ctx, cancel := chromedp.NewContext(allocCtx)
	defer cancel()
	ctx, cancelTimeout := context.WithTimeout(ctx, 2*time.Minute)
	defer cancelTimeout()

	// ---- log in --------------------------------------------------------

	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/login"),
		chromedp.WaitVisible(`input[name="password"]`, chromedp.ByQuery),
		// SetValue rather than SendKeys: SendKeys appends, so any default the
		// field carries becomes a prefix of the credential. That is exactly
		// how this test first failed, against a stale value="admin".
		chromedp.SetValue(`input[name="username"]`, adminUser, chromedp.ByQuery),
		chromedp.SetValue(`input[name="password"]`, adminPass, chromedp.ByQuery),
		chromedp.Click(`button[type="submit"]`, chromedp.ByQuery),
		chromedp.WaitVisible(`table.grid`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("login: %v", err)
	}

	// ---- the seeded month renders --------------------------------------

	var heading, grandBefore string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/m/2026-07"),
		chromedp.WaitVisible(`table.grid`, chromedp.ByQuery),
		chromedp.Text(`.month-nav h1`, &heading, chromedp.ByQuery),
		chromedp.Text(`[data-grand-total]`, &grandBefore, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("load grid: %v", err)
	}
	if !strings.Contains(heading, "Juli 2026") {
		t.Errorf("heading = %q, want July 2026 in German", heading)
	}
	if strings.TrimSpace(grandBefore) == "" {
		t.Fatal("grand total is empty on the seeded month")
	}

	// ---- entering hours updates the accumulators without a reload ------

	// Pick the first editable cell that is currently empty and type into it.
	var employeeID, date, picked string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`
		(() => {
			const i = [...document.querySelectorAll('input.hours')].find(x => x.value === '');
			if (!i) return '';
			i.focus(); i.value = '3,25';
			return i.dataset.employee + '|' + i.dataset.date;
		})()
	`, &picked)); err != nil {
		t.Fatalf("fill a cell: %v", err)
	}
	if picked == "" {
		t.Skip("the seeded month has no empty editable cell to type into")
	}
	parts := strings.SplitN(picked, "|", 2)
	employeeID, date = parts[0], parts[1]

	// Blur is what app.js listens for; this is the real interaction.
	var grandAfter, dayAfter string
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.querySelector('input.hours[data-employee="`+employeeID+`"][data-date="`+date+`"]').blur()`, nil),
		chromedp.Sleep(1500*time.Millisecond),
		chromedp.Text(`[data-grand-total]`, &grandAfter, chromedp.ByQuery),
		chromedp.Text(`[data-day-total="`+date+`"]`, &dayAfter, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("submit the cell: %v", err)
	}

	if strings.TrimSpace(grandAfter) == strings.TrimSpace(grandBefore) {
		t.Errorf("grand total did not change after entering hours: still %q", grandAfter)
	}
	// The comma the user typed must come back as the canonical rendering.
	if !strings.Contains(dayAfter, ".") {
		t.Errorf("day total %q is not formatted with the locale separator", dayAfter)
	}

	// And it survives a reload, which proves it reached the database rather
	// than only the DOM.
	var reloaded string
	if err := chromedp.Run(ctx,
		chromedp.Reload(),
		chromedp.WaitVisible(`table.grid`, chromedp.ByQuery),
		chromedp.Value(`input.hours[data-employee="`+employeeID+`"][data-date="`+date+`"]`, &reloaded, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("reload: %v", err)
	}
	if reloaded != "3.25" {
		t.Errorf("after reload the cell reads %q, want 3.25", reloaded)
	}
}

func TestBrowserNavigatesMonthsAcrossYearBoundary(t *testing.T) {
	root := projectRoot(t)
	wsURL := startChrome(t)
	base := startServer(t, root)

	allocCtx, cancelAlloc := chromedp.NewRemoteAllocator(context.Background(), wsURL)
	defer cancelAlloc()
	ctx, cancel := chromedp.NewContext(allocCtx)
	defer cancel()
	ctx, cancelTimeout := context.WithTimeout(ctx, 2*time.Minute)
	defer cancelTimeout()

	var heading string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/login"),
		chromedp.WaitVisible(`input[name="password"]`, chromedp.ByQuery),
		chromedp.SetValue(`input[name="username"]`, adminUser, chromedp.ByQuery),
		chromedp.SetValue(`input[name="password"]`, adminPass, chromedp.ByQuery),
		chromedp.Click(`button[type="submit"]`, chromedp.ByQuery),
		chromedp.WaitVisible(`table.grid`, chromedp.ByQuery),

		chromedp.Navigate(base+"/m/2026-12"),
		chromedp.WaitVisible(`.month-nav`, chromedp.ByQuery),
		chromedp.Click(`a[rel="next"]`, chromedp.ByQuery),
		chromedp.WaitVisible(`.month-nav`, chromedp.ByQuery),
		chromedp.Text(`.month-nav h1`, &heading, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("navigate: %v", err)
	}

	if !strings.Contains(heading, "Januar 2027") {
		t.Errorf("after next from December 2026 the heading is %q, want January 2027", heading)
	}
}
