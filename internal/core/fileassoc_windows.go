//go:build windows

package core

import (
	"fmt"

	"golang.org/x/sys/windows/registry"
)

// registerFileAssociations adds an "Open with mdv" verb to the right-click menu
// of Markdown files in File Explorer. It writes per-user keys under
// HKCU\Software\Classes\SystemFileAssociations\<ext>\shell, which augments the
// context menu without changing the file's default program, and needs no admin
// rights. The entry is per extension so .md, .mdx and .markdown each get it.
func registerFileAssociations(exe string) error {
	command := fmt.Sprintf("\"%s\" --gui \"%%1\"", exe)
	icon := fmt.Sprintf("\"%s\",0", exe)

	for _, ext := range markdownExtensions {
		verb := `Software\Classes\SystemFileAssociations\` + ext + `\shell\mdv`

		k, _, err := registry.CreateKey(registry.CURRENT_USER, verb, registry.SET_VALUE)
		if err != nil {
			return err
		}
		if err := k.SetStringValue("", "Open with mdv"); err != nil {
			k.Close()
			return err
		}
		// Best-effort menu icon; ignore failures (the verb still works).
		_ = k.SetStringValue("Icon", icon)
		k.Close()

		ck, _, err := registry.CreateKey(registry.CURRENT_USER, verb+`\command`, registry.SET_VALUE)
		if err != nil {
			return err
		}
		if err := ck.SetStringValue("", command); err != nil {
			ck.Close()
			return err
		}
		ck.Close()
	}
	return nil
}
