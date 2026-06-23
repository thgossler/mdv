// Separate module so winres (a Windows-resource build tool) and its transitive
// image-resizing dependencies stay out of mdv's main go.mod. See main.go.
module mdv-winres-gen

go 1.26

require github.com/tc-hib/winres v0.3.1

require (
	github.com/nfnt/resize v0.0.0-20180221191011-83c6a9932646 // indirect
	golang.org/x/image v0.38.0 // indirect
)
