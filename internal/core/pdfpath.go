package core

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// defaultPDFStdinName is the base filename used for PDFs generated from stdin,
// where there is no source filename to derive a name from.
const defaultPDFStdinName = "document"

// ResolvePDFOutputPath determines the absolute output path for a generated PDF
// from the --pdf argument and the input document's base name.
//
// The rules are:
//
//   - A path ending in a separator, or naming an existing directory, is treated
//     as a folder; the PDF is written there as "<inputName>.pdf"
//     (e.g. README.md.pdf), or "document.pdf" for stdin (empty inputName).
//   - Any other path is treated as a file. A ".pdf" extension is appended unless
//     the path already ends in ".pdf" (case-insensitive), so both "report" and
//     "notes.md" become "report.pdf" and "notes.md.pdf".
//
// Relative paths resolve against the current working directory. The parent
// directory is created if necessary so the subsequent write succeeds.
func ResolvePDFOutputPath(arg, inputName string) (string, error) {
	arg = strings.TrimSpace(arg)
	if arg == "" {
		return "", fmt.Errorf("no output path given for --pdf")
	}

	defaultName := defaultPDFStdinName + ".pdf"
	if inputName = strings.TrimSpace(inputName); inputName != "" {
		defaultName = inputName + ".pdf"
	}

	// Detect a folder intent before cleaning strips a trailing separator.
	trailingSep := strings.HasSuffix(arg, string(os.PathSeparator)) || strings.HasSuffix(arg, "/")

	abs, err := filepath.Abs(arg)
	if err != nil {
		return "", err
	}

	isDir := trailingSep
	if fi, statErr := os.Stat(abs); statErr == nil && fi.IsDir() {
		isDir = true
	}

	var out string
	if isDir {
		out = filepath.Join(abs, defaultName)
	} else if strings.EqualFold(filepath.Ext(abs), ".pdf") {
		out = abs
	} else {
		out = abs + ".pdf"
	}

	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return "", fmt.Errorf("creating output directory: %w", err)
	}
	return out, nil
}
