// Package response provides JSON response helpers with a stable error envelope.
package response

import (
	"encoding/json"
	"net/http"
)

type errorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type errorEnvelope struct {
	Error errorBody `json:"error"`
}

// JSON writes v as a JSON response with the given status code.
func JSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// Error writes the standard error envelope {"error":{"code","message"}}.
func Error(w http.ResponseWriter, status int, code, message string) {
	JSON(w, status, errorEnvelope{Error: errorBody{Code: code, Message: message}})
}
