package mcp

import (
	"context"
	"fmt"
	"io"

	mcplib "github.com/mark3labs/mcp-go/mcp"
	mcpsrv "github.com/mark3labs/mcp-go/server"
)

// Server wraps an Adapter as an MCP stdio JSON-RPC server. Each Engram tool
// in Adapter.Tools() is registered against the underlying MCPServer, and
// every tools/call invocation routes through Adapter.HandleTool so the
// existing service contract remains the single source of truth.
type Server struct {
	adapter *Adapter
	mcp     *mcpsrv.MCPServer
}

// NewServer builds an MCP server bound to the given adapter. The name and
// version are reported during the initialize handshake.
func NewServer(adapter *Adapter, name, version string) *Server {
	mcp := mcpsrv.NewMCPServer(name, version,
		mcpsrv.WithToolCapabilities(false),
	)
	s := &Server{adapter: adapter, mcp: mcp}
	s.registerTools()
	return s
}

func (s *Server) registerTools() {
	for _, tool := range s.adapter.Tools() {
		name := tool.Name
		s.mcp.AddTool(tool, func(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
			args := req.GetArguments()
			result, err := s.adapter.HandleTool(ctx, name, args)
			if err != nil {
				return mcplib.NewToolResultErrorf("%s: %v", name, err), nil
			}
			out, jerr := mcplib.NewToolResultJSON(result)
			if jerr != nil {
				return mcplib.NewToolResultErrorf("%s: marshal result: %v", name, jerr), nil
			}
			return out, nil
		})
	}
}

// Serve runs the MCP stdio loop until ctx is cancelled or stdin reaches EOF.
// in and out are typically os.Stdin / os.Stdout but pipes are accepted for
// tests and embedded use.
func (s *Server) Serve(ctx context.Context, in io.Reader, out io.Writer) error {
	if s == nil || s.mcp == nil {
		return fmt.Errorf("mcp server not initialised")
	}
	stdio := mcpsrv.NewStdioServer(s.mcp)
	return stdio.Listen(ctx, in, out)
}
