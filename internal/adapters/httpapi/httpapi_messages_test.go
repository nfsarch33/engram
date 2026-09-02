package httpapi_test

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/nfsarch33/engram/internal/domain/engram"
)

// TestAddMemory_ChatStyleObjects: POST /memories accepts messages as
// {role, content} objects as well as bare strings. Before this the decoder
// rejected the object shape outright as "invalid JSON".
func TestAddMemory_ChatStyleObjects(t *testing.T) {
	t.Parallel()
	srv := makeServer(t)

	resp := postJSON(t, srv.URL+"/memories", map[string]any{
		"messages": []any{
			map[string]any{"role": "user", "content": "object-shaped over http"},
			"plain over http",
		},
		"user_id": "u1",
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 201; body=%s", resp.StatusCode, body)
	}
	var recs []engram.MemoryRecord
	if err := json.NewDecoder(resp.Body).Decode(&recs); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(recs) != 2 || recs[0].Text != "object-shaped over http" || recs[1].Text != "plain over http" {
		t.Fatalf("stored records = %+v", recs)
	}
}

// TestAddMemory_ObjectsWithoutText: objects carrying no text are an empty
// add (400), not a blank record and not a decode error.
func TestAddMemory_ObjectsWithoutText(t *testing.T) {
	t.Parallel()
	srv := makeServer(t)
	resp := postJSON(t, srv.URL+"/memories", map[string]any{
		"messages": []any{map[string]any{"role": "user"}},
		"user_id":  "u1",
	})
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusBadRequest || !strings.Contains(string(body), "must not be empty") {
		t.Fatalf("status=%d body=%s, want 400 'must not be empty'", resp.StatusCode, body)
	}
}

// TestAddMemory_MessagesMustBeAnArray keeps the decode strictness for the
// shapes that are actually wrong.
func TestAddMemory_MessagesMustBeAnArray(t *testing.T) {
	t.Parallel()
	srv := makeServer(t)
	resp := postJSON(t, srv.URL+"/memories", map[string]any{"messages": "not an array", "user_id": "u1"})
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusBadRequest || !strings.Contains(string(body), "invalid JSON") {
		t.Fatalf("status=%d body=%s, want 400 'invalid JSON'", resp.StatusCode, body)
	}
}

// TestMetrics_VectorIndexGap: the index gap is exposed as a gauge and reads
// zero when every stored record is indexed.
func TestMetrics_VectorIndexGap(t *testing.T) {
	t.Parallel()
	srv := makeServer(t)
	resp := postJSON(t, srv.URL+"/memories", map[string]any{"messages": []string{"gap probe"}, "user_id": "u1"})
	_ = resp.Body.Close()

	mresp := getJSON(t, srv.URL+"/metrics")
	defer mresp.Body.Close()
	body, _ := io.ReadAll(mresp.Body)
	text := string(body)
	for _, want := range []string{
		"# TYPE engram_vector_indexed_count gauge",
		"engram_vector_indexed_count 1",
		"# TYPE engram_vector_index_gap gauge",
		"engram_vector_index_gap 0",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("/metrics missing %q\n----\n%s", want, text[:min(len(text), 1200)])
		}
	}
}
