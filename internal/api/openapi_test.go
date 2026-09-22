package api

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// The document is only worth publishing if it is true. These hold the server
// to it in both directions, because a described endpoint that does not exist
// and an endpoint nobody described fail in opposite, equally annoying ways.

func specPaths(t *testing.T) map[string]bool {
	t.Helper()
	doc, err := OpenAPIDocument()
	if err != nil {
		t.Fatalf("the shipped openapi.yaml does not parse: %v", err)
	}
	paths, ok := doc["paths"].(map[string]any)
	if !ok {
		t.Fatal("the document has no paths")
	}
	got := map[string]bool{}
	for path, item := range paths {
		methods, ok := item.(map[string]any)
		if !ok {
			continue
		}
		for method := range methods {
			switch strings.ToUpper(method) {
			case "GET", "POST", "PUT", "DELETE", "PATCH":
				got[strings.ToUpper(method)+" "+path] = true
			}
		}
	}
	return got
}

func TestEveryRouteIsDescribed(t *testing.T) {
	described := specPaths(t)
	for _, r := range (&Server{}).routes() {
		if !described[r.Pattern] {
			t.Errorf("route %q is not in openapi.yaml", r.Pattern)
		}
	}
}

func TestEveryDescribedPathIsARoute(t *testing.T) {
	real := map[string]bool{}
	for _, r := range (&Server{}).routes() {
		real[r.Pattern] = true
	}
	for described := range specPaths(t) {
		if !real[described] {
			t.Errorf("openapi.yaml describes %q, which the server does not serve", described)
		}
	}
}

func TestOnlyHealthzIsPublic(t *testing.T) {
	// Everything else needs the key, including the document itself: once a box
	// is exposed through a tunnel, an unauthenticated description would tell
	// the internet exactly what is listening.
	for _, r := range (&Server{}).routes() {
		if r.Public && r.Pattern != "GET /healthz" {
			t.Errorf("%q is public", r.Pattern)
		}
	}
}

func TestTheDocumentIsServedBothWays(t *testing.T) {
	h := answering(t, answers("s", "hi"))
	for _, path := range []string{"/openapi.yaml", "/openapi.json"} {
		w := h.do("GET", path, nil)
		if w.Code != http.StatusOK {
			t.Errorf("%s: code = %d", path, w.Code)
		}
		if !strings.Contains(w.Body.String(), "ClaudeBox API") {
			t.Errorf("%s did not return the document", path)
		}
	}
}

func TestTheDocumentNeedsAKey(t *testing.T) {
	h := answering(t, answers("s", "hi"))
	r, _ := http.NewRequest("GET", "/openapi.json", nil)
	w := recorderFor(h, r)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("code = %d, want 401", w.Code)
	}
}

// --- the command spec over HTTP ---

func TestGetCommandsReturnsTheAllowlist(t *testing.T) {
	h := answering(t, answers("s", "hi"))
	got := h.json(h.do("GET", "/commands", nil))
	cmds, _ := got["commands"].([]any)
	if len(cmds) == 0 {
		t.Fatalf("no commands returned: %v", got)
	}
	if got["path"] != h.SpecPath {
		t.Errorf("path = %v, want %v", got["path"], h.SpecPath)
	}
}

func TestPutCommandsReplacesTheAllowlist(t *testing.T) {
	h := answering(t, answers("uuid-q", "ok"))
	h.headlessSession("q", "uuid-q", 1)

	w := h.do("PUT", "/commands", "version: 1\ncommands:\n  - name: /context\n    effect: forward\n")
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d: %s", w.Code, w.Body)
	}
	// The change takes effect on the next request, because the spec is read
	// per request rather than cached.
	if got := h.do("POST", "/sessions/q/command", map[string]any{"command": "/context", "respond_within": "5s"}); got.Code != http.StatusOK {
		t.Errorf("newly allowed command was refused: %d %s", got.Code, got.Body)
	}
	if got := h.do("POST", "/sessions/q/command", map[string]any{"command": "/clear"}); got.Code != http.StatusBadRequest {
		t.Errorf("a command the new spec drops was still allowed: %d", got.Code)
	}
}

func TestPutCommandsAcceptsJSON(t *testing.T) {
	h := answering(t, answers("s", "hi"))
	// JSON is a subset of YAML, so one parser reads both.
	w := h.do("PUT", "/commands", `{"version":1,"commands":[{"name":"/compact","effect":"forward"}]}`)
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d: %s", w.Code, w.Body)
	}
}

func TestPutCommandsRefusesAMalformedSpec(t *testing.T) {
	h := answering(t, answers("s", "hi"))
	before := h.json(h.do("GET", "/commands", nil))

	for _, bad := range []string{"commands: [broken: [", "version: 1\ncommands:\n  - name: /x\n    effect: teleport\n"} {
		if w := h.do("PUT", "/commands", bad); w.Code != http.StatusBadRequest {
			t.Errorf("malformed spec accepted: %d", w.Code)
		}
	}
	// And the working spec on disk is untouched.
	after := h.json(h.do("GET", "/commands", nil))
	if fmt.Sprint(after["commands"]) != fmt.Sprint(before["commands"]) {
		t.Error("a rejected spec still changed what the box accepts")
	}
}
