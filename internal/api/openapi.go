package api

import (
	"encoding/json"
	"net/http"

	"gopkg.in/yaml.v3"

	claudebox "github.com/vutran1710/claudebox"
)

// The API describes itself.
//
// The document is openapi.yaml at the project root — visible on arrival,
// rather than buried where only a running server would reveal it. A test
// checks it against the route table, because a description that has drifted
// from the server is worse than none: it is believed.
//
// Both endpoints sit behind the bearer key. Once a box is exposed through a
// tunnel, an unauthenticated document would tell the internet exactly what is
// listening; anybody entitled to call the API has a key already.

func (s *Server) openAPIYAML(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/yaml")
	w.Write(claudebox.OpenAPI)
}

func (s *Server) openAPIJSON(w http.ResponseWriter, _ *http.Request) {
	doc, err := OpenAPIDocument()
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, doc)
}

// OpenAPIDocument parses the shipped description. Exported so a test can hold
// the server to it.
func OpenAPIDocument() (map[string]any, error) {
	var doc map[string]any
	if err := yaml.Unmarshal(claudebox.OpenAPI, &doc); err != nil {
		return nil, err
	}
	// yaml.v3 decodes into map[string]any already, but nested maps from other
	// decoders can arrive keyed by any; round-tripping through JSON keeps the
	// served document valid JSON whatever the YAML held.
	raw, err := json.Marshal(doc)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out, nil
}
