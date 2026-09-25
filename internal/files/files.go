package files

import (
	"fmt"
	"path"
	"strings"
)

var allowedMIME = map[string]bool{
	"image/png": true, "image/jpeg": true, "image/webp": true,
	"application/pdf": true, "text/csv": true,
}

const maxBytes = 10 << 20 // 10 MB Phase 1 limit

// Validate checks MIME + size before any storage. Never trust client filename.
func Validate(mime string, sizeBytes int64) error {
	if !allowedMIME[mime] {
		return fmt.Errorf("mime %q not allowed: png, jpeg, webp, pdf, csv only", mime)
	}
	if sizeBytes < 0 || sizeBytes > maxBytes {
		return fmt.Errorf("file must be 0-10MB, got %d bytes", sizeBytes)
	}
	return nil
}

// ObjectKey returns a random namespaced key; never uses the raw client filename.
func ObjectKey(orgID, randomHex, clientName string) string {
	ext := strings.ToLower(path.Ext(clientName))
	if len(ext) > 8 {
		ext = ""
	}
	return fmt.Sprintf("org/%s/%s%s", orgID, randomHex, ext)
}
