// Package wpcalc embeds the manuals so the binary can display them without
// needing the source tree beside it.
//
// This package exists at the module root purely so //go:embed can reach docs/.
// An embed directive cannot use "..", and keeping the manuals under docs/ is
// worth a one-file package: that is where a reader expects to find them, and
// where GitHub renders them.
package wpcalc

import "embed"

// Manuals holds the user and administrator guides, one directory per language.
// The directory names match the i18n catalog names, so a new locale is a
// catalog plus a directory and nothing else.
//
//go:embed docs
var Manuals embed.FS

// ManualRoot is the prefix every path inside Manuals carries.
const ManualRoot = "docs"
