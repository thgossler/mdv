package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/thgossler/mdv/internal/core"
)

// TestReadDocumentUsesInputDirForStdinTemp verifies that the initial document
// resolves its relative links and images against the directory mdv was launched
// from (input.Dir), not the directory the file physically lives in. This is the
// piped-stdin case: the content is materialized into a throwaway temp file, but
// relative references like <img src="images/x.png"> must resolve against the
// user's working directory.
func TestReadDocumentUsesInputDirForStdinTemp(t *testing.T) {
	tmp, err := core.WriteStdinTempFile([]byte("# hi\n"))
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmp)

	launchDir := filepath.Join(string(filepath.Separator)+"some", "launch", "dir")
	b := &Bridge{input: core.Input{Kind: core.InputFile, Path: tmp, Dir: launchDir}}

	doc := b.ReadDocument(tmp)
	if doc.Error != "" {
		t.Fatalf("ReadDocument returned error: %s", doc.Error)
	}
	if doc.Dir != launchDir {
		t.Errorf("doc.Dir = %q, want %q (the launch dir, not the temp dir %q)",
			doc.Dir, launchDir, filepath.Dir(tmp))
	}
}

// TestReadDocumentUsesFileDirForNonInputFiles verifies that a document other
// than the program's initial input still resolves against its own directory, so
// navigating to a sibling/linked file is unaffected by the stdin override.
func TestReadDocumentUsesFileDirForNonInputFiles(t *testing.T) {
	tmp, err := core.WriteStdinTempFile([]byte("# hi\n"))
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmp)

	// The bridge's input is a different path than the file being read.
	b := &Bridge{input: core.Input{Kind: core.InputFolder, Path: "/workspace", Dir: "/workspace"}}

	doc := b.ReadDocument(tmp)
	if doc.Error != "" {
		t.Fatalf("ReadDocument returned error: %s", doc.Error)
	}
	if doc.Dir != filepath.Dir(tmp) {
		t.Errorf("doc.Dir = %q, want %q (the file's own directory)", doc.Dir, filepath.Dir(tmp))
	}
}
