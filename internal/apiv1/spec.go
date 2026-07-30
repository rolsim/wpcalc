package apiv1

import (
	_ "embed"
	"net/http"

	"github.com/rolsim/wpcalc/internal/specdoc"
)

//go:embed openapi.yaml
var specYAML []byte

var spec = specdoc.MustParse(specYAML)

// ServeSpecYAML serves the exact bytes of internal/apiv1/openapi.yaml — the
// same file oapi-codegen generated server.gen.go from.
func ServeSpecYAML(w http.ResponseWriter, r *http.Request) { spec.ServeYAML(w, r) }

// ServeSpecJSON serves the same document, parsed and re-encoded as JSON.
func ServeSpecJSON(w http.ResponseWriter, r *http.Request) { spec.ServeJSON(w, r) }

// ServeSpecHTML serves a static, self-contained rendering of the document.
func ServeSpecHTML(w http.ResponseWriter, r *http.Request) { spec.ServeHTML(w, r) }
