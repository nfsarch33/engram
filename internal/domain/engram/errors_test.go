package engram_test

import (
	"errors"
	"testing"

	"github.com/nfsarch33/engram/internal/domain/engram"
)

func TestSentinelErrors(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		err  error
	}{
		{"ErrNotFound", engram.ErrNotFound},
		{"ErrAlreadyExists", engram.ErrAlreadyExists},
		{"ErrInvalidID", engram.ErrInvalidID},
		{"ErrEmptyText", engram.ErrEmptyText},
		{"ErrInvalidTopK", engram.ErrInvalidTopK},
		{"ErrMissingEmbedder", engram.ErrMissingEmbedder},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if tc.err == nil {
				t.Errorf("%s must not be nil", tc.name)
			}
			if tc.err.Error() == "" {
				t.Errorf("%s must have a non-empty message", tc.name)
			}
		})
	}
}

func TestSentinelErrorIdentity(t *testing.T) {
	t.Parallel()
	// Verify errors.Is works for wrapped errors.
	wrapped := errors.Join(engram.ErrNotFound, errors.New("extra context"))
	if !errors.Is(wrapped, engram.ErrNotFound) {
		t.Error("errors.Is should unwrap to ErrNotFound")
	}
}
