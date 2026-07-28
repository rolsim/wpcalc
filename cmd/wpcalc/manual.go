package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"sort"
	"strings"

	wpcalc "source.simonet.internal/rolsim/wpcalc"
	"source.simonet.internal/rolsim/wpcalc/internal/i18n"
)

// cmdManual prints an embedded manual, rendered with glow when it is available.
//
// The markdown is embedded rather than read from disk so the manual travels
// with the binary — the WordPress sidecar and a copied-to-a-server binary have
// no source tree beside them.
//
// glow is used when it is on PATH and the output is a terminal, and the raw
// markdown is printed otherwise. That covers the two cases that matter: a
// person reading it, and a pipe into a file or another program, where ANSI
// escapes would be corruption rather than formatting.
func cmdManual(_ context.Context, args []string) error {
	// Named flags, not fs: io/fs is imported here and shadowing a package with
	// a local is how a later edit picks the wrong one silently.
	flags := flag.NewFlagSet("manual", flag.ContinueOnError)
	lang := flags.String("lang", defaultManualLang(), "language: de-CH or en")
	raw := flags.Bool("raw", false, "print markdown as-is, without rendering")
	list := flags.Bool("list", false, "list the available manuals and exit")

	positional, err := parseArgs(flags, args)
	if err != nil {
		return err
	}

	if *list {
		return listManuals(os.Stdout)
	}

	topic := "user"
	if len(positional) > 0 {
		topic = strings.ToLower(positional[0])
	}

	md, err := readManual(*lang, topic)
	if err != nil {
		return err
	}

	return renderManual(md, *raw)
}

// readManual loads one manual, falling back to the default language rather
// than failing when a locale has no translation of that topic yet.
func readManual(lang, topic string) ([]byte, error) {
	if !strings.HasSuffix(topic, ".md") {
		topic += ".md"
	}
	// Reject anything that could climb out of the embedded tree. The input is
	// a command-line argument, so this is cheap insurance rather than a
	// realistic attack, but path.Clean on an unvalidated join is a habit worth
	// not forming.
	if strings.ContainsAny(lang, "/\\") || strings.ContainsAny(topic, "/\\") {
		return nil, fmt.Errorf("manual: invalid name %q/%q", lang, topic)
	}

	md, err := wpcalc.Manuals.ReadFile(path.Join(wpcalc.ManualRoot, lang, topic))
	if err == nil {
		return md, nil
	}

	if lang != i18n.DefaultLang {
		if md, fallbackErr := wpcalc.Manuals.ReadFile(
			path.Join(wpcalc.ManualRoot, i18n.DefaultLang, topic)); fallbackErr == nil {
			fmt.Fprintf(os.Stderr, "no %s manual in %s; showing %s\n",
				strings.TrimSuffix(topic, ".md"), lang, i18n.DefaultLang)
			return md, nil
		}
	}

	var have strings.Builder
	_ = listManuals(&have)
	return nil, fmt.Errorf("manual: no %q manual in %q.\n\n%s",
		strings.TrimSuffix(topic, ".md"), lang, have.String())
}

// renderManual writes the manual, through glow when that makes sense.
func renderManual(md []byte, raw bool) error {
	if raw || !isTerminal(os.Stdout) {
		_, err := os.Stdout.Write(md)
		return err
	}

	glow, err := exec.LookPath("glow")
	if err != nil {
		// Not an error: markdown is readable on its own. Say so once, on
		// stderr, so the hint never lands in a redirected file.
		fmt.Fprintln(os.Stderr,
			"note: install glow (https://github.com/charmbracelet/glow) for a rendered manual")
		_, werr := os.Stdout.Write(md)
		return werr
	}

	// "-" reads from stdin, "-p" pages. Paging is what makes a long manual
	// usable in a terminal, and glow falls back gracefully where it cannot.
	cmd := exec.Command(glow, "-p", "-")
	cmd.Stdin = strings.NewReader(string(md))
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return fmt.Errorf("manual: glow exited unsuccessfully: %w", err)
		}
		return fmt.Errorf("manual: run glow: %w", err)
	}
	return nil
}

// listManuals writes what is embedded, grouped by language.
func listManuals(w io.Writer) error {
	byLang := map[string][]string{}
	err := fs.WalkDir(wpcalc.Manuals, wpcalc.ManualRoot, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".md") {
			return err
		}
		lang := path.Base(path.Dir(p))
		byLang[lang] = append(byLang[lang], strings.TrimSuffix(path.Base(p), ".md"))
		return nil
	})
	if err != nil {
		return err
	}

	langs := make([]string, 0, len(byLang))
	for l := range byLang {
		langs = append(langs, l)
	}
	sort.Strings(langs)

	fmt.Fprintln(w, "Available manuals:")
	for _, l := range langs {
		topics := byLang[l]
		sort.Strings(topics)
		marker := " "
		if l == i18n.DefaultLang {
			marker = "*"
		}
		fmt.Fprintf(w, "  %s %-8s %s\n", marker, l, strings.Join(topics, ", "))
	}
	fmt.Fprintf(w, "\n  * default    e.g. `wpcalc manual admin --lang en`\n")
	return nil
}

// defaultManualLang picks a language from the environment, so a Swiss-German
// shell gets the Swiss-German manual without being told to ask for it.
func defaultManualLang() string {
	for _, key := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		v := os.Getenv(key)
		if v == "" {
			continue
		}
		// "de_CH.UTF-8" -> "de"
		tag := strings.ToLower(v)
		if i := strings.IndexAny(tag, "_.@"); i >= 0 {
			tag = tag[:i]
		}
		switch tag {
		case "de":
			return i18n.DefaultLang
		case "en":
			return "en"
		}
	}
	return i18n.DefaultLang
}

// isTerminal reports whether f is attached to a terminal.
func isTerminal(f *os.File) bool {
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}
