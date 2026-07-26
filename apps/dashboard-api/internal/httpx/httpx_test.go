package httpx

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestJSONWritesStatusAndContentType(t *testing.T) {
	w := httptest.NewRecorder()
	JSON(w, http.StatusCreated, map[string]int{"n": 42})

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", ct)
	}
	var got map[string]int
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode %q: %v", w.Body.String(), err)
	}
	if got["n"] != 42 {
		t.Fatalf("body = %v, want n=42", got)
	}
}

func TestOKIs200(t *testing.T) {
	w := httptest.NewRecorder()
	OK(w, map[string]string{"hello": "world"})

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
}

func TestErrorUsesTheSingleErrorField(t *testing.T) {
	w := httptest.NewRecorder()
	Error(w, "entry not found", http.StatusNotFound)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
	// The dashboard's fetch layer reads exactly this field, so the envelope
	// must stay {"error": "..."} and nothing else.
	var got map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode %q: %v", w.Body.String(), err)
	}
	if len(got) != 1 || got["error"] != "entry not found" {
		t.Fatalf("body = %v, want a single error field", got)
	}
}

func TestNilPayloadIsValidJSON(t *testing.T) {
	w := httptest.NewRecorder()
	OK(w, nil)

	// json.Encoder writes "null\n" — still parseable, which is what matters:
	// a client must never receive a truncated or empty body.
	var got any
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("a nil payload produced unparseable output %q: %v", w.Body.String(), err)
	}
}
