package main

import (
	"io/fs"
	"path"
	"strings"
	"testing"

	wpcalc "github.com/rolsim/wpcalc"
	"github.com/rolsim/wpcalc/internal/i18n"
)

// TestEveryCatalogHasAManual keeps the manuals and the interface languages in
// step. A locale that the application can render its own UI in but has no
// manual for is a gap nobody notices until someone asks for the manual.
func TestEveryCatalogHasAManual(t *testing.T) {
	bundle, err := i18n.New()
	if err != nil {
		t.Fatal(err)
	}

	for _, lang := range bundle.Languages() {
		for _, topic := range []string{"user", "admin"} {
			p := path.Join(wpcalc.ManualRoot, lang, topic+".md")
			if _, err := wpcalc.Manuals.ReadFile(p); err != nil {
				t.Errorf("catalog %s has no %s manual at %s", lang, topic, p)
			}
		}
	}
}

func TestManualsAreEmbeddedAndNonTrivial(t *testing.T) {
	// An embed directive that silently matches nothing produces an empty FS
	// and a command that prints nothing.
	var count int
	err := fs.WalkDir(wpcalc.Manuals, wpcalc.ManualRoot, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		count++
		body, err := wpcalc.Manuals.ReadFile(p)
		if err != nil {
			return err
		}
		if len(body) < 500 {
			t.Errorf("%s is only %d bytes; suspiciously short for a manual", p, len(body))
		}
		if !strings.HasPrefix(string(body), "# ") {
			t.Errorf("%s does not start with a top-level heading", p)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if count < 4 {
		t.Errorf("embedded %d manuals, want at least 4 (user+admin in two languages)", count)
	}
}

func TestReadManualFindsBothTopicsAndLanguages(t *testing.T) {
	for _, lang := range []string{"de-CH", "en"} {
		for _, topic := range []string{"user", "admin"} {
			md, err := readManual(lang, topic)
			if err != nil {
				t.Errorf("readManual(%q, %q): %v", lang, topic, err)
				continue
			}
			if len(md) == 0 {
				t.Errorf("readManual(%q, %q) returned nothing", lang, topic)
			}
		}
	}

	// The ".md" suffix is optional at the command line.
	withSuffix, err := readManual("en", "user.md")
	if err != nil {
		t.Fatal(err)
	}
	without, err := readManual("en", "user")
	if err != nil {
		t.Fatal(err)
	}
	if string(withSuffix) != string(without) {
		t.Error("`user` and `user.md` resolved to different documents")
	}
}

func TestReadManualFallsBackToTheDefaultLanguage(t *testing.T) {
	// An unsupported locale should still get a manual rather than an error.
	md, err := readManual("fr-CH", "user")
	if err != nil {
		t.Fatalf("readManual for an unsupported locale: %v", err)
	}
	want, err := readManual(i18n.DefaultLang, "user")
	if err != nil {
		t.Fatal(err)
	}
	if string(md) != string(want) {
		t.Error("fallback did not return the default-language manual")
	}
}

func TestReadManualRejectsUnknownTopicAndPathEscapes(t *testing.T) {
	if _, err := readManual("en", "nonsense"); err == nil {
		t.Error("an unknown topic returned a manual")
	} else if !strings.Contains(err.Error(), "Available manuals") {
		t.Errorf("the error does not say what exists: %v", err)
	}

	// The topic and language come from the command line, so a path that climbs
	// out of the embedded tree must be refused rather than joined blindly.
	for _, c := range []struct{ lang, topic string }{
		{"en", "../../go"},
		{"../..", "user"},
		{"en", "..\\..\\go"},
	} {
		if _, err := readManual(c.lang, c.topic); err == nil {
			t.Errorf("readManual(%q, %q) succeeded; want rejection", c.lang, c.topic)
		}
	}
}

func TestListManualsNamesEveryLanguage(t *testing.T) {
	var sb strings.Builder
	if err := listManuals(&sb); err != nil {
		t.Fatal(err)
	}
	out := sb.String()
	for _, want := range []string{"de-CH", "en", "user", "admin"} {
		if !strings.Contains(out, want) {
			t.Errorf("listing omits %q:\n%s", want, out)
		}
	}
	// The default is marked, so a reader knows which they get without --lang.
	if !strings.Contains(out, "* "+i18n.DefaultLang) {
		t.Errorf("listing does not mark %s as the default:\n%s", i18n.DefaultLang, out)
	}
}

func TestDefaultManualLangFollowsTheEnvironment(t *testing.T) {
	cases := map[string]string{
		"de_CH.UTF-8": "de-CH",
		"de_DE.UTF-8": "de-CH", // only one German catalog exists
		"en_GB.UTF-8": "en",
		"en_US":       "en",
		"fr_CH.UTF-8": i18n.DefaultLang, // unsupported -> default
		"":            i18n.DefaultLang,
		"C":           i18n.DefaultLang,
	}
	for value, want := range cases {
		t.Setenv("LC_ALL", "")
		t.Setenv("LC_MESSAGES", "")
		t.Setenv("LANG", value)
		if got := defaultManualLang(); got != want {
			t.Errorf("LANG=%q -> %q, want %q", value, got, want)
		}
	}

	// LC_ALL wins over LANG, as it does everywhere else.
	t.Setenv("LANG", "de_CH.UTF-8")
	t.Setenv("LC_ALL", "en_GB.UTF-8")
	if got := defaultManualLang(); got != "en" {
		t.Errorf("LC_ALL did not take precedence over LANG: got %q", got)
	}
}
