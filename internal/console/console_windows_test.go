//go:build windows

package console

import (
	"syscall"
	"testing"

	"golang.org/x/term"
)

func TestConsoleInputSupportsRawMode(t *testing.T) {
	getConsoleWindow := syscall.NewLazyDLL("kernel32.dll").NewProc("GetConsoleWindow")
	if handle, _, _ := getConsoleWindow.Call(); handle == 0 {
		t.Skip("requires an attached Windows console")
	}
	input, err := openConsoleInput()
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	previous, err := term.MakeRaw(int(input.Fd()))
	if err != nil {
		t.Fatalf("cannot enable raw mode on console input: %v", err)
	}
	if err := term.Restore(int(input.Fd()), previous); err != nil {
		t.Fatalf("cannot restore console mode: %v", err)
	}
}
