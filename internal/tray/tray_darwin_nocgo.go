//go:build darwin && !cgo
// +build darwin,!cgo

package tray

// Setup is a no-op when CGO is disabled on macOS.
func Setup(icon []byte, showFunc func(), hideFunc func(), quitFunc func(), language string) {
	// CGO disabled: tray not supported.
}

// Quit is a no-op when CGO is disabled on macOS.
func Quit() {
	// CGO disabled: tray not supported.
}

// UpdateLanguage is a no-op when CGO is disabled on macOS.
func UpdateLanguage(language string) {
	// CGO disabled: tray not supported.
}
