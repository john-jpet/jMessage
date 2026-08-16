package validation

// AttachmentMimes are the accepted message-attachment content types.
// Deliberately narrow: images and video only, so the blob store never
// becomes a general file host (no PDFs, archives, executables, etc.).
var AttachmentMimes = map[string]bool{
	"image/png":       true,
	"image/jpeg":      true,
	"image/gif":       true,
	"image/webp":      true,
	"video/mp4":       true,
	"video/webm":      true,
	"video/quicktime": true,
}

// AttachmentMime checks a sniffed content type against the allow-list.
func AttachmentMime(mime string) error {
	if !AttachmentMimes[mime] {
		return fail("attachments must be an image, GIF, or video")
	}
	return nil
}
