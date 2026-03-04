package core

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// LoadImageFromClipboard extracts an image from the system clipboard.
// Supports macOS (osascript) and Linux (xclip).
func LoadImageFromClipboard() (ImageData, error) {
	switch runtime.GOOS {
	case "darwin":
		return loadClipboardImageDarwin()
	case "linux":
		return loadClipboardImageLinux()
	default:
		return ImageData{}, fmt.Errorf("clipboard image not supported on %s", runtime.GOOS)
	}
}

// loadClipboardImageDarwin uses AppleScript to extract PNG data from clipboard.
func loadClipboardImageDarwin() (ImageData, error) {
	tmpFile := filepath.Join(os.TempDir(), "nekoclaw-clipboard.png")
	defer os.Remove(tmpFile)

	script := fmt.Sprintf(`
set theFile to POSIX file "%s"
try
    set theData to the clipboard as «class PNGf»
    set theFileRef to open for access theFile with write permission
    set eof theFileRef to 0
    write theData to theFileRef
    close access theFileRef
    return "ok"
on error errMsg
    return "error:" & errMsg
end try
`, tmpFile)

	out, err := exec.Command("osascript", "-e", script).Output()
	if err != nil {
		return ImageData{}, fmt.Errorf("failed to access clipboard: %w", err)
	}

	result := strings.TrimSpace(string(out))
	if strings.HasPrefix(result, "error:") {
		return ImageData{}, fmt.Errorf("no image in clipboard")
	}

	img, err := LoadImageFromPath(tmpFile)
	if err != nil {
		return ImageData{}, fmt.Errorf("failed to read clipboard image: %w", err)
	}

	// Replace temp filename with a meaningful name
	img.FileName = fmt.Sprintf("clipboard-%s.png", time.Now().Format("150405"))
	return img, nil
}

// loadClipboardImageLinux uses xclip to extract PNG data from clipboard.
func loadClipboardImageLinux() (ImageData, error) {
	// Check if xclip is available
	if _, err := exec.LookPath("xclip"); err != nil {
		return ImageData{}, fmt.Errorf("xclip not found — install with: sudo apt install xclip")
	}

	tmpFile := filepath.Join(os.TempDir(), "nekoclaw-clipboard.png")
	defer os.Remove(tmpFile)

	data, err := exec.Command("xclip", "-selection", "clipboard", "-t", "image/png", "-o").Output()
	if err != nil || len(data) == 0 {
		return ImageData{}, fmt.Errorf("no image in clipboard")
	}

	if writeErr := os.WriteFile(tmpFile, data, 0600); writeErr != nil {
		return ImageData{}, fmt.Errorf("failed to save clipboard image: %w", writeErr)
	}

	img, err := LoadImageFromPath(tmpFile)
	if err != nil {
		return ImageData{}, fmt.Errorf("failed to read clipboard image: %w", err)
	}

	img.FileName = fmt.Sprintf("clipboard-%s.png", time.Now().Format("150405"))
	return img, nil
}
