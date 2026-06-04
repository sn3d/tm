package client

import "testing"

func TestParseTaskState_Valid(t *testing.T) {
	for _, s := range TaskStates {
		got, err := ParseTaskState(string(s))
		if err != nil {
			t.Errorf("ParseTaskState(%q): unexpected error %v", s, err)
		}
		if got != s {
			t.Errorf("ParseTaskState(%q) = %q, want %q", s, got, s)
		}
	}
}

func TestParseTaskState_Invalid(t *testing.T) {
	for _, s := range []string{"", "pending", "completed", "unknown"} {
		if _, err := ParseTaskState(s); err == nil {
			t.Errorf("ParseTaskState(%q): expected error, got nil", s)
		}
	}
}

func TestTaskStateCategory(t *testing.T) {
	tests := []struct {
		state TaskState
		want  Category
	}{
		{TaskStateTodo, CategoryOpen},
		{TaskStateInProgress, CategoryActive},
		{TaskStateBlocked, CategoryActive},
		{TaskStateInReview, CategoryActive},
		{TaskStateDone, CategoryDone},
		{TaskStateCancelled, CategoryCancelled},
	}
	for _, tt := range tests {
		if got := tt.state.Category(); got != tt.want {
			t.Errorf("%s.Category() = %v, want %v", tt.state, got, tt.want)
		}
	}
}

func TestTaskStateDefault(t *testing.T) {
	if !TaskStateDefault.Valid() {
		t.Errorf("TaskStateDefault %q is not a valid TaskState", TaskStateDefault)
	}
}
