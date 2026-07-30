// Package specdoc serves an OpenAPI 3.1 document as YAML, JSON, and an
// interactive Swagger UI page — the same logic wpcalc's two specs (the
// HTML app's and /api/v1's) both need, kept in one place rather than
// copied.
package specdoc

import (
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
)

//go:embed spec.html.tmpl
var templateSrc string

var htmlTemplate = template.Must(template.New("spec.html.tmpl").Parse(templateSrc))

// uiAssetsFS holds a pinned, unmodified copy of Swagger UI's dist/ output
// (Apache-2.0; see vendor/swagger-ui/LICENSE and VERSION.txt for the exact
// version and upgrade instructions). Vendored rather than loaded from a
// CDN so the page still renders with no outbound network access, matching
// the rest of wpcalc shipping as one self-contained binary.
//
//go:embed vendor/swagger-ui/*.js vendor/swagger-ui/*.css
var uiAssetsFS embed.FS

// AssetsHandler serves the vendored Swagger UI JS/CSS. Callers mount it at
// a path of their choosing; ServeHTML's page references those assets with
// a relative "openapi-assets/..." URL, so the mount point just needs to be
// the sibling of wherever ServeHTML itself is served from (e.g. ServeHTML
// at "/openapi.html" pairs with AssetsHandler at "/openapi-assets/").
func AssetsHandler() http.Handler {
	sub, err := fs.Sub(uiAssetsFS, "vendor/swagger-ui")
	if err != nil {
		panic("specdoc: vendored swagger-ui assets missing: " + err.Error())
	}
	return http.FileServerFS(sub)
}

// Doc is a parsed OpenAPI document, pre-rendered once at Parse time into
// every format it is served in — a request never re-parses or
// re-renders.
type Doc struct {
	doc  *openapi3.T
	yaml []byte
	json []byte
	html []byte
}

// Parse reads an OpenAPI 3.1 YAML document and prepares it for serving.
// Call it once, typically from a package-level var backed by an embedded
// file.
func Parse(yamlBytes []byte) (*Doc, error) {
	doc, err := openapi3.NewLoader().LoadFromData(yamlBytes)
	if err != nil {
		return nil, fmt.Errorf("specdoc: parse: %w", err)
	}
	j, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("specdoc: marshal to json: %w", err)
	}
	html, err := renderHTML(doc)
	if err != nil {
		return nil, fmt.Errorf("specdoc: render html: %w", err)
	}
	return &Doc{doc: doc, yaml: yamlBytes, json: j, html: []byte(html)}, nil
}

// OpenAPI3 returns the parsed document, for callers that build a
// request/response validator or router from it rather than just serving it
// — reusing the same parse rather than loading the spec a second time.
// Callers must treat this as read-only: it is shared, not copied.
func (d *Doc) OpenAPI3() *openapi3.T { return d.doc }

// MustParse is Parse, panicking on error — for the common case of parsing
// an embedded spec at package init, where a failure can only mean the
// checked-in file is broken.
func MustParse(yamlBytes []byte) *Doc {
	d, err := Parse(yamlBytes)
	if err != nil {
		panic(err)
	}
	return d
}

// ServeYAML writes back exactly the bytes Parse was given — the same file
// that generated any code built from it, not a re-serialization, so a
// served copy can never drift from what actually generated the server.
func (d *Doc) ServeYAML(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
	_, _ = w.Write(d.yaml)
}

// ServeJSON writes the same document, parsed and re-encoded — what
// ServeHTML's Swagger UI page fetches to render itself, and what a
// codegen tool or Postman would import.
func (d *Doc) ServeJSON(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write(d.json)
}

// ServeHTML writes an interactive Swagger UI page — rendered once at Parse
// time, so a request never re-renders the shell — that fetches ServeJSON's
// output client-side and lets a caller browse every operation and, via the
// "Authorize" button using the spec's own declared security scheme, send
// real requests against the live server. Pair with a mount of
// AssetsHandler at the sibling "openapi-assets/" path.
func (d *Doc) ServeHTML(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(d.html)
}

func renderHTML(doc *openapi3.T) (string, error) {
	var buf strings.Builder
	err := htmlTemplate.Execute(&buf, struct {
		Title   string
		Version string
	}{
		Title:   doc.Info.Title,
		Version: doc.Info.Version,
	})
	return buf.String(), err
}
