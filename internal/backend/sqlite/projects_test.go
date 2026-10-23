package sqlite

import (
	"testing"
	"time"

	"github.com/sn3d/tm/internal/client"
)

func TestProjectsRepository_SaveAndGet(t *testing.T) {
	b := newTempBackend(t)
	repo := b.Projects()

	p := &client.Project{Ticker: "bill", DisplayName: "BILL", Name: "Billing service"}
	if err := repo.Save(p); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if p.CreatedAt.IsZero() || p.UpdatedAt.IsZero() {
		t.Error("Save should stamp CreatedAt and UpdatedAt back into the struct")
	}

	got, err := repo.GetByTicker("bill")
	if err != nil {
		t.Fatalf("GetByTicker: %v", err)
	}
	if got == nil {
		t.Fatal("expected project, got nil")
	}
	if got.DisplayName != "BILL" {
		t.Errorf("DisplayName = %q, want %q (user casing must survive)", got.DisplayName, "BILL")
	}
	if got.Name != "Billing service" {
		t.Errorf("Name = %q, want %q", got.Name, "Billing service")
	}
}

// TestProjectsRepository_GetByTicker_CaseInsensitive is the point of storing
// the canonical lowercase form: TM, tm and Tm all address one project.
func TestProjectsRepository_GetByTicker_CaseInsensitive(t *testing.T) {
	b := newTempBackend(t)
	repo := b.Projects()
	if err := repo.Save(&client.Project{Ticker: "bill", DisplayName: "BILL"}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	for _, variant := range []string{"bill", "BILL", "Bill", "bILL"} {
		got, err := repo.GetByTicker(variant)
		if err != nil {
			t.Fatalf("GetByTicker(%q): %v", variant, err)
		}
		if got == nil {
			t.Fatalf("GetByTicker(%q) returned nil; all casings must address one project", variant)
		}
		if got.Ticker != "bill" {
			t.Errorf("GetByTicker(%q).Ticker = %q, want %q", variant, got.Ticker, "bill")
		}
	}
}

func TestProjectsRepository_Save_CanonicalisesTicker(t *testing.T) {
	b := newTempBackend(t)
	repo := b.Projects()

	p := &client.Project{Ticker: "WebApp", DisplayName: "WebApp"}
	if err := repo.Save(p); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if p.Ticker != "webapp" {
		t.Errorf("Save should canonicalise Ticker in place, got %q", p.Ticker)
	}
	// The table's CHECK (ticker = lower(ticker)) makes this a database
	// guarantee, not just an application convention — so an uncanonicalised
	// value would have been rejected outright.
	got, err := repo.GetByTicker("webapp")
	if err != nil || got == nil {
		t.Fatalf("GetByTicker: %v (got %+v)", err, got)
	}
	if got.DisplayName != "WebApp" {
		t.Errorf("DisplayName = %q, want %q", got.DisplayName, "WebApp")
	}
}

func TestProjectsRepository_Save_RejectsInvalidTicker(t *testing.T) {
	b := newTempBackend(t)
	for _, bad := range []string{"", "my_proj", "1tm", "abcdefghijklmnop", "a/b"} {
		if err := b.Projects().Save(&client.Project{Ticker: bad}); err == nil {
			t.Errorf("Save(%q) should fail validation", bad)
		}
	}
}

func TestProjectsRepository_GetByTicker_NotFound(t *testing.T) {
	b := newTempBackend(t)
	got, err := b.Projects().GetByTicker("nope")
	if err != nil {
		t.Fatalf("GetByTicker: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil for an absent project, got %+v", got)
	}
}

func TestProjectsRepository_Save_PreservesCreatedAtOnUpdate(t *testing.T) {
	b := newTempBackend(t)
	repo := b.Projects()

	p := &client.Project{Ticker: "bill", DisplayName: "BILL", Name: "first"}
	if err := repo.Save(p); err != nil {
		t.Fatalf("Save: %v", err)
	}
	created := p.CreatedAt

	time.Sleep(2 * time.Millisecond)
	p.Name = "second"
	if err := repo.Save(p); err != nil {
		t.Fatalf("Save (update): %v", err)
	}
	if !p.CreatedAt.Equal(created) {
		t.Errorf("CreatedAt changed on update: %v -> %v", created, p.CreatedAt)
	}
	if !p.UpdatedAt.After(created) {
		t.Errorf("UpdatedAt should advance on update, got %v", p.UpdatedAt)
	}

	got, _ := repo.GetByTicker("bill")
	if got.Name != "second" {
		t.Errorf("Name = %q, want the updated value", got.Name)
	}
}

func TestProjectsRepository_GetAll_OrdersByTickerAndHidesArchived(t *testing.T) {
	b := newTempBackend(t)
	repo := b.Projects()

	// testProject ("tm") already exists as the migration placeholder.
	for _, tk := range []string{"bill", "infra"} {
		if err := repo.Save(&client.Project{Ticker: tk, DisplayName: tk}); err != nil {
			t.Fatalf("Save %q: %v", tk, err)
		}
	}
	now := time.Now().UTC()
	if err := repo.Save(&client.Project{Ticker: "old", DisplayName: "old", ArchivedAt: &now}); err != nil {
		t.Fatalf("Save archived: %v", err)
	}

	active, err := repo.GetAll(false)
	if err != nil {
		t.Fatalf("GetAll(false): %v", err)
	}
	gotTickers := make([]string, len(active))
	for i, p := range active {
		gotTickers[i] = p.Ticker
	}
	want := []string{"bill", "infra", testProject}
	if len(gotTickers) != len(want) {
		t.Fatalf("GetAll(false) = %v, want %v", gotTickers, want)
	}
	for i := range want {
		if gotTickers[i] != want[i] {
			t.Errorf("GetAll(false)[%d] = %q, want %q (ordered by ticker)", i, gotTickers[i], want[i])
		}
	}

	all, err := repo.GetAll(true)
	if err != nil {
		t.Fatalf("GetAll(true): %v", err)
	}
	if len(all) != len(want)+1 {
		t.Errorf("GetAll(true) returned %d projects, want %d (archived included)", len(all), len(want)+1)
	}
}

// TestProjectsRepository_ArchiveRoundTrip covers the archive-over-delete
// decision: archiving hides the project but keeps its data.
func TestProjectsRepository_ArchiveRoundTrip(t *testing.T) {
	b := newTempBackend(t)
	repo := b.Projects()
	if err := repo.Save(&client.Project{Ticker: "bill", DisplayName: "BILL"}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	// Give it a task so we can prove archiving does not destroy data.
	if err := b.Tasks().Save("bill", &client.Task{Subject: "keep me"}); err != nil {
		t.Fatalf("Save task: %v", err)
	}

	p, _ := repo.GetByTicker("bill")
	now := time.Now().UTC().Truncate(time.Millisecond)
	p.ArchivedAt = &now
	if err := repo.Save(p); err != nil {
		t.Fatalf("Save (archive): %v", err)
	}

	got, _ := repo.GetByTicker("bill")
	if got.ArchivedAt == nil {
		t.Fatal("ArchivedAt should round-trip as non-nil")
	}
	if !got.ArchivedAt.Equal(now) {
		t.Errorf("ArchivedAt = %v, want %v", got.ArchivedAt, now)
	}
	// Archived projects drop out of the default listing...
	active, _ := repo.GetAll(false)
	for _, ap := range active {
		if ap.Ticker == "bill" {
			t.Error("archived project should not appear in GetAll(false)")
		}
	}
	// ...but their tasks are untouched.
	tasks, err := b.Tasks().GetAll("bill")
	if err != nil {
		t.Fatalf("GetAll tasks: %v", err)
	}
	if len(tasks) != 1 {
		t.Errorf("archiving a project must not delete its tasks, got %d", len(tasks))
	}

	// Unarchiving brings it back.
	got.ArchivedAt = nil
	if err := repo.Save(got); err != nil {
		t.Fatalf("Save (unarchive): %v", err)
	}
	back, _ := repo.GetByTicker("bill")
	if back.ArchivedAt != nil {
		t.Error("ArchivedAt should clear back to nil")
	}
}

func TestProjectsRepository_TaskCounts(t *testing.T) {
	b := newTempBackend(t)
	ensureProject(t, b, "bill")

	for i := 0; i < 3; i++ {
		if err := b.Tasks().Save(testProject, &client.Task{Subject: "t"}); err != nil {
			t.Fatalf("Save: %v", err)
		}
	}
	if err := b.Tasks().Save("bill", &client.Task{Subject: "b"}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	// An archived task must not be counted — the picker shows active work.
	archived := client.Task{Subject: "hidden"}
	now := time.Now().UTC()
	archived.ArchivedAt = &now
	if err := b.Tasks().Save("bill", &archived); err != nil {
		t.Fatalf("Save archived: %v", err)
	}

	counts, err := b.Projects().TaskCounts()
	if err != nil {
		t.Fatalf("TaskCounts: %v", err)
	}
	if counts[testProject] != 3 {
		t.Errorf("counts[%q] = %d, want 3", testProject, counts[testProject])
	}
	if counts["bill"] != 1 {
		t.Errorf("counts[bill] = %d, want 1 (archived excluded)", counts["bill"])
	}
	if _, present := counts["nosuch"]; present {
		t.Error("projects with no tasks should be absent from the map")
	}
}
