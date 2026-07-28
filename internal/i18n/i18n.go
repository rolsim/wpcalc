// Package i18n resolves user-facing strings from embedded JSON catalogs.
//
// Every string the user sees goes through T from the first line of template
// code. The catalogs themselves can grow later — adding a locale is a
// translation job — but retrofitting the call sites would mean reopening every
// template, which is the expensive half and the reason this exists at P0.
package i18n

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"golang.org/x/text/language"
)

//go:embed catalogs/*.json
var catalogsFS embed.FS

// DefaultLang is the fallback and the locale the grid is designed around.
const DefaultLang = "de-CH"

// Bundle holds every loaded catalog.
type Bundle struct {
	msgs    map[string]map[string]string
	tags    []language.Tag
	names   []string
	matcher language.Matcher
}

// New loads every embedded catalog.
func New() (*Bundle, error) {
	entries, err := fs.Glob(catalogsFS, "catalogs/*.json")
	if err != nil {
		return nil, fmt.Errorf("i18n: list catalogs: %w", err)
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("i18n: no catalogs embedded")
	}

	b := &Bundle{msgs: make(map[string]map[string]string)}
	for _, name := range entries {
		raw, err := catalogsFS.ReadFile(name)
		if err != nil {
			return nil, fmt.Errorf("i18n: read %s: %w", name, err)
		}
		var msgs map[string]string
		if err := json.Unmarshal(raw, &msgs); err != nil {
			return nil, fmt.Errorf("i18n: parse %s: %w", name, err)
		}
		lang := strings.TrimSuffix(filepath.Base(name), ".json")
		tag, err := language.Parse(lang)
		if err != nil {
			return nil, fmt.Errorf("i18n: %s is not a language tag: %w", name, err)
		}
		b.msgs[lang] = msgs
		b.names = append(b.names, lang)
		b.tags = append(b.tags, tag)
	}

	if _, ok := b.msgs[DefaultLang]; !ok {
		return nil, fmt.Errorf("i18n: default catalog %s is missing", DefaultLang)
	}

	// Put the default first so language.NewMatcher prefers it on a tie.
	sort.SliceStable(b.tags, func(i, j int) bool { return b.tags[i].String() == DefaultLang })
	sort.Strings(b.names)
	b.matcher = language.NewMatcher(b.tags)
	return b, nil
}

// Languages lists the loaded locales, sorted.
func (b *Bundle) Languages() []string { return append([]string(nil), b.names...) }

// Has reports whether a locale is loaded.
func (b *Bundle) Has(lang string) bool { _, ok := b.msgs[lang]; return ok }

// Match negotiates a locale from an Accept-Language header, falling back to
// the default. Language selection is P3 work; the plumbing is here so that
// adding the second catalog does not require touching handlers.
func (b *Bundle) Match(acceptLanguage string) string {
	if acceptLanguage == "" {
		return DefaultLang
	}
	tags, _, err := language.ParseAcceptLanguage(acceptLanguage)
	if err != nil {
		return DefaultLang
	}
	_, idx, conf := b.matcher.Match(tags...)
	if conf == language.No || idx < 0 || idx >= len(b.tags) {
		return DefaultLang
	}
	name := b.tags[idx].String()
	if b.Has(name) {
		return name
	}
	return DefaultLang
}

// T resolves a key. Extra arguments are applied with fmt.Sprintf, so a catalog
// entry may carry verbs.
//
// A missing key renders as "!!key!!" rather than as an empty string: a blank
// label looks like a styling bug and survives review, whereas the marker is
// impossible to miss. Tests assert no such marker can be produced.
func (b *Bundle) T(lang, key string, args ...any) string {
	msg, ok := b.lookup(lang, key)
	if !ok {
		return "!!" + key + "!!"
	}
	if len(args) == 0 {
		return msg
	}
	return fmt.Sprintf(msg, args...)
}

func (b *Bundle) lookup(lang, key string) (string, bool) {
	if msgs, ok := b.msgs[lang]; ok {
		if msg, ok := msgs[key]; ok && msg != "" {
			return msg, true
		}
	}
	if lang != DefaultLang {
		if msg, ok := b.msgs[DefaultLang][key]; ok && msg != "" {
			return msg, true
		}
	}
	return "", false
}

// Keys lists every key defined in a catalog, sorted.
func (b *Bundle) Keys(lang string) []string {
	out := make([]string, 0, len(b.msgs[lang]))
	for k := range b.msgs[lang] {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// WeekdayKey maps a weekday to its catalog key. Weekday and month names come
// from the catalog rather than from time.Time's English names, which would be
// the one place untranslated text leaked into an otherwise localised grid.
func WeekdayKey(w time.Weekday) string {
	return [...]string{
		"weekday.sun", "weekday.mon", "weekday.tue", "weekday.wed",
		"weekday.thu", "weekday.fri", "weekday.sat",
	}[int(w)%7]
}

// MonthKey maps a month to its catalog key.
func MonthKey(m time.Month) string { return fmt.Sprintf("month.%d", int(m)) }

// Printer binds a locale so templates can call T with just a key.
type Printer struct {
	bundle *Bundle
	lang   string
}

// For returns a Printer for the given locale.
func (b *Bundle) For(lang string) *Printer {
	if !b.Has(lang) {
		lang = DefaultLang
	}
	return &Printer{bundle: b, lang: lang}
}

// T resolves a key in the bound locale.
func (p *Printer) T(key string, args ...any) string { return p.bundle.T(p.lang, key, args...) }

// Lang is the bound locale.
func (p *Printer) Lang() string { return p.lang }

// DecimalSep is the separator this locale writes hours with. de-CH uses a
// dot for decimals in this context (7.75 h), matching payroll convention.
func (p *Printer) DecimalSep() string { return "." }
