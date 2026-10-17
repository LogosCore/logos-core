package wiki

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.uber.org/zap"
)

func diagramSidecar(t *testing.T, path string, status int, body any, got *diagramApplyRequest) *HocuspocusClient {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != path {
			t.Errorf("wrong path %s, want %s", r.URL.Path, path)
		}
		if r.Header.Get("X-Internal-Signature-256") == "" {
			t.Error("request is not signed")
		}
		if got != nil {
			_ = json.NewDecoder(r.Body).Decode(got)
		}
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(body)
	}))
	t.Cleanup(srv.Close)
	return NewHocuspocusClient(srv.URL, "secret", zap.NewNop())
}

func TestApplyDrawingDiagram_SendsTheTextAndDecodesTheResult(t *testing.T) {
	var got diagramApplyRequest
	c := diagramSidecar(t, "/internal/drawing/diagram/apply", http.StatusOK, map[string]any{
		"ok": true, "created": 2, "connected": 1, "warnings": []string{"w"},
		"diagram": "a: server", "nodes": 2, "edges": 1, "watchers": 1,
	}, &got)

	res, err := c.ApplyDrawingDiagram(context.Background(), "doc-1", "a: server\nb: router\na -> b", DiagramMerge, "user-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Diagram != "a: server\nb: router\na -> b" || got.Mode != DiagramMerge || got.UserID != "user-1" {
		t.Fatalf("request not carried: %+v", got)
	}
	if res.Created != 2 || res.Connected != 1 || res.Diagram != "a: server" || res.Nodes != 2 || len(res.Warnings) != 1 {
		t.Fatalf("result not decoded: %+v", res)
	}
}

// A 422 is the agent's text being wrong. Every problem has to reach the agent
// with its line, so one round trip fixes them all.
func TestApplyDrawingDiagram_CarriesEveryProblem(t *testing.T) {
	c := diagramSidecar(t, "/internal/drawing/diagram/apply", http.StatusUnprocessableEntity, map[string]any{
		"error": "x",
		"problems": []map[string]any{
			{"line": 1, "message": `unknown kind "domain-controler". Did you mean domain-controller?`},
			{"line": 3, "message": `no node "nosuch"`},
		},
	}, nil)

	_, err := c.ApplyDrawingDiagram(context.Background(), "doc-1", "x", DiagramMerge, "")
	if !errors.Is(err, ErrDrawingDiagramInvalid) {
		t.Fatalf("got %v, want ErrDrawingDiagramInvalid", err)
	}
	var invalid *DiagramInvalidError
	if !errors.As(err, &invalid) || len(invalid.Problems) != 2 {
		t.Fatalf("problems lost: %v", err)
	}
	for _, want := range []string{"line 1: unknown kind", "Did you mean domain-controller", "line 3: no node"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("message %q lacks %q", err.Error(), want)
		}
	}
}

func TestApplyDrawingDiagram_RefusesAnUnknownModeWithoutCalling(t *testing.T) {
	c := NewHocuspocusClient("http://127.0.0.1:1", "secret", zap.NewNop())
	if _, err := c.ApplyDrawingDiagram(context.Background(), "d", "a: server", DiagramMode("sideways"), ""); err == nil {
		t.Fatal("an unknown mode was sent")
	}
}

func TestReadDrawingDiagram_DecodesOthers(t *testing.T) {
	c := diagramSidecar(t, "/internal/drawing/diagram/read", http.StatusOK, map[string]any{
		"diagram": "dc01: domain-controller \"DC01\"", "nodes": 1,
		"others":      []map[string]any{{"id": "t1", "type": "text", "text": "TODO", "x": 5, "y": 6}},
		"othersTotal": 1,
	}, nil)
	d, err := c.ReadDrawingDiagram(context.Background(), "doc-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d.Nodes != 1 || len(d.Others) != 1 || d.Others[0].Text != "TODO" || d.OthersTotal != 1 {
		t.Fatalf("not decoded: %+v", d)
	}
}
