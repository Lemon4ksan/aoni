// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package netutil provides network layer security helpers, filename sanitization, and transport utilities.
package netutil

import (
	"mime"
	"path/filepath"
	"strings"
)

// SanitizeFileName cleans a string by stripping path traversal sequences, null bytes, and path separators.
func SanitizeFileName(filename string) string {
	filename = strings.ReplaceAll(filename, "\x00", "")
	filename = strings.TrimSpace(filename)
	filename = filepath.Base(filepath.Clean(filename))

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

// ExtractSanitizedFilename extracts and cleans the filename parameter from a Content-Disposition HTTP header.
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

	if idx := strings.Index(filename, "''"); idx != -1 {
		filename = filename[idx+2:]
	}

	return SanitizeFileName(filename)
}
