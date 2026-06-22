package main

import (
	"runtime"

	"github.com/thgossler/mdv/internal/core"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// buildMenu constructs the native application menu. Menu actions are forwarded
// to the frontend via events so the UI owns the behaviour in one place.
func buildMenu(app *application.App) *application.Menu {
	menu := app.NewMenu()

	if runtime.GOOS == "darwin" {
		menu.AddRole(application.AppMenu)
	}

	// File menu.
	fileMenu := menu.AddSubmenu("File")
	fileMenu.Add("Open File…").SetAccelerator("CmdOrCtrl+O").OnClick(func(*application.Context) {
		app.Event.Emit("menu:open-file", "")
	})
	fileMenu.Add("Open Folder…").SetAccelerator("CmdOrCtrl+Shift+O").OnClick(func(*application.Context) {
		app.Event.Emit("menu:open-folder", "")
	})
	fileMenu.Add("Open in New Window").SetAccelerator("CmdOrCtrl+N").OnClick(func(*application.Context) {
		app.Event.Emit("menu:new-window", "")
	})
	fileMenu.AddSeparator()
	fileMenu.Add("Reload").SetAccelerator("CmdOrCtrl+R").OnClick(func(*application.Context) {
		app.Event.Emit("menu:reload", "")
	})
	if runtime.GOOS != "darwin" {
		fileMenu.AddSeparator()
		fileMenu.Add("Quit").SetAccelerator("CmdOrCtrl+Q").OnClick(func(*application.Context) {
			app.Quit()
		})
	}

	// Edit menu (Copy/Paste/Select All roles).
	menu.AddRole(application.EditMenu)

	// View menu.
	viewMenu := menu.AddSubmenu("View")
	viewMenu.Add("Toggle Sidebar").SetAccelerator("CmdOrCtrl+B").OnClick(func(*application.Context) {
		app.Event.Emit("menu:toggle-sidebar", "")
	})
	viewMenu.Add("Toggle Table of Contents").SetAccelerator("CmdOrCtrl+Shift+T").OnClick(func(*application.Context) {
		app.Event.Emit("menu:toggle-toc", "")
	})
	viewMenu.Add("Toggle Backlinks").OnClick(func(*application.Context) {
		app.Event.Emit("menu:toggle-backlinks", "")
	})
	viewMenu.AddSeparator()
	viewMenu.Add("Toggle Theme").SetAccelerator("CmdOrCtrl+J").OnClick(func(*application.Context) {
		app.Event.Emit("menu:toggle-theme", "")
	})
	viewMenu.Add("Toggle Filename / Title").SetAccelerator("CmdOrCtrl+T").OnClick(func(*application.Context) {
		app.Event.Emit("menu:toggle-labels", "")
	})
	viewMenu.Add("Toggle Monospace").OnClick(func(*application.Context) {
		app.Event.Emit("menu:toggle-mono", "")
	})
	viewMenu.AddSeparator()
	viewMenu.Add("Zoom In").SetAccelerator("CmdOrCtrl+Plus").OnClick(func(*application.Context) {
		app.Event.Emit("menu:zoom-in", "")
	})
	viewMenu.Add("Zoom Out").SetAccelerator("CmdOrCtrl+-").OnClick(func(*application.Context) {
		app.Event.Emit("menu:zoom-out", "")
	})
	viewMenu.Add("Reset Zoom").SetAccelerator("CmdOrCtrl+0").OnClick(func(*application.Context) {
		app.Event.Emit("menu:zoom-reset", "")
	})

	// Navigate menu.
	navMenu := menu.AddSubmenu("Navigate")
	navMenu.Add("Back").SetAccelerator("CmdOrCtrl+Left").OnClick(func(*application.Context) {
		app.Event.Emit("menu:back", "")
	})
	navMenu.Add("Forward").SetAccelerator("CmdOrCtrl+Right").OnClick(func(*application.Context) {
		app.Event.Emit("menu:forward", "")
	})
	navMenu.Add("Find…").SetAccelerator("CmdOrCtrl+F").OnClick(func(*application.Context) {
		app.Event.Emit("menu:find", "")
	})

	if runtime.GOOS == "darwin" {
		menu.AddRole(application.WindowMenu)
	}

	// Help menu.
	helpMenu := menu.AddSubmenu("Help")
	helpMenu.Add(core.AppName + " on GitHub").OnClick(func(*application.Context) {
		_ = core.OpenInOS("https://github.com/" + core.DefaultSettings().UpdateRepo)
	})

	return menu
}
