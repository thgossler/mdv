//go:build windows

package core

import (
	"fmt"

	"golang.org/x/sys/windows/registry"
)

// registerFileAssociations adds an "Open with mdv" verb to Markdown files and
// folders in File Explorer. It writes per-user keys under HKCU\Software\Classes,
// which augments the context menus without changing default programs and needs
// no admin rights.
func registerFileAssociations(exe string) error {
	icon := fmt.Sprintf("\"%s\",0", exe)
	for _, registration := range windowsShellRegistrations() {
		if err := registerWindowsShellVerb(registration.verb, exe, registration.argument, icon); err != nil {
			return err
		}
	}
	return nil
}

type windowsShellRegistration struct {
	verb     string
	argument string
}

func windowsShellRegistrations() []windowsShellRegistration {
	registrations := make([]windowsShellRegistration, 0, len(markdownExtensions)+2)
	for _, ext := range markdownExtensions {
		registrations = append(registrations, windowsShellRegistration{
			verb:     `Software\Classes\SystemFileAssociations\` + ext + `\shell\mdv`,
			argument: "%1",
		})
	}
	return append(registrations,
		windowsShellRegistration{verb: `Software\Classes\Directory\shell\mdv`, argument: "%1"},
		windowsShellRegistration{verb: `Software\Classes\Directory\Background\shell\mdv`, argument: "%V"},
	)
}

func registerWindowsShellVerb(verb, exe, argument, icon string) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, verb, registry.SET_VALUE)
	if err != nil {
		return err
	}
	if err := k.SetStringValue("", "Open with mdv"); err != nil {
		k.Close()
		return err
	}
	_ = k.SetStringValue("Icon", icon)
	k.Close()

	command, _, err := registry.CreateKey(registry.CURRENT_USER, verb+`\command`, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer command.Close()
	return command.SetStringValue("", fmt.Sprintf("\"%s\" --gui \"%s\"", exe, argument))
}
