// Command engramcli is the Engram command-line client.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

// Deps holds injectable dependencies for testability.
type Deps struct {
	Stdout io.Writer
	Stderr io.Writer
}

func main() {
	deps := Deps{
		Stdout: os.Stdout,
		Stderr: os.Stderr,
	}
	os.Exit(Run(deps, os.Args[1:]))
}

const usageText = `Usage: engramcli <command> [flags]

Commands:
  health    Check daemon health
  add       Add a memory
  search    Semantic search
  get       Get a memory by ID
  delete    Delete a memory by ID
  doctor    Run diagnostic checks against the daemon
  migrate   Import memories from another system (e.g. Mem0 OSS)
  shadow    Dual-run comparison: query both Engram and Mem0, log discrepancies

Use engramcli <command> --help for flag details.
`

// Run dispatches to subcommands and returns an exit code.
func Run(deps Deps, args []string) int {
	if len(args) == 0 {
		fmt.Fprint(deps.Stdout, usageText)
		return 1
	}

	cmd, rest := args[0], args[1:]
	switch cmd {
	case "health":
		return runHealth(deps, rest)
	case "add":
		return runAdd(deps, rest)
	case "search":
		return runSearch(deps, rest)
	case "get":
		return runGet(deps, rest)
	case "delete":
		return runDelete(deps, rest)
	case "doctor":
		return runDoctor(deps, rest)
	case "migrate":
		return runMigrate(deps, rest)
	case "shadow":
		return runShadow(deps, rest)
	default:
		fmt.Fprintf(deps.Stderr, "engramcli: unknown command %q\n\n%s", cmd, usageText)
		return 1
	}
}

// defaultAddr returns the ENGRAM_ADDR env or the built-in default.
func defaultAddr() string {
	if v := os.Getenv("ENGRAM_ADDR"); v != "" {
		return v
	}
	return "http://localhost:8280"
}

func runHealth(deps Deps, args []string) int {
	fs := flag.NewFlagSet("health", flag.ContinueOnError)
	addr := fs.String("addr", defaultAddr(), "Engram daemon address")
	fs.SetOutput(deps.Stderr)
	if err := fs.Parse(args); err != nil {
		return 1
	}

	resp, err := http.Get(*addr + "/healthz") //nolint:noctx
	if err != nil {
		fmt.Fprintf(deps.Stderr, "engramcli health: %v\n", err)
		return 1
	}
	defer resp.Body.Close()

	var out map[string]any
	json.NewDecoder(resp.Body).Decode(&out) //nolint:errcheck
	fmt.Fprintf(deps.Stdout, "status: %v\n", out["status"])
	return 0
}

func runAdd(deps Deps, args []string) int {
	fs := flag.NewFlagSet("add", flag.ContinueOnError)
	addr := fs.String("addr", defaultAddr(), "Engram daemon address")
	message := fs.String("message", "", "Message in role:content format (e.g. user:hello)")
	userID := fs.String("user-id", "", "User ID")
	agentID := fs.String("agent-id", "", "Agent ID")
	fs.SetOutput(deps.Stderr)
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if *message == "" {
		fmt.Fprintln(deps.Stderr, "engramcli add: --message is required")
		return 1
	}

	role, content, ok := strings.Cut(*message, ":")
	if !ok {
		fmt.Fprintln(deps.Stderr, "engramcli add: --message must be role:content")
		return 1
	}

	body := map[string]any{
		"messages": []map[string]string{{"role": role, "content": content}},
	}
	if *userID != "" {
		body["user_id"] = *userID
	}
	if *agentID != "" {
		body["agent_id"] = *agentID
	}

	data, _ := json.Marshal(body)
	resp, err := http.Post(*addr+"/memories", "application/json", bytes.NewReader(data)) //nolint:noctx
	if err != nil {
		fmt.Fprintf(deps.Stderr, "engramcli add: %v\n", err)
		return 1
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		fmt.Fprintf(deps.Stderr, "engramcli add: server returned %d: %s\n", resp.StatusCode, body)
		return 1
	}

	var out map[string]any
	json.NewDecoder(resp.Body).Decode(&out) //nolint:errcheck
	fmt.Fprintf(deps.Stdout, "added: %v\n", out["id"])
	return 0
}

func runSearch(deps Deps, args []string) int {
	fs := flag.NewFlagSet("search", flag.ContinueOnError)
	addr := fs.String("addr", defaultAddr(), "Engram daemon address")
	query := fs.String("query", "", "Search query")
	topK := fs.Int("top-k", 10, "Max results")
	userID := fs.String("user-id", "", "Filter by user ID")
	fs.SetOutput(deps.Stderr)
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if *query == "" {
		fmt.Fprintln(deps.Stderr, "engramcli search: --query is required")
		return 1
	}

	body := map[string]any{
		"query": *query,
		"top_k": *topK,
	}
	if *userID != "" {
		body["user_id"] = *userID
	}

	data, _ := json.Marshal(body)
	resp, err := http.Post(*addr+"/search", "application/json", bytes.NewReader(data)) //nolint:noctx
	if err != nil {
		fmt.Fprintf(deps.Stderr, "engramcli search: %v\n", err)
		return 1
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		fmt.Fprintf(deps.Stderr, "engramcli search: server returned %d: %s\n", resp.StatusCode, bodyBytes)
		return 1
	}

	var out map[string]any
	json.NewDecoder(resp.Body).Decode(&out) //nolint:errcheck
	results, _ := out["results"].([]any)
	for i, r := range results {
		rm, _ := r.(map[string]any)
		fmt.Fprintf(deps.Stdout, "[%d] id=%v score=%v\n", i+1, rm["id"], rm["score"])
		if msgs, ok := rm["messages"].([]any); ok {
			for _, m := range msgs {
				mm, _ := m.(map[string]any)
				fmt.Fprintf(deps.Stdout, "     %v: %v\n", mm["role"], mm["content"])
			}
		}
	}
	return 0
}

func runGet(deps Deps, args []string) int {
	fs := flag.NewFlagSet("get", flag.ContinueOnError)
	addr := fs.String("addr", defaultAddr(), "Engram daemon address")
	id := fs.String("id", "", "Memory ID")
	fs.SetOutput(deps.Stderr)
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if *id == "" {
		fmt.Fprintln(deps.Stderr, "engramcli get: --id is required")
		return 1
	}

	resp, err := http.Get(*addr + "/memories/" + *id) //nolint:noctx
	if err != nil {
		fmt.Fprintf(deps.Stderr, "engramcli get: %v\n", err)
		return 1
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		fmt.Fprintf(deps.Stderr, "engramcli get: memory %q not found\n", *id)
		return 1
	}
	if resp.StatusCode >= 300 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		fmt.Fprintf(deps.Stderr, "engramcli get: server returned %d: %s\n", resp.StatusCode, bodyBytes)
		return 1
	}

	var out map[string]any
	json.NewDecoder(resp.Body).Decode(&out) //nolint:errcheck
	fmt.Fprintf(deps.Stdout, "id: %v\n", out["id"])
	if msgs, ok := out["messages"].([]any); ok {
		for _, m := range msgs {
			mm, _ := m.(map[string]any)
			fmt.Fprintf(deps.Stdout, "  %v: %v\n", mm["role"], mm["content"])
		}
	}
	return 0
}

func runDelete(deps Deps, args []string) int {
	fs := flag.NewFlagSet("delete", flag.ContinueOnError)
	addr := fs.String("addr", defaultAddr(), "Engram daemon address")
	id := fs.String("id", "", "Memory ID")
	fs.SetOutput(deps.Stderr)
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if *id == "" {
		fmt.Fprintln(deps.Stderr, "engramcli delete: --id is required")
		return 1
	}

	req, _ := http.NewRequest(http.MethodDelete, *addr+"/memories/"+*id, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Fprintf(deps.Stderr, "engramcli delete: %v\n", err)
		return 1
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		fmt.Fprintf(deps.Stderr, "engramcli delete: server returned %d: %s\n", resp.StatusCode, bodyBytes)
		return 1
	}
	fmt.Fprintf(deps.Stdout, "deleted: %v\n", *id)
	return 0
}
