package api

import (
	"errors"
	"image"
	_ "image/gif"  // avatar dimension decoding
	_ "image/jpeg" // avatar dimension decoding
	_ "image/png"  // avatar dimension decoding
	"net/http"

	"jmessage/internal/auth"
	"jmessage/internal/model"
	"jmessage/internal/validation"
)

// settingsProfile is the settings-page view of the caller's own record:
// unlike UserSummary it exposes the RAW display name (which may be
// empty), so the page can distinguish "unset" from "same as username".
type settingsProfile struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"displayName"`
	Status      string `json:"status"`
	AvatarID    string `json:"avatarID"`
	CreatedAt   int64  `json:"createdAt"`
}

func toSettingsProfile(u *model.User) settingsProfile {
	return settingsProfile{
		ID:          u.ID,
		Username:    u.Username,
		DisplayName: u.DisplayName,
		Status:      u.Status,
		AvatarID:    u.AvatarID,
		CreatedAt:   u.CreatedAt,
	}
}

func (s *Server) handleGetProfileSettings(w http.ResponseWriter, r *http.Request) {
	u, err := s.Store.GetUser(auth.UserID(r.Context()))
	if err != nil {
		storeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toSettingsProfile(u))
}

// handlePatchProfileSettings updates profile fields. Pointer fields:
// absent leaves a value unchanged; an empty string clears it (display
// name → username shown; avatarAttachmentID → avatar removed).
func (s *Server) handlePatchProfileSettings(w http.ResponseWriter, r *http.Request) {
	uid := auth.UserID(r.Context())
	var req struct {
		DisplayName        *string `json:"displayName"`
		Status             *string `json:"status"`
		AvatarAttachmentID *string `json:"avatarAttachmentID"`
	}
	if !readJSON(w, r, &req) {
		return
	}

	// Validate everything before mutating anything.
	if req.DisplayName != nil {
		v, err := validation.DisplayName(*req.DisplayName)
		if err != nil {
			writeErr(w, http.StatusBadRequest, userMsg(err))
			return
		}
		*req.DisplayName = v
	}
	if req.Status != nil {
		v, err := validation.Status(*req.Status)
		if err != nil {
			writeErr(w, http.StatusBadRequest, userMsg(err))
			return
		}
		*req.Status = v
	}
	var newAvatar *model.Attachment
	if req.AvatarAttachmentID != nil && *req.AvatarAttachmentID != "" {
		att, err := s.validateAvatarAttachment(uid, *req.AvatarAttachmentID)
		if err != nil {
			writeErr(w, http.StatusBadRequest, userMsg(err))
			return
		}
		newAvatar = att
	}

	// Claim the new avatar first (leak-safe direction: a crash here
	// leaves an unreferenced avatar-state attachment for the janitor,
	// never a user doc pointing at something prunable).
	if newAvatar != nil {
		newAvatar.State = model.AttachmentAvatar
		if err := s.Store.CreateAttachment(newAvatar); err != nil {
			storeErr(w, err)
			return
		}
	}

	var oldAvatarID string
	u, err := s.Store.UpdateUser(uid, func(u *model.User) error {
		if req.DisplayName != nil {
			u.DisplayName = *req.DisplayName
		}
		if req.Status != nil {
			u.Status = *req.Status
		}
		if req.AvatarAttachmentID != nil {
			oldAvatarID = u.AvatarID
			if newAvatar != nil {
				u.AvatarID = newAvatar.ID
			} else {
				u.AvatarID = "" // removal
			}
		}
		return nil
	})
	if err != nil {
		storeErr(w, err)
		return
	}

	// Reclaim the replaced avatar (best-effort; janitor covers crashes).
	if req.AvatarAttachmentID != nil && oldAvatarID != "" && oldAvatarID != u.AvatarID {
		if err := s.Blobs.Remove(oldAvatarID); err == nil {
			s.Store.DeleteAttachment(oldAvatarID)
		}
	}

	writeJSON(w, http.StatusOK, toSettingsProfile(u))
}

// validateAvatarAttachment enforces the avatar rules: exists, owned by
// the caller, not attached elsewhere (Pending), supported image type,
// sane dimensions.
func (s *Server) validateAvatarAttachment(uid, id string) (*model.Attachment, error) {
	att, err := s.Store.GetAttachment(id)
	if err != nil {
		return nil, errors.New("validation: avatar attachment not found")
	}
	if att.OwnerID != uid {
		return nil, errors.New("validation: avatar attachment belongs to another user")
	}
	if att.State != model.AttachmentPending {
		return nil, errors.New("validation: avatar attachment is already in use")
	}
	if att.Size > validation.AvatarMaxBytes {
		return nil, errors.New("validation: avatar exceeds the 5 MB limit")
	}
	if err := validation.AvatarMime(att.MimeType); err != nil {
		return nil, err
	}
	f, err := s.Blobs.Open(att.ID)
	if err != nil {
		return nil, errors.New("validation: avatar bytes missing")
	}
	defer f.Close()
	cfg, _, err := image.DecodeConfig(f)
	if err != nil {
		return nil, errors.New("validation: avatar image could not be decoded")
	}
	if err := validation.AvatarDimensions(cfg.Width, cfg.Height); err != nil {
		return nil, err
	}
	return att, nil
}
