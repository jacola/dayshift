package comments

import (
	"testing"
)

func TestWrapWithMarker(t *testing.T) {
	result := WrapWithMarker(MarkerResearch, MarkerResearchEnd, "Research findings here")
	expected := "<!-- dayshift:research -->\nResearch findings here\n<!-- /dayshift:research -->"
	if result != expected {
		t.Errorf("expected:\n%s\ngot:\n%s", expected, result)
	}
}

func TestHasMarker(t *testing.T) {
	body := "Some text\n<!-- dayshift:research -->\nResearch content\n<!-- /dayshift:research -->"

	if !HasMarker(body, MarkerResearch) {
		t.Error("expected HasMarker to find research marker")
	}
	if HasMarker(body, MarkerPlan) {
		t.Error("expected HasMarker not to find plan marker")
	}
}

func TestExtractMarkedContent(t *testing.T) {
	body := "Preamble\n<!-- dayshift:plan -->\nThis is the plan\nWith multiple lines\n<!-- /dayshift:plan -->\nPostamble"

	content, found := ExtractMarkedContent(body, MarkerPlan, MarkerPlanEnd)
	if !found {
		t.Fatal("expected to find marked content")
	}
	if content != "This is the plan\nWith multiple lines" {
		t.Errorf("unexpected content: %q", content)
	}
}

func TestExtractMarkedContentNoClosing(t *testing.T) {
	body := "Text\n<!-- dayshift:research -->\nResearch without closing marker"

	content, found := ExtractMarkedContent(body, MarkerResearch, MarkerResearchEnd)
	if !found {
		t.Fatal("expected to find marked content even without closing marker")
	}
	if content != "Research without closing marker" {
		t.Errorf("unexpected content: %q", content)
	}
}

func TestExtractMarkedContentNotFound(t *testing.T) {
	body := "Just some text with no markers"

	_, found := ExtractMarkedContent(body, MarkerPlan, MarkerPlanEnd)
	if found {
		t.Error("expected not to find marked content")
	}
}

func TestFindMarkedComment(t *testing.T) {
	bodies := []string{
		"Regular comment",
		"<!-- dayshift:research -->\nResearch here\n<!-- /dayshift:research -->",
		"Another comment",
	}

	body, found := FindMarkedComment(bodies, MarkerResearch)
	if !found {
		t.Fatal("expected to find comment with research marker")
	}
	if body != bodies[1] {
		t.Errorf("expected second comment, got %q", body)
	}

	_, found = FindMarkedComment(bodies, MarkerPlan)
	if found {
		t.Error("expected not to find comment with plan marker")
	}
}

func TestAllMarkersArePaired(t *testing.T) {
	pairs := [][2]string{
		{MarkerResearch, MarkerResearchEnd},
		{MarkerPlan, MarkerPlanEnd},
		{MarkerQuestions, MarkerQuestionsEnd},
		{MarkerApproval, MarkerApprovalEnd},
		{MarkerValidation, MarkerValidationEnd},
	}
	for _, pair := range pairs {
		if pair[0] == "" || pair[1] == "" {
			t.Errorf("marker pair has empty value: %v", pair)
		}
	}
}
