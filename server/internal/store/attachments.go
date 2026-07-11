package store

import (
	"time"

	"jmessage/internal/model"
)

// CreateAttachment records freshly uploaded blob metadata (Pending).
func (s *Store) CreateAttachment(a *model.Attachment) error {
	return s.putJSON(attachmentKey(a.ID), a)
}

// GetAttachment loads attachment metadata.
func (s *Store) GetAttachment(id string) (*model.Attachment, error) {
	var a model.Attachment
	if err := s.getJSON(attachmentKey(id), &a); err != nil {
		return nil, err
	}
	return &a, nil
}

// DeleteAttachment removes the metadata record (the janitor removes the
// blob separately; order there is blob first, so a crash between leaves
// a stale Pending record that the next sweep retries).
func (s *Store) DeleteAttachment(id string) error {
	return s.db.Delete(attachmentKey(id))
}

// AttachmentExists reports whether a metadata record exists (used by
// the orphan-blob sweep).
func (s *Store) AttachmentExists(id string) (bool, error) {
	_, err := s.GetAttachment(id)
	if err == ErrNotFound {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// ListOrphanAvatarAttachments returns avatar-state attachments that are
// no longer referenced by their owner's profile — crash-window
// leftovers from avatar replacement. The age guard keeps in-flight
// profile PATCHes safe.
func (s *Store) ListOrphanAvatarAttachments(maxAge time.Duration) ([]model.Attachment, error) {
	cutoff := time.Now().Add(-maxAge).UnixMilli()
	prefix := []byte(attachmentPrefix)
	it, err := s.db.Scan(prefix, prefixEnd(prefix))
	if err != nil {
		return nil, err
	}
	var candidates []model.Attachment
	for ; it.Valid(); it.Next() {
		var a model.Attachment
		if err := jsonUnmarshal(it.Value(), &a); err != nil {
			continue
		}
		if a.State == model.AttachmentAvatar && a.CreatedAt < cutoff {
			candidates = append(candidates, a)
		}
	}
	scanErr := it.Err()
	it.Close() // close before the Gets below
	if scanErr != nil {
		return nil, scanErr
	}

	var orphans []model.Attachment
	for _, a := range candidates {
		owner, err := s.GetUser(a.OwnerID)
		if err != nil || owner.AvatarID != a.ID {
			orphans = append(orphans, a)
		}
	}
	return orphans, nil
}

// ListStalePendingAttachments returns attachments still Pending after
// maxAge — abandoned uploads whose message never arrived. Referenced
// attachments can't appear here: the send path commits them, and the
// allocator recovery scan re-commits any whose state flip was lost to
// a crash.
func (s *Store) ListStalePendingAttachments(maxAge time.Duration) ([]model.Attachment, error) {
	cutoff := time.Now().Add(-maxAge).UnixMilli()
	prefix := []byte(attachmentPrefix)
	it, err := s.db.Scan(prefix, prefixEnd(prefix))
	if err != nil {
		return nil, err
	}
	defer it.Close()
	var stale []model.Attachment
	for ; it.Valid(); it.Next() {
		var a model.Attachment
		if err := jsonUnmarshal(it.Value(), &a); err != nil {
			continue
		}
		if a.State == model.AttachmentPending && a.CreatedAt < cutoff {
			stale = append(stale, a)
		}
	}
	return stale, it.Err()
}
