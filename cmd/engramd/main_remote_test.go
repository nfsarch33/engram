package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/nfsarch33/engram/internal/config"
)

// TestRunWith_RemoteRequiresStdioProxyShape: --remote is only meaningful for
// the MCP stdio proxy; any other combination is rejected before anything is
// opened.
func TestRunWith_RemoteRequiresStdioProxyShape(t *testing.T) {
	t.Parallel()
	cfg := config.Config{Timeout: time.Second}
	for _, o := range []runOpts{
		{remote: "http://127.0.0.1:1"},
		{remote: "http://127.0.0.1:1", mcpStdio: true},
	} {
		err := runWith(context.Background(), discardLogger(), cfg, o)
		if err == nil || !strings.Contains(err.Error(), "--mcp-stdio --no-http") {
			t.Errorf("opts %+v: err=%v, want the proxy-shape hint", o, err)
		}
	}
}

// TestRunWith_RemoteRefusesUnreachableDaemon: the proxy fails at startup with
// a message naming the daemon, instead of serving tools that cannot work.
func TestRunWith_RemoteRefusesUnreachableDaemon(t *testing.T) {
	t.Parallel()
	cfg := config.Config{Timeout: time.Second}
	err := runWith(context.Background(), discardLogger(), cfg,
		runOpts{remote: "http://127.0.0.1:1", mcpStdio: true, noHTTP: true})
	if err == nil || !strings.Contains(err.Error(), "remote daemon") {
		t.Fatalf("err=%v, want a 'remote daemon ... unreachable' error", err)
	}
}

func TestRunWith_RemoteRejectsBadURL(t *testing.T) {
	t.Parallel()
	err := runWith(context.Background(), discardLogger(), config.Config{Timeout: time.Second},
		runOpts{remote: "127.0.0.1:8280", mcpStdio: true, noHTTP: true})
	if err == nil || !strings.Contains(err.Error(), "scheme://host") {
		t.Fatalf("err=%v, want the URL-shape error", err)
	}
}
