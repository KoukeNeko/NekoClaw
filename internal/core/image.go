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

// detectMimeType resolves MIME type by content sniffing first, then by extension.
// Content-based detection is preferred because file extensions can be misleading
// (e.g. a PNG file saved with .jpg extension), and sending an incorrect MIME type
// causes API rejections.
func detectMimeType(path string, data []byte) string {
	// Prefer content-based detection for accuracy.
	if detected := DetectImageMimeType(data); detected != "" {
		return detected
	}
	// Fallback: extension-based lookup.
	ext := strings.ToLower(filepath.Ext(path))
	if mime, ok := SupportedImageExtensions[ext]; ok {
		return mime
	}
	return http.DetectContentType(data)
}

// DetectImageMimeType inspects the magic bytes of raw image data and returns
// the correct MIME type. Returns "" if the format is not recognized.
func DetectImageMimeType(data []byte) string {
	if len(data) < 8 {
		return ""
	}
	// PNG: 89 50 4E 47 0D 0A 1A 0A
	if data[0] == 0x89 && data[1] == 0x50 && data[2] == 0x4E && data[3] == 0x47 {
		return "image/png"
	}
	// JPEG: FF D8 FF
	if data[0] == 0xFF && data[1] == 0xD8 && data[2] == 0xFF {
		return "image/jpeg"
	}
	// GIF: GIF87a or GIF89a
	if data[0] == 'G' && data[1] == 'I' && data[2] == 'F' {
		return "image/gif"
	}
	// WebP: RIFF....WEBP
	if len(data) >= 12 && data[0] == 'R' && data[1] == 'I' && data[2] == 'F' && data[3] == 'F' &&
		data[8] == 'W' && data[9] == 'E' && data[10] == 'B' && data[11] == 'P' {
		return "image/webp"
	}
	// BMP: BM
	if data[0] == 'B' && data[1] == 'M' {
		return "image/bmp"
	}
	return ""
}

// ValidateImageMimeTypes decodes the base64 data of each image and corrects
// the MIME type if it doesn't match the actual image content. This prevents
// API errors like "image was specified using image/jpeg but appears to be image/png".
func ValidateImageMimeTypes(images []ImageData) {
	for i := range images {
		raw, err := base64.StdEncoding.DecodeString(images[i].Data)
		if err != nil {
			continue
		}
		if detected := DetectImageMimeType(raw); detected != "" && detected != images[i].MimeType {
			images[i].MimeType = detected
		}
	}
}

// ValidateMessageImageMimeTypes validates and corrects MIME types for images
// in all messages. This covers images stored in conversation history that may
// have been persisted with incorrect MIME types.
func ValidateMessageImageMimeTypes(messages []Message) {
	for i := range messages {
		if len(messages[i].Images) > 0 {
			ValidateImageMimeTypes(messages[i].Images)
		}
	}
}
