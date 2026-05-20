package engram

import (
	"crypto/rand"
	"time"

	"github.com/oklog/ulid/v2"
)

// NewMemoryID generates a new time-ordered ULID-based MemoryID.
// Uses crypto/rand for entropy so concurrent calls never collide.
func NewMemoryID() MemoryID {
	return MemoryID(ulid.MustNew(ulid.Timestamp(time.Now().UTC()), rand.Reader).String())
}

// ParseMemoryID validates and returns a MemoryID from a raw string.
func ParseMemoryID(s string) (MemoryID, error) {
	if _, err := ulid.Parse(s); err != nil {
		return "", ErrInvalidID
	}
	return MemoryID(s), nil
}
