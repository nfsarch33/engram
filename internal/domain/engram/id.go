package engram

import (
	"math/rand"
	"time"

	"github.com/oklog/ulid/v2"
)

// NewMemoryID generates a new time-ordered ULID-based MemoryID.
func NewMemoryID() MemoryID {
	entropy := rand.New(rand.NewSource(time.Now().UnixNano())) //nolint:gosec
	return MemoryID(ulid.MustNew(ulid.Timestamp(time.Now().UTC()), entropy).String())
}

// ParseMemoryID validates and returns a MemoryID from a raw string.
func ParseMemoryID(s string) (MemoryID, error) {
	if _, err := ulid.Parse(s); err != nil {
		return "", ErrInvalidID
	}
	return MemoryID(s), nil
}
