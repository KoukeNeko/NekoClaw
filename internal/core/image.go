package core

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// SupportedImageExtensions lists file extensions recognized as images.
var SupportedImageExtensions = map[string]string{
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".gif":  "image/gif",
	".webp": "image/webp",
	".bmp":  "image/bmp",
}

const maxImageFileSize = 20 * 1024 * 1024 // 20 MB

// IsImagePath checks whether a file path has a recognized image extension.
func IsImagePath(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	_, ok := SupportedImageExtensions[ext]
	return ok
}

// LoadImageFromPath reads an image file and returns base64-encoded ImageData.
func LoadImageFromPath(path string) (ImageData, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return ImageData{}, fmt.Errorf("empty image path")
	}

	// Expand ~ to home directory
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			path = filepath.Join(home, path[2:])
		}
	}

	info, err := os.Stat(path)
	if err != nil {
		return ImageData{}, fmt.Errorf("cannot access image: %w", err)
	}
	if info.IsDir() {
		return ImageData{}, fmt.Errorf("path is a directory, not an image file")
	}
	if info.Size() > maxImageFileSize {
		return ImageData{}, fmt.Errorf("image too large (%d MB, max %d MB)",
			info.Size()/(1024*1024), maxImageFileSize/(1024*1024))
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return ImageData{}, fmt.Errorf("failed to read image: %w", err)
	}

	mimeType := detectMimeType(path, data)
	encoded := base64.StdEncoding.EncodeToString(data)
	fileName := filepath.Base(path)

	return ImageData{
		MimeType: mimeType,
		Data:     encoded,
		FileName: fileName,
	}, nil
}

// detectMimeType resolves MIME type by extension first, then by content sniffing.
func detectMimeType(path string, data []byte) string {
	ext := strings.ToLower(filepath.Ext(path))
	if mime, ok := SupportedImageExtensions[ext]; ok {
		return mime
	}
	// Fallback: content-based detection
	return http.DetectContentType(data)
}
