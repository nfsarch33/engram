package mem0compat

import (
	"encoding/json"
	"net/http"
)

// errorBody mirrors the shape FastAPI emits when Mem0 OSS rejects a request.
// Mem0 OSS uses {"detail": "..."}; mem0-mcp-go just surfaces the entire body
// as a string, so the only invariant is "non-empty JSON object".
type errorBody struct {
	Detail string `json:"detail"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, errorBody{Detail: msg})
}
