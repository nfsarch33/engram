// v14788: goleak harness - detect goroutine leaks on test exit.
// q11-2: Ginkgo v2's interrupt_handler spawns a goroutine that
// stays alive after the suite completes. This is a known false
// positive (see onsii/ginkgo#1641). Whitelist the package so
// the integration-tagged Ginkgo spec in cache_ginkgo_test.go and
// the regular cache_test.go can coexist under goleak.
package cache_test

import (
	"testing"

	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	// q11-2: Ginkgo v2's interrupt_handler launches a goroutine
	// whose runtime function name is the closure
	// `InterruptHandler.registerForInterrupts.func2`. The goroutine
	// stays alive across the whole suite and is a known false
	// positive in go.uber.org/goleak.VerifyTestMain.
	goleak.VerifyTestMain(
		m,
		goleak.IgnoreAnyFunction(
			"github.com/onsi/ginkgo/v2/internal/interrupt_handler.(*InterruptHandler).registerForInterrupts.func2",
		),
	)
}
