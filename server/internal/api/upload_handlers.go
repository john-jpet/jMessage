package api

import (
	"errors"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"jmessage/internal/auth"
	"jmessage/internal/model"
	"jmessage/internal/validation"
)

// DefaultMaxUploadBytes bounds one attachment (overridable via Server).
const DefaultMaxUploadBytes = validation.AttachmentMaxBytes

// uploadReadTimeout aborts stalled upload streams.
const uploadReadTimeout = 60 * time.Second

// handleUpload accepts the raw file as the request body (single
// streaming request: no multipart, no separate begin/commit round
// trips). Filename rides the X-Filename header; Content-Type is a hint
// (server sniffing wins when conclusive). The blob is durable on disk
// before its metadata record exists, so a crash between the two leaves
// only an orphan blob for the janitor.
func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	uid := auth.UserID(r.Context())

	max := s.MaxUploadBytes
	if max <= 0 {
		max = DefaultMaxUploadBytes
	}
	// Declared-size fast path: reject before reading a single byte.
	if r.ContentLength > max {
		writeErr(w, http.StatusRequestEntityTooLarge, "attachment exceeds size limit")
		return
	}
	// Stalled-stream guard: the whole body must arrive within the window.
	rc := http.NewResponseController(w)
	rc.SetReadDeadline(time.Now().Add(uploadReadTimeout))
	defer rc.SetReadDeadline(time.Time{})

	body := http.MaxBytesReader(w, r.Body, max)

	res, err := s.Blobs.Save(body, r.Header.Get("Content-Type"))
	if err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			writeErr(w, http.StatusRequestEntityTooLarge, "attachment exceeds size limit")
			return
		}
		s.Logger.Error("blob save", "err", err)
		writeErr(w, http.StatusInternalServerError, "upload failed")
		return
	}
	if res.Size == 0 {
		s.Blobs.Remove(res.ID)
		writeErr(w, http.StatusBadRequest, "empty upload")
		return
	}

	filename := sanitizeFilename(r.Header.Get("X-Filename"))

	att := &model.Attachment{
		ID:        res.ID,
		OwnerID:   uid,
		Filename:  filename,
		MimeType:  res.MimeType,
		Size:      res.Size,
		SHA256:    res.SHA256,
		State:     model.AttachmentPending,
		CreatedAt: time.Now().UnixMilli(),
	}
	if err := s.Store.CreateAttachment(att); err != nil {
		s.Blobs.Remove(res.ID) // metadata failed: reclaim the blob now
		storeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, att.Ref())
}

// handleDownload streams attachment bytes after authorization:
// Committed → requester must be a member of the attachment's
// conversation; Pending → owner only (preview before send).
func (s *Server) handleDownload(w http.ResponseWriter, r *http.Request) {
	uid, err := s.Tokens.VerifyRequest(r)
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "invalid token")
		return
	}
	id := chi.URLParam(r, "id")
	att, err := s.Store.GetAttachment(id)
	if err != nil {
		storeErr(w, err)
		return
	}

	allowed := false
	switch att.State {
	case model.AttachmentCommitted:
		ok, err := s.Store.IsMember(att.ConvID, uid)
		if err != nil {
			storeErr(w, err)
			return
		}
		allowed = ok
	case model.AttachmentPending:
		allowed = att.OwnerID == uid
	case model.AttachmentAvatar:
		allowed = true // avatars are visible to any authenticated user
	}
	if !allowed {
		writeErr(w, http.StatusForbidden, "not authorized for this attachment")
		return
	}

	f, err := s.Blobs.Open(att.ID)
	if err != nil {
		writeErr(w, http.StatusNotFound, "attachment bytes missing")
		return
	}
	defer f.Close()

	w.Header().Set("Content-Type", att.MimeType)
	w.Header().Set("ETag", `"`+att.SHA256+`"`)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	// The response is authorization-dependent: keep it out of shared
	// caches and key the browser cache on the Authorization header —
	// otherwise one user's cached 200 would be replayed for another
	// user (or an anonymous request) hitting the same URL.
	w.Header().Set("Cache-Control", "private, max-age=3600")
	w.Header().Set("Vary", "Authorization")
	disposition := "attachment"
	if isInlineMime(att.MimeType) {
		disposition = "inline"
	}
	w.Header().Set("Content-Disposition", disposition+`; filename="`+sanitizeHeaderName(att.Filename)+`"`)

	// ServeContent gives us range requests and If-None-Match for free.
	http.ServeContent(w, r, "", time.UnixMilli(att.CreatedAt), f)
}

// isInlineMime lists types safe to render in-browser; everything else
// downloads (defense against HTML/script smuggling).
func isInlineMime(mime string) bool {
	switch mime {
	case "image/png", "image/jpeg", "image/gif", "image/webp",
		"video/mp4", "audio/mpeg", "audio/wav", "application/pdf", "text/plain; charset=utf-8":
		return true
	}
	return false
}

// sanitizeFilename reduces a client-supplied filename to its base name,
// bounded to 120 bytes. path.Base only recognizes "/", so both
// separators are normalized first — path/filepath.Base is OS-dependent
// (it only splits on "\" when GOOS is windows) and would let a
// Windows-style traversal path through unchanged on a Linux server.
func sanitizeFilename(name string) string {
	name = strings.ReplaceAll(name, "\\", "/")
	name = path.Base(name)
	if name == "." || name == "/" || name == "" {
		name = "file"
	}
	if len(name) > 120 {
		name = name[len(name)-120:]
	}
	return name
}

// sanitizeHeaderName strips characters that would break the
// Content-Disposition header.
func sanitizeHeaderName(name string) string {
	out := make([]rune, 0, len(name))
	for _, r := range name {
		if r == '"' || r == '\\' || r < 0x20 {
			continue
		}
		out = append(out, r)
	}
	if len(out) == 0 {
		return "file"
	}
	return string(out)
}
