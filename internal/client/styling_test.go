package client

import (
	"reflect"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestStyling_YAMLRoundTrip(t *testing.T) {
	in := Styling{
		States: map[string]Style{
			"todo":        {Icon: "📋", Color: "yellow"},
			"in_progress": {Icon: "🔄", Color: "#ff5577"},
		},
		Labels: map[string]Style{
			"bug": {Icon: "🐛", Color: "red"},
		},
		LabelsDefault: Style{Icon: "•", Color: "white"},
	}
	body, err := yaml.Marshal(in)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var got Styling
	if err := yaml.Unmarshal(body, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if !reflect.DeepEqual(got, in) {
		t.Errorf("round-trip mismatch:\n got: %+v\nwant: %+v\nyaml:\n%s", got, in, body)
	}
}

func TestMergeStyling_ProjectOverridesPerField(t *testing.T) {
	global := Styling{
		States: map[string]Style{
			"todo": {Icon: "📋", Color: "yellow"},
		},
	}
	project := Styling{
		States: map[string]Style{
			"todo": {Color: "red"}, // icon left empty → falls through
		},
	}
	got := mergeStyling(global, project)
	want := Style{Icon: "📋", Color: "red"}
	if got.States["todo"] != want {
		t.Errorf("merged todo: got %+v, want %+v", got.States["todo"], want)
	}
}

func TestMergeStyling_ProjectAddsNewKey(t *testing.T) {
	global := Styling{
		States: map[string]Style{
			"todo": {Icon: "📋", Color: "yellow"},
		},
	}
	project := Styling{
		States: map[string]Style{
			"blocked": {Icon: "⛔", Color: "red"},
		},
	}
	got := mergeStyling(global, project)
	if got.States["todo"].Icon != "📋" {
		t.Errorf("todo lost during merge: %+v", got.States["todo"])
	}
	if got.States["blocked"].Icon != "⛔" {
		t.Errorf("blocked not added: %+v", got.States["blocked"])
	}
}

func TestMergeStyling_LabelsDefaultMerged(t *testing.T) {
	global := Styling{LabelsDefault: Style{Icon: "•", Color: "white"}}
	project := Styling{LabelsDefault: Style{Color: "gray"}}
	got := mergeStyling(global, project)
	want := Style{Icon: "•", Color: "gray"}
	if got.LabelsDefault != want {
		t.Errorf("labels_default: got %+v, want %+v", got.LabelsDefault, want)
	}
}

func TestMergeStyling_EmptyInputsProduceEmptyOutput(t *testing.T) {
	got := mergeStyling(Styling{}, Styling{})
	if len(got.States) != 0 || len(got.Labels) != 0 {
		t.Errorf("expected empty merge, got %+v", got)
	}
}
