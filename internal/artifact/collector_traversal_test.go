package artifact_test

import (
	"testing"

	"minidrone/internal/artifact"
)

func TestFilterRejectsEmbeddedTraversal(t *testing.T) {
	patterns := []string{"dist/*.tgz", "reports/../../secrets.txt"}

	if got, err := artifact.Filter(patterns); err == nil {
		t.Fatalf("embedded parent-directory traversal should be rejected, got %q", got)
	}
}
