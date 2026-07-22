// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license can be found in the LICENSE file.

package netutil

import (
	"mime"
	"path/filepath"
	"strings"
)

// SanitizeFileName strips path traversal sequences (../, ..\), path separators,
// null bytes, and dangerous characters from a filename string.
func SanitizeFileName(filename string) string {
	// Strip null bytes and control chars
	filename = strings.ReplaceAll(filename, "\x00", "")
	filename = strings.TrimSpace(filename)

	// Extract base filename only
	filename = filepath.Base(filepath.Clean(filename))

	// Clean out any remaining path separators or traversalDots
	for strings.HasPrefix(filename, "..") || strings.HasPrefix(filename, ".") {
		filename = strings.TrimPrefix(filename, "..")
		filename = strings.TrimPrefix(filename, ".")
	}

	filename = strings.TrimSpace(filename)
	if filename == "" || filename == "." || filename == "/" || filename == "\\" {
		return "downloaded_file"
	}

	return filename
}

// ExtractSanitizedFilename parses Content-Disposition header and returns a safe, sanitized filename.
func ExtractSanitizedFilename(contentDispositionHeader string) string {
	if contentDispositionHeader == "" {
		return "downloaded_file"
	}

	_, params, err := mime.ParseMediaType(contentDispositionHeader)
	if err != nil {
		return "downloaded_file"
	}

	filename, ok := params["filename*"]
	if !ok || filename == "" {
		filename = params["filename"]
	}

	if filename == "" {
		return "downloaded_file"
	}

	// Clean UTF-8 filename* encoding if present (e.g. UTF-8''filename.txt)
	if idx := strings.Index(filename, "''"); idx != -1 {
		filename = filename[idx+2:]
	}

	return SanitizeFileName(filename)
}
