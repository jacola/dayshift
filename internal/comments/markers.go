// Package comments handles parsing and creating structured comments for dayshift phases.
package comments

import (
	"fmt"
	"strings"
)

// Phase comment markers (HTML comments for machine-readable boundaries).
const (
	MarkerResearch      = "<!-- dayshift:research -->"
	MarkerResearchEnd   = "<!-- /dayshift:research -->"
	MarkerPlan          = "<!-- dayshift:plan -->"
	MarkerPlanEnd       = "<!-- /dayshift:plan -->"
	MarkerQuestions     = "<!-- dayshift:questions -->"
	MarkerQuestionsEnd  = "<!-- /dayshift:questions -->"
	MarkerApproval      = "<!-- dayshift:approval -->"
	MarkerApprovalEnd   = "<!-- /dayshift:approval -->"
	MarkerValidation    = "<!-- dayshift:validation -->"
	MarkerValidationEnd = "<!-- /dayshift:validation -->"
)

// WrapWithMarker wraps content between an opening and closing marker.
func WrapWithMarker(openMarker, closeMarker, content string) string {
	return fmt.Sprintf("%s\n%s\n%s", openMarker, content, closeMarker)
}

// HasMarker checks if a comment body contains a specific marker.
func HasMarker(body string, marker string) bool {
	return strings.Contains(body, marker)
}

// ExtractMarkedContent extracts content between opening and closing markers.
// Returns the content and true if found, empty string and false if not.
func ExtractMarkedContent(body string, openMarker, closeMarker string) (string, bool) {
	startIdx := strings.Index(body, openMarker)
	if startIdx == -1 {
		return "", false
	}

	contentStart := startIdx + len(openMarker)
	endIdx := strings.Index(body[contentStart:], closeMarker)
	if endIdx == -1 {
		// No closing marker — return everything after the opening marker
		return strings.TrimSpace(body[contentStart:]), true
	}

	content := body[contentStart : contentStart+endIdx]
	return strings.TrimSpace(content), true
}

// FindMarkedComment searches a list of comment bodies for one containing a marker.
// Returns the body and true if found.
func FindMarkedComment(bodies []string, marker string) (string, bool) {
	for _, body := range bodies {
		if HasMarker(body, marker) {
			return body, true
		}
	}
	return "", false
}
