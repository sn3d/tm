package sqlite

import (
	"testing"

	"github.com/sn3d/tm/internal/client"
)

// TestPerProjectIDCounter_NumberingRestartsPerProject is what "TM/#1 and
// BILL/#1 coexist" means in practice: the counter is scoped by project_id, so
// each project numbers from 1 independently. That independence is also what
// makes folding many old databases together tractable — no ID needs
// rewriting because the composite key makes collisions impossible.
func TestPerProjectIDCounter_NumberingRestartsPerProject(t *testing.T) {
	b := newTempBackend(t)
	ensureProject(t, b, "bill")
	repo := b.Tasks()

	first := client.Task{Subject: "tm one"}
	if err := repo.Save(testProject, &first); err != nil {
		t.Fatalf("Save tm: %v", err)
	}
	if first.ID.ID != "1" {
		t.Errorf("first %s task ID = %q, want %q", testProject, first.ID.ID, "1")
	}
	if first.ID.Project != testProject {
		t.Errorf("project = %q, want %q", first.ID.Project, testProject)
	}

	billFirst := client.Task{Subject: "bill one"}
	if err := repo.Save("bill", &billFirst); err != nil {
		t.Fatalf("Save bill: %v", err)
	}
	if billFirst.ID.ID != "1" {
		t.Errorf("first bill task ID = %q, want %q (numbering restarts per project)", billFirst.ID.ID, "1")
	}
	if billFirst.ID.Project != "bill" {
		t.Errorf("project = %q, want %q", billFirst.ID.Project, "bill")
	}

	// Both #1 rows coexist and address different tasks.
	if first.ID == billFirst.ID {
		t.Fatal("tm/#1 and bill/#1 must be distinct references")
	}
	gotTM, err := repo.GetByID(first.ID)
	if err != nil || gotTM == nil {
		t.Fatalf("GetByID(%s): %v", first.ID, err)
	}
	gotBill, err := repo.GetByID(billFirst.ID)
	if err != nil || gotBill == nil {
		t.Fatalf("GetByID(%s): %v", billFirst.ID, err)
	}
	if gotTM.Subject != "tm one" {
		t.Errorf("tm/#1 subject = %q, want %q", gotTM.Subject, "tm one")
	}
	if gotBill.Subject != "bill one" {
		t.Errorf("bill/#1 subject = %q, want %q", gotBill.Subject, "bill one")
	}
}

func TestPerProjectIDCounter_AdvancesIndependently(t *testing.T) {
	b := newTempBackend(t)
	ensureProject(t, b, "bill")
	repo := b.Tasks()

	// Three in tm, one in bill, then one more in each.
	for i := 0; i < 3; i++ {
		tk := client.Task{Subject: "tm"}
		if err := repo.Save(testProject, &tk); err != nil {
			t.Fatalf("Save tm: %v", err)
		}
	}
	bill1 := client.Task{Subject: "bill"}
	if err := repo.Save("bill", &bill1); err != nil {
		t.Fatalf("Save bill: %v", err)
	}

	tm4 := client.Task{Subject: "tm again"}
	if err := repo.Save(testProject, &tm4); err != nil {
		t.Fatalf("Save tm: %v", err)
	}
	if tm4.ID.ID != "4" {
		t.Errorf("fourth tm task = %q, want %q", tm4.ID.ID, "4")
	}

	bill2 := client.Task{Subject: "bill again"}
	if err := repo.Save("bill", &bill2); err != nil {
		t.Fatalf("Save bill: %v", err)
	}
	if bill2.ID.ID != "2" {
		t.Errorf("second bill task = %q, want %q (tm's count must not leak)", bill2.ID.ID, "2")
	}
}

func TestPerProjectIDCounter_IgnoresNonNumericIDs(t *testing.T) {
	b := newTempBackend(t)
	repo := b.Tasks()

	// A hand-written non-numeric ID must not break the counter: the GLOB
	// filter excludes anything with a non-digit, so the max stays 0.
	legacy := client.Task{ID: ref("TASK-123"), Subject: "legacy"}
	if err := repo.Save(testProject, &legacy); err != nil {
		t.Fatalf("Save legacy: %v", err)
	}
	auto := client.Task{Subject: "auto"}
	if err := repo.Save(testProject, &auto); err != nil {
		t.Fatalf("Save auto: %v", err)
	}
	if auto.ID.ID != "1" {
		t.Errorf("auto ID = %q, want %q (non-numeric IDs are excluded from the max)", auto.ID.ID, "1")
	}
}

// TestProjectIsolation_GetAll proves a project-scoped list never leaks another
// project's rows.
func TestProjectIsolation_GetAll(t *testing.T) {
	b := newTempBackend(t)
	ensureProject(t, b, "bill")
	repo := b.Tasks()

	for i := 0; i < 2; i++ {
		tk := client.Task{Subject: "tm task"}
		if err := repo.Save(testProject, &tk); err != nil {
			t.Fatalf("Save: %v", err)
		}
	}
	for i := 0; i < 3; i++ {
		tk := client.Task{Subject: "bill task"}
		if err := repo.Save("bill", &tk); err != nil {
			t.Fatalf("Save: %v", err)
		}
	}

	tmTasks, err := repo.GetAll(testProject)
	if err != nil {
		t.Fatalf("GetAll(%s): %v", testProject, err)
	}
	if len(tmTasks) != 2 {
		t.Errorf("GetAll(%s) returned %d tasks, want 2", testProject, len(tmTasks))
	}
	for _, tk := range tmTasks {
		if tk.ID.Project != testProject {
			t.Errorf("GetAll(%s) leaked a task from %q", testProject, tk.ID.Project)
		}
	}

	billTasks, err := repo.GetAll("bill")
	if err != nil {
		t.Fatalf("GetAll(bill): %v", err)
	}
	if len(billTasks) != 3 {
		t.Errorf("GetAll(bill) returned %d tasks, want 3", len(billTasks))
	}
}

// TestProjectIsolation_GetByIDWrongProject guards the composite key: the same
// bare number in another project must not resolve.
func TestProjectIsolation_GetByIDWrongProject(t *testing.T) {
	b := newTempBackend(t)
	ensureProject(t, b, "bill")
	repo := b.Tasks()

	tk := client.Task{Subject: "only in tm"}
	if err := repo.Save(testProject, &tk); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := repo.GetByID(projectRef("bill", tk.ID.ID))
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got != nil {
		t.Errorf("bill/#%s must not resolve to a tm task, got %+v", tk.ID.ID, got)
	}
}

func TestProjectIsolation_GetByParent(t *testing.T) {
	b := newTempBackend(t)
	ensureProject(t, b, "bill")
	repo := b.Tasks()

	tmParent := client.Task{Subject: "tm parent"}
	if err := repo.Save(testProject, &tmParent); err != nil {
		t.Fatalf("Save: %v", err)
	}
	tmChild := client.Task{Subject: "tm child", ParentID: tmParent.ID}
	if err := repo.Save(testProject, &tmChild); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// A bill task whose stored bare parent_id happens to equal tmParent's
	// number must not show up under the tm parent.
	billParent := client.Task{Subject: "bill parent"}
	if err := repo.Save("bill", &billParent); err != nil {
		t.Fatalf("Save: %v", err)
	}
	billChild := client.Task{Subject: "bill child", ParentID: billParent.ID}
	if err := repo.Save("bill", &billChild); err != nil {
		t.Fatalf("Save: %v", err)
	}

	children, err := repo.GetByParent(tmParent.ID)
	if err != nil {
		t.Fatalf("GetByParent: %v", err)
	}
	if len(children) != 1 {
		t.Fatalf("GetByParent returned %d children, want 1", len(children))
	}
	if children[0].Subject != "tm child" {
		t.Errorf("child subject = %q, want %q", children[0].Subject, "tm child")
	}
	// The child's parent ref is re-qualified with its own project.
	if children[0].ParentID != tmParent.ID {
		t.Errorf("ParentID = %+v, want %+v", children[0].ParentID, tmParent.ID)
	}
}

func TestProjectIsolation_Comments(t *testing.T) {
	b := newTempBackend(t)
	ensureProject(t, b, "bill")

	tmTask := client.Task{Subject: "tm"}
	if err := b.Tasks().Save(testProject, &tmTask); err != nil {
		t.Fatalf("Save: %v", err)
	}
	billTask := client.Task{Subject: "bill"}
	if err := b.Tasks().Save("bill", &billTask); err != nil {
		t.Fatalf("Save: %v", err)
	}

	tmComment := client.Comment{Who: "alice", Comment: "tm note"}
	if err := b.Comments().Add(tmTask.ID, &tmComment); err != nil {
		t.Fatalf("Add: %v", err)
	}
	billComment := client.Comment{Who: "bob", Comment: "bill note"}
	if err := b.Comments().Add(billTask.ID, &billComment); err != nil {
		t.Fatalf("Add: %v", err)
	}

	tmGot, err := b.Comments().GetForTask(tmTask.ID)
	if err != nil {
		t.Fatalf("GetForTask: %v", err)
	}
	if len(tmGot) != 1 || tmGot[0].Comment != "tm note" {
		t.Errorf("tm comments = %+v, want just the tm note", tmGot)
	}
	// Same bare number, other project: no leakage.
	if both := len(tmGot); both != 1 {
		t.Errorf("expected exactly one comment, got %d", both)
	}
}

// TestDescriptions_LazyRow documents the storage asymmetry: a task with no
// description has no row, GetByID joins the body in, and the list paths
// deliberately do not.
func TestDescriptions_LazyRow(t *testing.T) {
	b := newTempBackend(t)
	repo := b.Tasks()

	withBody := client.Task{Subject: "has body", Description: "# Heading\n\nprose"}
	if err := repo.Save(testProject, &withBody); err != nil {
		t.Fatalf("Save: %v", err)
	}
	withoutBody := client.Task{Subject: "no body"}
	if err := repo.Save(testProject, &withoutBody); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// GetByID joins the description.
	got, err := repo.GetByID(withBody.ID)
	if err != nil || got == nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.Description != "# Heading\n\nprose" {
		t.Errorf("Description = %q, want the stored body", got.Description)
	}
	// A task with no description reads back "" rather than failing.
	none, err := repo.GetByID(withoutBody.ID)
	if err != nil || none == nil {
		t.Fatalf("GetByID: %v", err)
	}
	if none.Description != "" {
		t.Errorf("Description = %q, want empty", none.Description)
	}

	// The descriptions repository agrees, and stores no row for the empty one.
	body, err := b.Descriptions().Get(withBody.ID)
	if err != nil {
		t.Fatalf("Descriptions.Get: %v", err)
	}
	if body != "# Heading\n\nprose" {
		t.Errorf("Descriptions.Get = %q", body)
	}
	if body, err := b.Descriptions().Get(withoutBody.ID); err != nil || body != "" {
		t.Errorf("Descriptions.Get for a bodyless task = %q, %v; want \"\", nil", body, err)
	}

	// GetAll does NOT populate Description — asserted so a future change
	// that starts joining it (or a caller relying on it) is caught here
	// rather than becoming a performance regression nobody notices.
	all, err := repo.GetAll(testProject)
	if err != nil {
		t.Fatalf("GetAll: %v", err)
	}
	for _, tk := range all {
		if tk.Description != "" {
			t.Errorf("GetAll must not populate Description (task %s got %q)", tk.ID, tk.Description)
		}
	}
}

func TestDescriptions_SetClearsRow(t *testing.T) {
	b := newTempBackend(t)
	tk := client.Task{Subject: "s", Description: "body"}
	if err := b.Tasks().Save(testProject, &tk); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Writing "" deletes the row rather than storing an empty body, keeping
	// the lazy-row invariant.
	if err := b.Descriptions().Set(tk.ID, ""); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got, err := b.Descriptions().Get(tk.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "" {
		t.Errorf("Get after clearing = %q, want empty", got)
	}

	// And setting it again recreates the row.
	if err := b.Descriptions().Set(tk.ID, "back again"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if got, _ := b.Descriptions().Get(tk.ID); got != "back again" {
		t.Errorf("Get = %q, want %q", got, "back again")
	}
}

// TestSave_ClearingDescriptionRemovesBody guards the seam between
// tasksRepository.Save and the descriptions table: saving a task whose
// Description went empty must clear the stored body, not leave the old one.
func TestSave_ClearingDescriptionRemovesBody(t *testing.T) {
	b := newTempBackend(t)
	repo := b.Tasks()

	tk := client.Task{Subject: "s", Description: "original"}
	if err := repo.Save(testProject, &tk); err != nil {
		t.Fatalf("Save: %v", err)
	}
	tk.Description = ""
	if err := repo.Save(testProject, &tk); err != nil {
		t.Fatalf("Save (clear): %v", err)
	}

	got, err := repo.GetByID(tk.ID)
	if err != nil || got == nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.Description != "" {
		t.Errorf("Description = %q after clearing, want empty", got.Description)
	}
}

func TestActorCursors_PerProject(t *testing.T) {
	b := newTempBackend(t)
	ensureProject(t, b, "bill")
	cursors := b.ActorCursors()

	// An unseen (actor, project) pair yields the zero time so every event
	// reads as new.
	if ts, err := cursors.Get("alice", testProject); err != nil || !ts.IsZero() {
		t.Errorf("unseen cursor = %v, %v; want zero time", ts, err)
	}

	tmTime := timeAt(1000)
	if err := cursors.Set("alice", testProject, tmTime); err != nil {
		t.Fatalf("Set: %v", err)
	}

	// Setting tm's cursor must leave bill's alone — a global cursor would
	// mean glancing at one project marks every other project's work as seen.
	billTS, err := cursors.Get("alice", "bill")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !billTS.IsZero() {
		t.Errorf("bill cursor = %v, want zero (per-project isolation)", billTS)
	}

	gotTM, err := cursors.Get("alice", testProject)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !gotTM.Equal(tmTime) {
		t.Errorf("tm cursor = %v, want %v", gotTM, tmTime)
	}

	// Different actors are also independent within one project.
	if ts, _ := cursors.Get("bob", testProject); !ts.IsZero() {
		t.Errorf("bob's cursor = %v, want zero", ts)
	}
}

func TestEvents_ScopedByProject(t *testing.T) {
	b := newTempBackend(t)
	ensureProject(t, b, "bill")

	tmEvent := client.Event{Kind: client.EventTaskCreated, TaskID: ref("1"), Actor: "alice"}
	if err := b.Events().Append(&tmEvent); err != nil {
		t.Fatalf("Append: %v", err)
	}
	billEvent := client.Event{Kind: client.EventTaskCreated, TaskID: projectRef("bill", "1"), Actor: "bob"}
	if err := b.Events().Append(&billEvent); err != nil {
		t.Fatalf("Append: %v", err)
	}

	// Append derives project from the task reference.
	if tmEvent.Project != testProject {
		t.Errorf("derived project = %q, want %q", tmEvent.Project, testProject)
	}

	tmEvents, err := b.Events().List(client.EventFilter{Project: testProject})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(tmEvents) != 1 {
		t.Fatalf("List(project=%s) returned %d events, want 1", testProject, len(tmEvents))
	}
	if tmEvents[0].Actor != "alice" {
		t.Errorf("actor = %q, want alice", tmEvents[0].Actor)
	}
	if tmEvents[0].TaskID != ref("1") {
		t.Errorf("TaskID = %+v, want %+v", tmEvents[0].TaskID, ref("1"))
	}

	// An empty Project means "every project in the instance", which is what
	// JournalAll relies on.
	all, err := b.Events().List(client.EventFilter{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(all) != 2 {
		t.Errorf("List with no project filter returned %d events, want 2", len(all))
	}
}

// TestEvents_TaskFilterPinsProject guards against bare-id collisions: both
// projects number a task 1, so filtering by a task reference must also
// constrain the project.
func TestEvents_TaskFilterPinsProject(t *testing.T) {
	b := newTempBackend(t)
	ensureProject(t, b, "bill")

	if err := b.Events().Append(&client.Event{
		Kind: client.EventTaskCreated, TaskID: ref("1"), Actor: "alice"}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if err := b.Events().Append(&client.Event{
		Kind: client.EventTaskCreated, TaskID: projectRef("bill", "1"), Actor: "bob"}); err != nil {
		t.Fatalf("Append: %v", err)
	}

	got, err := b.Events().List(client.EventFilter{TaskID: ref("1")})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("filtering by %s returned %d events, want 1 (bare ids collide across projects)",
			ref("1"), len(got))
	}
	if got[0].Actor != "alice" {
		t.Errorf("actor = %q, want alice", got[0].Actor)
	}
}

func TestComments_CreatedAtRoundTrips(t *testing.T) {
	b := newTempBackend(t)
	tk := client.Task{Subject: "s"}
	if err := b.Tasks().Save(testProject, &tk); err != nil {
		t.Fatalf("Save: %v", err)
	}

	c := client.Comment{Who: "alice", Comment: "note"}
	if err := b.Comments().Add(tk.ID, &c); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if c.ID == "" {
		t.Error("Add should assign a ULID")
	}
	if c.CreatedAt.IsZero() {
		t.Error("Add should stamp CreatedAt")
	}

	got, err := b.Comments().GetForTask(tk.ID)
	if err != nil {
		t.Fatalf("GetForTask: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d comments, want 1", len(got))
	}
	if got[0].CreatedAt.IsZero() {
		t.Error("CreatedAt should round-trip as non-zero")
	}
	if !got[0].CreatedAt.Equal(c.CreatedAt.UTC().Truncate(0)) &&
		got[0].CreatedAt.Sub(c.CreatedAt).Abs() > 0 {
		t.Errorf("CreatedAt = %v, want %v", got[0].CreatedAt, c.CreatedAt)
	}
}

// TestComments_OrderedByCreatedAt covers why the column was added: rows
// merged from several databases have no meaningful cross-source ULID order,
// so ordering is by the explicit timestamp.
func TestComments_OrderedByCreatedAt(t *testing.T) {
	b := newTempBackend(t)
	tk := client.Task{Subject: "s"}
	if err := b.Tasks().Save(testProject, &tk); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Insert with explicit out-of-order ULIDs but ascending timestamps: a
	// ULID-only ordering would get this wrong.
	later := client.Comment{ID: "ZZZZZZZZZZZZZZZZZZZZZZZZZZ", Who: "a", Comment: "first in time", CreatedAt: timeAt(100)}
	earlier := client.Comment{ID: "AAAAAAAAAAAAAAAAAAAAAAAAAA", Who: "b", Comment: "second in time", CreatedAt: timeAt(200)}
	if err := b.Comments().Add(tk.ID, &later); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := b.Comments().Add(tk.ID, &earlier); err != nil {
		t.Fatalf("Add: %v", err)
	}

	got, err := b.Comments().GetForTask(tk.ID)
	if err != nil {
		t.Fatalf("GetForTask: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d comments, want 2", len(got))
	}
	if got[0].Comment != "first in time" {
		t.Errorf("first comment = %q, want the earlier timestamp regardless of ULID order", got[0].Comment)
	}
}
