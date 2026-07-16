// Package buildinfo exposes release metadata injected at link time.
package buildinfo

// Version is overridden for release builds with -ldflags -X.
var Version = "dev"
