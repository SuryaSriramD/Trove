package native

import "errors"

// ErrDocumentUnavailable denotes an intentionally disabled capability, not a
// damaged installation. Do not fall back to an ambient PDF process on this error.
var ErrDocumentUnavailable = errors.New("Linux PDF text extraction and PDF export are unavailable until whole-process containment is implemented; use the PDF viewer or export HTML. Original files are retained")

func documentPlatformCapability(platform string) error {
	if platform == "linux" {
		return ErrDocumentUnavailable
	}
	return nil
}
