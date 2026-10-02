package httpapi_test

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/nfsarch33/engram/internal/domain/engram"
)

// TestListMemories_ScopedAndOrdered: GET /memories honours the scope filter
// and returns records in insertion order; an empty scope is an empty array,
// never null.
func TestListMemories_ScopedAndOrdered(t *testing.T) {
	t.Parallel()
	srv := makeServer(t)
	for _, text := range []string{"first", "second"} {
		resp := postJSON(t, srv.URL+"/memories", map[string]any{
			"messages": []string{text}, "user_id": "lister", "workspace_id": "ws-1",
		})
		_ = resp.Body.Close()
	}
	resp := postJSON(t, srv.URL+"/memories", map[string]any{
		"messages": []string{"elsewhere"}, "user_id": "lister", "workspace_id": "ws-2",
	})
	_ = resp.Body.Close()

	lresp := getJSON(t, srv.URL+"/memories?user_id=lister&workspace_id=ws-1")
	defer lresp.Body.Close()
	if lresp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", lresp.StatusCode)
	}
	var recs []engram.MemoryRecord
	if err := json.NewDecoder(lresp.Body).Decode(&recs); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(recs) != 2 || recs[0].Text != "first" || recs[1].Text != "second" {
		t.Fatalf("scoped list = %+v", recs)
	}

	eresp := getJSON(t, srv.URL+"/memories?user_id=nobody")
	defer eresp.Body.Close()
	body, _ := io.ReadAll(eresp.Body)
	if string(body) != "[]\n" {
		t.Fatalf("empty scope must be [] not %q", body)
	}
}

// TestDeleteAll_WorkspaceFilter: DELETE /memories scopes by workspace_id too.
func TestDeleteAll_WorkspaceFilter(t *testing.T) {
	t.Parallel()
	srv := makeServer(t)
	for _, ws := range []string{"keep", "drop"} {
		resp := postJSON(t, srv.URL+"/memories", map[string]any{
			"messages": []string{"in " + ws}, "user_id": "d", "workspace_id": ws,
		})
		_ = resp.Body.Close()
	}
	req, _ := http.NewRequest(http.MethodDelete, srv.URL+"/memories?user_id=d&workspace_id=drop", nil) //nolint:noctx
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE: %v", err)
	}
	defer resp.Body.Close()
	var out struct {
		Count int `json:"count"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if out.Count != 1 {
		t.Fatalf("delete-all by workspace: count=%d want 1", out.Count)
	}
	lresp := getJSON(t, srv.URL+"/memories?user_id=d")
	defer lresp.Body.Close()
	var recs []engram.MemoryRecord
	_ = json.NewDecoder(lresp.Body).Decode(&recs)
	if len(recs) != 1 || recs[0].WorkspaceID != "keep" {
		t.Fatalf("after scoped delete: %+v", recs)
	}
}

// Two apps share a user; an app-scoped list must never return the other
// app's rows, and an app-scoped delete-all must never touch them. This is
// the HTTP boundary over the store's app_id boundary.
//
// MUTANT: drop the AppID branch in the sqlite ListRecords and these rows
// go red.
func TestListMemoriesAppIDIsolation(t *testing.T) {
	t.Parallel()
	srv := makeServer(t)
	for _, app := range []string{"app-a", "app-b"} {
		resp := postJSON(t, srv.URL+"/memories", map[string]any{
			"messages": []string{"row for " + app}, "user_id": "shared", "app_id": app,
		})
		_ = resp.Body.Close()
	}

	lresp := getJSON(t, srv.URL+"/memories?user_id=shared&app_id=app-a")
	defer lresp.Body.Close()
	if lresp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", lresp.StatusCode)
	}
	var recs []engram.MemoryRecord
	if err := json.NewDecoder(lresp.Body).Decode(&recs); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(recs) != 1 || recs[0].AppID != "app-a" {
		t.Fatalf("app-a scope leaked or miscounted: %+v", recs)
	}

	eresp := getJSON(t, srv.URL+"/memories?user_id=shared&app_id=nonexistent-app-xyz")
	defer eresp.Body.Close()
	body, _ := io.ReadAll(eresp.Body)
	if string(body) != "[]\n" {
		t.Fatalf("a nonexistent app must list [] not %q", body)
	}
}

// TestDeleteAll_AppFilter: DELETE /memories?app_id= removes only that app.
func TestDeleteAll_AppFilter(t *testing.T) {
	t.Parallel()
	srv := makeServer(t)
	for _, app := range []string{"keep-app", "drop-app"} {
		resp := postJSON(t, srv.URL+"/memories", map[string]any{
			"messages": []string{"in " + app}, "user_id": "d", "app_id": app,
		})
		_ = resp.Body.Close()
	}
	req, _ := http.NewRequest(http.MethodDelete, srv.URL+"/memories?user_id=d&app_id=drop-app", nil) //nolint:noctx
	dresp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer dresp.Body.Close()
	if dresp.StatusCode != http.StatusOK {
		t.Fatalf("delete-all: %d", dresp.StatusCode)
	}
	lresp := getJSON(t, srv.URL+"/memories?user_id=d")
	defer lresp.Body.Close()
	var recs []engram.MemoryRecord
	if err := json.NewDecoder(lresp.Body).Decode(&recs); err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1 || recs[0].AppID != "keep-app" {
		t.Fatalf("the OTHER app must survive an app-scoped delete: %+v", recs)
	}
}
