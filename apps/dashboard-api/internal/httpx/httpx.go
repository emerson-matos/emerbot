// Package httpx holds the JSON response helpers every dashboard-api handler
// shares, so all endpoints answer with the same content type and the same error
// envelope — previously each handler package carried its own copy.
package httpx

import (
	"encoding/json"
	"net/http"
)

// JSON writes v as the response body with the given status code.
func JSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v) //nolint:errcheck
}

// OK writes v with 200 OK.
func OK(w http.ResponseWriter, v any) {
	JSON(w, http.StatusOK, v)
}

// Error writes {"error": msg} with the given status code. The dashboard's fetch
// layer reads that single field, so every failure path must go through here.
func Error(w http.ResponseWriter, msg string, code int) {
	JSON(w, code, map[string]string{"error": msg})
}
