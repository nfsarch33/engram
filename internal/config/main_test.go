// v14788: goleak harness - detect goroutine leaks on test exit.
package config_test

import (
	"testing"

	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
