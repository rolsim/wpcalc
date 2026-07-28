// Package wpcalc embeds the manuals and the WordPress plugin so the binary
// carries them without needing the source tree beside it.
//
// This package exists at the module root purely so //go:embed can reach docs/
// and wordpress/. An embed directive cannot use "..", and keeping those where
// they are is worth a one-file package: that is where a reader expects to find
// them, and where GitHub renders them.
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

// Plugin holds the WordPress plugin's PHP sources.
//
// The pattern is *.php rather than the directory, deliberately. The plugin
// directory also holds bin/wpcalc during an e2e run, and embedding the
// directory would embed a copy of this binary inside itself — a build that
// doubles in size for no reason and grows again on every rebuild.
//
//go:embed wordpress/wpcalc/*.php
var Plugin embed.FS

// PluginRoot is the prefix every path inside Plugin carries.
const PluginRoot = "wordpress/wpcalc"

// PluginDirName is the directory the plugin must live in under
// wp-content/plugins. WordPress does not require the folder to match the file,
// but every convention and every set of instructions assumes it does.
const PluginDirName = "wpcalc"
