// v14788: goleak harness for cmd/engramd.
// main_test.go tests buildAdapters/runWith synchronously and does not start
// the actual HTTP/MCP servers, so a plain goleak.VerifyTestMain is safe here.
package main

import (
	"testing"

	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
