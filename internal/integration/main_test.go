// v14788: goleak harness - detect goroutine leaks on test exit.
// Note: integration/ contains build-tag-gated live tests; goleak ignores
// those by default (they don't run under plain `go test ./...`).
package integration_test

import (
	"testing"

	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
