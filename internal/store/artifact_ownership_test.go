package store

import "testing"

func TestAddArtifactsRetainsIndependentRecords(t *testing.T) {
	b := &Build{}
	batch := []Artifact{{
		Stage: "compile",
		Step:  "package",
		Name:  "dist/app.tar.gz",
		Path:  "/artifacts/compile/package/dist/app.tar.gz",
		Size:  128,
	}}

	b.AddArtifacts(batch)

	// Artifact collectors may reuse their result buffer for the next step.
	batch[0] = Artifact{
		Stage: "release",
		Step:  "manifest",
		Name:  "dist/checksums.txt",
		Path:  "/artifacts/release/manifest/dist/checksums.txt",
		Size:  64,
	}

	got := b.Snapshot().Artifacts
	if len(got) != 1 {
		t.Fatalf("expected one retained artifact, got %d", len(got))
	}
	if got[0].Stage != "compile" || got[0].Step != "package" || got[0].Name != "dist/app.tar.gz" {
		t.Fatalf("retained artifact changed after input reuse: %+v", got[0])
	}
}
