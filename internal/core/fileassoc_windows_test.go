//go:build windows

package core

import "testing"

func TestWindowsShellRegistrationsIncludeFolders(t *testing.T) {
	registrations := windowsShellRegistrations()
	want := map[string]string{
		`Software\Classes\Directory\shell\mdv`:            "%1",
		`Software\Classes\Directory\Background\shell\mdv`: "%V",
	}
	for _, ext := range markdownExtensions {
		want[`Software\Classes\SystemFileAssociations\`+ext+`\shell\mdv`] = "%1"
	}

	if len(registrations) != len(want) {
		t.Fatalf("registration count = %d, want %d", len(registrations), len(want))
	}
	for _, registration := range registrations {
		argument, ok := want[registration.verb]
		if !ok {
			t.Errorf("unexpected registration %q", registration.verb)
			continue
		}
		if registration.argument != argument {
			t.Errorf("argument for %q = %q, want %q", registration.verb, registration.argument, argument)
		}
		delete(want, registration.verb)
	}
	for verb := range want {
		t.Errorf("missing registration %q", verb)
	}
}
