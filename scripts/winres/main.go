// Command winres generates a Windows resource object (.syso) that `go build`
// links into an mdv executable. It embeds:
//
//   - the application icon at resource ID 3 (the numeric value of RT_ICON, which
//     is the ID the Wails v3 Windows backend looks up for the window / App-
//     switcher / title-bar icon — see webview_window_windows.go). The same icon
//     resource is what Windows Explorer shows for the .exe file and any desktop
//     shortcut to it.
//   - an application manifest (per-monitor-v2 DPI awareness, common controls v6,
//     asInvoker, long-path aware).
//   - version information (product/file version + name + description).
//
// This is a *separate* Go module so its build-only dependency (winres and its
// image-resizing transitive deps) stays out of mdv's main go.mod. It is invoked
// from scripts/build.ps1 before each Windows `go build`. Run it from this
// directory, e.g.:
//
//	go run . -icon ../../images/icon.png -out ../../gui/rsrc_windows_amd64.syso \
//	    -version 0.7.2 -description "Markdown Document Viewer (GUI)"
package main

import (
	"flag"
	"fmt"
	"image"
	_ "image/png"
	"os"

	"github.com/tc-hib/winres"
	"github.com/tc-hib/winres/version"
)

func main() {
	icon := flag.String("icon", "", "path to the source PNG icon (required)")
	out := flag.String("out", "", "output .syso path (required)")
	ver := flag.String("version", "0.0.0", "product/file version, e.g. 1.2.3")
	desc := flag.String("description", "", "file description shown in Explorer")
	arch := flag.String("arch", "amd64", "target architecture: amd64 or arm64")
	flag.Parse()

	if *icon == "" || *out == "" {
		fmt.Fprintln(os.Stderr, "winres: -icon and -out are required")
		os.Exit(2)
	}
	if err := generate(*icon, *out, *ver, *desc, *arch); err != nil {
		fmt.Fprintln(os.Stderr, "winres:", err)
		os.Exit(1)
	}
	fmt.Printf("winres: wrote %s\n", *out)
}

func generate(iconPath, outPath, ver, desc, arch string) error {
	// Map the Go GOARCH name to the matching COFF machine type. The .syso must
	// be built for the same architecture as the `go build` that links it, or the
	// linker rejects the object.
	var machine winres.Arch
	switch arch {
	case "amd64":
		machine = winres.ArchAMD64
	case "arm64":
		machine = winres.ArchARM64
	default:
		return fmt.Errorf("unsupported arch %q (want amd64 or arm64)", arch)
	}

	f, err := os.Open(iconPath)
	if err != nil {
		return err
	}
	img, _, err := image.Decode(f)
	_ = f.Close()
	if err != nil {
		return fmt.Errorf("decode icon %q: %w", iconPath, err)
	}

	// nil sizes -> winres.DefaultIconSizes (16,24,32,48,64,128,256), which is the
	// full set Explorer and the taskbar need at any DPI.
	ico, err := winres.NewIconFromResizedImage(img, nil)
	if err != nil {
		return fmt.Errorf("build icon: %w", err)
	}

	rs := &winres.ResourceSet{}
	if err := rs.SetIcon(winres.RT_ICON, ico); err != nil {
		return fmt.Errorf("set icon: %w", err)
	}

	rs.SetManifest(winres.AppManifest{
		DPIAwareness:        winres.DPIPerMonitorV2,
		UseCommonControlsV6: true,
		ExecutionLevel:      winres.AsInvoker,
		LongPathAware:       true,
	})

	var vi version.Info
	vi.SetFileVersion(ver)
	vi.SetProductVersion(ver)
	_ = vi.Set(version.LangNeutral, version.ProductName, "mdv")
	if desc != "" {
		_ = vi.Set(version.LangNeutral, version.FileDescription, desc)
	}
	rs.SetVersionInfo(vi)

	out, err := os.Create(outPath)
	if err != nil {
		return err
	}
	if err := rs.WriteObject(out, machine); err != nil {
		_ = out.Close()
		return fmt.Errorf("write %q: %w", outPath, err)
	}
	return out.Close()
}
