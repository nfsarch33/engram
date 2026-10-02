package sqlite_test

import (
	"context"
	"testing"

	"github.com/nfsarch33/engram/internal/domain/engram"
)

// MUTANT: drop the AppID branch in ListRecords and every app-scoped listing
// (and, through DeleteAll, every scoped delete) leaks across apps; these
// rows go red. Seeded with TWO apps sharing a user, because a single-app
// fixture cannot see the boundary.
func TestListRecordsFiltersOnAppID(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	ctx := context.Background()
	for _, app := range []string{"app-a", "app-b"} {
		for _, text := range []string{"one", "two"} {
			r := makeRecord("shared-user", text)
			r.AppID = app
			if err := s.SaveRecord(ctx, r); err != nil {
				t.Fatalf("SaveRecord: %v", err)
			}
		}
	}

	onlyA, err := s.ListRecords(ctx, engram.HistoryFilter{AppID: "app-a"})
	if err != nil {
		t.Fatalf("ListRecords(app-a): %v", err)
	}
	if len(onlyA) != 2 {
		t.Fatalf("app-a rows = %d, want 2 (never app-b's)", len(onlyA))
	}
	for _, r := range onlyA {
		if r.AppID != "app-a" {
			t.Fatalf("cross-app row leaked: %+v", r)
		}
	}

	none, err := s.ListRecords(ctx, engram.HistoryFilter{AppID: "nonexistent-app-xyz"})
	if err != nil {
		t.Fatalf("ListRecords(nonexistent): %v", err)
	}
	if len(none) != 0 {
		t.Fatalf("a nonexistent app must list 0 rows, got %d", len(none))
	}

	// An EMPTY filter still lists everything: the compatibility adapter's
	// unscoped parity reads rely on it.
	all, err := s.ListRecords(ctx, engram.HistoryFilter{})
	if err != nil {
		t.Fatalf("ListRecords(empty): %v", err)
	}
	if len(all) != 4 {
		t.Fatalf("unscoped rows = %d, want all 4", len(all))
	}
}

// MUTANT guard for the delete path: DeleteAll lists through ListRecords, so
// an unfiltered app_id there would delete across apps.
func TestDeleteAllScopesOnAppID(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	ctx := context.Background()
	for _, app := range []string{"app-a", "app-b"} {
		r := makeRecord("shared-user", "row")
		r.AppID = app
		if err := s.SaveRecord(ctx, r); err != nil {
			t.Fatalf("SaveRecord: %v", err)
		}
	}
	// The service's DeleteAll lists through ListRecords and deletes each
	// id — that flow is reproduced here against the real store.
	doomed, err := s.ListRecords(ctx, engram.HistoryFilter{AppID: "app-a"})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range doomed {
		if err := s.DeleteRecord(ctx, r.ID); err != nil {
			t.Fatalf("DeleteRecord %s: %v", r.ID, err)
		}
	}
	left, err := s.ListRecords(ctx, engram.HistoryFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(left) != 1 || left[0].AppID != "app-b" {
		t.Fatalf("app-b must survive an app-a delete, got %+v", left)
	}
}
