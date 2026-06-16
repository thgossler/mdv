package core

import (
	"os"
	"strings"
	"testing"
)

func TestReadStdin(t *testing.T) {
	in, err := ReadStdin(strings.NewReader("# Piped\n\nHello.\n"))
	if err != nil {
		t.Fatalf("ReadStdin: %v", err)
	}
	if in.Kind != InputStdin {
		t.Errorf("kind = %v, want InputStdin", in.Kind)
	}
	if string(in.Data) != "# Piped\n\nHello.\n" {
		t.Errorf("data = %q", in.Data)
	}
	if in.Dir == "" {
		t.Errorf("Dir should default to the working directory")
	}
}

func TestReadStdinEmpty(t *testing.T) {
	in, err := ReadStdin(strings.NewReader("   \n\t\n"))
	if err != nil {
		t.Fatalf("ReadStdin: %v", err)
	}
	if in.Kind != InputNone {
		t.Errorf("blank stdin kind = %v, want InputNone", in.Kind)
	}
}

func TestWriteStdinTempFile(t *testing.T) {
	content := []byte("# Temp\n")
	path, err := WriteStdinTempFile(content)
	if err != nil {
		t.Fatalf("WriteStdinTempFile: %v", err)
	}
	defer os.Remove(path)

	if !strings.HasSuffix(path, ".md") {
		t.Errorf("temp file should keep a .md suffix: %q", path)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading temp file: %v", err)
	}
	if string(got) != string(content) {
		t.Errorf("temp file content = %q, want %q", got, content)
	}
}
