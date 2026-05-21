package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"time"
)

func runDoctor(deps Deps, args []string) int {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	addr := fs.String("addr", defaultAddr(), "Engram daemon address")
	fs.SetOutput(deps.Stderr)
	if err := fs.Parse(args); err != nil {
		return 1
	}

	fmt.Fprintf(deps.Stdout, "engram doctor — checking %s\n\n", *addr)
	allOK := true

	allOK = checkHealth(deps, *addr) && allOK
	allOK = checkSearch(deps, *addr) && allOK
	allOK = checkMemories(deps, *addr) && allOK

	fmt.Fprintln(deps.Stdout)
	if allOK {
		fmt.Fprintln(deps.Stdout, "result: all checks passed")
	} else {
		fmt.Fprintln(deps.Stdout, "result: one or more checks failed")
		return 1
	}
	return 0
}

func checkHealth(deps Deps, addr string) bool {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(addr + "/healthz") //nolint:noctx
	if err != nil {
		fmt.Fprintf(deps.Stdout, "[FAIL] healthz: %v\n", err)
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(deps.Stdout, "[FAIL] healthz: HTTP %d\n", resp.StatusCode)
		return false
	}
	fmt.Fprintln(deps.Stdout, "[ OK ] healthz: daemon is reachable")
	return true
}

func checkSearch(deps Deps, addr string) bool {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Post(addr+"/search", "application/json", //nolint:noctx
		jsonReader(map[string]any{"query": "doctor-probe", "top_k": 1}))
	if err != nil {
		fmt.Fprintf(deps.Stdout, "[FAIL] search: %v\n", err)
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		fmt.Fprintln(deps.Stdout, "[ OK ] search: embedder is operational")
		return true
	}

	var body map[string]any
	json.NewDecoder(resp.Body).Decode(&body) //nolint:errcheck
	msg := "unknown"
	if e, ok := body["error"].(string); ok {
		msg = e
	}
	fmt.Fprintf(deps.Stdout, "[WARN] search: HTTP %d — %s\n", resp.StatusCode, msg)
	return false
}

func checkMemories(deps Deps, addr string) bool {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Post(addr+"/memories", "application/json", //nolint:noctx
		jsonReader(map[string]any{
			"messages": []string{"engram-doctor-probe"},
			"user_id":  "__engram_doctor__",
		}))
	if err != nil {
		fmt.Fprintf(deps.Stdout, "[FAIL] write: %v\n", err)
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		fmt.Fprintln(deps.Stdout, "[ OK ] write: history store is writable")
	} else {
		fmt.Fprintf(deps.Stdout, "[FAIL] write: HTTP %d\n", resp.StatusCode)
		return false
	}

	// Clean up the probe memory.
	var result []map[string]any
	json.NewDecoder(resp.Body).Decode(&result) //nolint:errcheck
	if len(result) > 0 {
		if id, ok := result[0]["ID"].(string); ok && id != "" {
			delReq, _ := http.NewRequest(http.MethodDelete, addr+"/memories/"+id, nil) //nolint:noctx
			delResp, delErr := client.Do(delReq)
			if delErr == nil {
				delResp.Body.Close()
			}
		}
	}
	return true
}
