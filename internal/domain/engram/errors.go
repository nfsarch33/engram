package engram

import "errors"

// Sentinel errors for domain-level conditions.
var (
	ErrNotFound       = errors.New("engram: record not found")
	ErrAlreadyExists  = errors.New("engram: record already exists")
	ErrInvalidID      = errors.New("engram: invalid memory id")
	ErrEmptyText      = errors.New("engram: text must not be empty")
	ErrInvalidTopK    = errors.New("engram: top-k must be positive")
	ErrMissingEmbedder = errors.New("engram: embedder required")
)
