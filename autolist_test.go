package main

import "testing"

func TestAutoListTargetBasic(t *testing.T) {
	name, rest := autoListTarget([]string{"Work"}, "shopping", map[string]bool{"shopping": true})
	if name != "Work" {
		t.Errorf("name = %q, want Work", name)
	}
	if len(rest) != 0 {
		t.Errorf("rest = %v", rest)
	}
}

func TestAutoListTargetStripsPrefix(t *testing.T) {
	name, _ := autoListTarget([]string{"@errands"}, "todo", nil)
	if name != "errands" {
		t.Errorf("name = %q, want errands", name)
	}
}

func TestAutoListTargetSkipsCurrentList(t *testing.T) {
	if name, _ := autoListTarget([]string{"todo"}, "todo", nil); name != "" {
		t.Errorf("task already in its tag-list should not move, got %q", name)
	}
}

func TestAutoListTargetSkipsCollision(t *testing.T) {
	// "dell" would collide with configured "Dell" (same slug)
	if name, _ := autoListTarget([]string{"dell"}, "shopping", map[string]bool{"dell": true}); name != "" {
		t.Errorf("colliding tag must be left as label, got %q", name)
	}
}

func TestAutoListTargetNoTags(t *testing.T) {
	if name, _ := autoListTarget(nil, "todo", nil); name != "" {
		t.Errorf("untagged task must not move, got %q", name)
	}
}

func TestAutoListTargetKeepsRestAsLabels(t *testing.T) {
	name, rest := autoListTarget([]string{"Work", "urgent"}, "shopping", nil)
	if name != "Work" || len(rest) != 1 || rest[0] != "urgent" {
		t.Errorf("name=%q rest=%v", name, rest)
	}
}

func TestAutoListTargetLeadingEmptyTag(t *testing.T) {
	name, _ := autoListTarget([]string{"", "Work"}, "shopping", nil)
	if name != "Work" {
		t.Errorf("name = %q", name)
	}
}
