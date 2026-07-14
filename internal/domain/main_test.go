// v14788: goleak harness. This package has no behavioural tests yet; the
// TestMain exists so goleak still verifies that package init does not leak
// goroutines. Add real tests + per-test ignores as the package grows.
package domain_test

import (
	"testing"

	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
