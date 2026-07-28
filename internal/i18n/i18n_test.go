package i18n

import (
	"strings"
	"testing"
	"time"
)

func newBundle(t *testing.T) *Bundle {
	t.Helper()
	b, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return b
}

func TestMissingTranslationKeyFails(t *testing.T) {
	// A missing string must be loud. Returning "" would render as a blank
	// label that looks like a CSS problem and survives review; the marker is
	// impossible to miss and is what the completeness tests below grep for.
	b := newBundle(t)

	got := b.T(DefaultLang, "no.such.key")
	if !strings.Contains(got, "no.such.key") || !strings.HasPrefix(got, "!!") {
		t.Errorf("T on a missing key = %q, want a visible !!marker!!", got)
	}

	// An entry present but empty counts as missing, not as an intentional blank.
	b.msgs[DefaultLang]["deliberately.empty"] = ""
	if got := b.T(DefaultLang, "deliberately.empty"); !strings.HasPrefix(got, "!!") {
		t.Errorf("T on an empty value = %q, want a visible marker", got)
	}
}

func TestNoCatalogValueIsEmpty(t *testing.T) {
	b := newBundle(t)
	for _, lang := range b.Languages() {
		for _, key := range b.Keys(lang) {
			if strings.TrimSpace(b.msgs[lang][key]) == "" {
				t.Errorf("catalog %s: key %q is empty", lang, key)
			}
		}
	}
}

func TestAllCatalogsDefineTheSameKeys(t *testing.T) {
	// With one catalog this is trivially true. It is here so that adding `en`
	// at P3 cannot land half-translated without the build noticing.
	b := newBundle(t)
	langs := b.Languages()
	if len(langs) < 2 {
		t.Skipf("only %d catalog(s) loaded; nothing to compare yet", len(langs))
	}
	want := b.Keys(DefaultLang)
	for _, lang := range langs {
		if lang == DefaultLang {
			continue
		}
		got := make(map[string]bool)
		for _, k := range b.Keys(lang) {
			got[k] = true
		}
		for _, k := range want {
			if !got[k] {
				t.Errorf("catalog %s is missing key %q", lang, k)
			}
			delete(got, k)
		}
		for k := range got {
			t.Errorf("catalog %s defines key %q that %s does not", lang, k, DefaultLang)
		}
	}
}

func TestEveryWeekdayAndMonthResolves(t *testing.T) {
	// These are generated keys rather than literals in a template, so nothing
	// else would catch a gap in them.
	b := newBundle(t)
	for w := time.Sunday; w <= time.Saturday; w++ {
		if got := b.T(DefaultLang, WeekdayKey(w)); strings.HasPrefix(got, "!!") {
			t.Errorf("weekday %s unresolved: %s", w, got)
		}
	}
	for m := time.January; m <= time.December; m++ {
		if got := b.T(DefaultLang, MonthKey(m)); strings.HasPrefix(got, "!!") {
			t.Errorf("month %s unresolved: %s", m, got)
		}
	}
}

func TestWeekdayKeyMapsCorrectly(t *testing.T) {
	cases := map[time.Weekday]string{
		time.Monday:   "weekday.mon",
		time.Saturday: "weekday.sat",
		time.Sunday:   "weekday.sun",
	}
	for w, want := range cases {
		if got := WeekdayKey(w); got != want {
			t.Errorf("WeekdayKey(%s) = %q, want %q", w, got, want)
		}
	}
}

func TestDefaultCatalogIsSwissGerman(t *testing.T) {
	b := newBundle(t)
	if !b.Has(DefaultLang) {
		t.Fatalf("default catalog %s not loaded; have %v", DefaultLang, b.Languages())
	}
	// Swiss German writes ss, never ß. One eszett anywhere means a de-DE
	// string was pasted in.
	for _, lang := range b.Languages() {
		if lang != "de-CH" {
			continue
		}
		for _, key := range b.Keys(lang) {
			if strings.Contains(b.msgs[lang][key], "ß") {
				t.Errorf("de-CH key %q contains ß; Swiss German uses ss", key)
			}
		}
	}
}

func TestFormattingArguments(t *testing.T) {
	b := newBundle(t)
	got := b.T(DefaultLang, "report.generated_at", "28.07.2026")
	if !strings.Contains(got, "28.07.2026") {
		t.Errorf("T with argument = %q, want the date interpolated", got)
	}
	if strings.Contains(got, "%s") {
		t.Errorf("T left an unformatted verb in %q", got)
	}
}

func TestMatchFallsBackToDefault(t *testing.T) {
	b := newBundle(t)
	for _, accept := range []string{"", "xx-YY", "not a header", "ja-JP,ja;q=0.9"} {
		if got := b.Match(accept); !b.Has(got) {
			t.Errorf("Match(%q) = %q, which is not a loaded catalog", accept, got)
		}
	}
	if got := b.Match("de-CH,de;q=0.9"); got != "de-CH" {
		t.Errorf("Match on de-CH = %q, want de-CH", got)
	}
}

func TestPrinterBindsLocale(t *testing.T) {
	b := newBundle(t)
	p := b.For(DefaultLang)
	if p.Lang() != DefaultLang {
		t.Errorf("Lang() = %q, want %q", p.Lang(), DefaultLang)
	}
	if got, want := p.T("app.title"), b.T(DefaultLang, "app.title"); got != want {
		t.Errorf("Printer.T = %q, Bundle.T = %q", got, want)
	}
	// An unknown locale degrades to the default rather than rendering markers.
	if got := b.For("xx-YY").Lang(); got != DefaultLang {
		t.Errorf("For(unknown).Lang() = %q, want %q", got, DefaultLang)
	}
}
