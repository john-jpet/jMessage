package store

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"time"

	petdb "petdb"

	"jmessage/internal/model"
)

func mustID(s string) uint64 {
	n, _ := strconv.ParseUint(s, 10, 64)
	return n
}

// CreateConversation creates a dm or group conversation with the given
// participants (creator included by the caller). DMs dedup via the
// dm:<a>:<b> key: creating an existing DM returns it.
//
// Write order (under convMu): conv doc → per participant member: then
// usermember: → dm dedup key → counter hint. member: is the
// authorization source of truth, so it lands before the usermember:
// index makes the conversation visible; the worst crash outcome is a
// conversation invisible to some participants — never a member who
// shouldn't be one.
func (s *Store) CreateConversation(typ, name, creatorID string, participantIDs []string) (*model.Conversation, error) {
	if typ != model.ConvDM && typ != model.ConvGroup {
		return nil, fmt.Errorf("store: bad conversation type %q", typ)
	}
	participants := dedupe(append([]string{creatorID}, participantIDs...))
	if typ == model.ConvDM && len(participants) != 2 {
		return nil, fmt.Errorf("store: a dm needs exactly 2 distinct participants")
	}

	s.convMu.Lock()
	defer s.convMu.Unlock()

	if typ == model.ConvDM {
		if v, err := s.db.Get(dmKey(participants[0], participants[1])); err == nil {
			return s.GetConversation(string(v)) // existing DM
		} else if !errors.Is(err, petdb.ErrNotFound) {
			return nil, err
		}
	}

	now := time.Now().UnixMilli()
	conv := &model.Conversation{
		ID:             pad10(s.convs.alloc()),
		Type:           typ,
		Name:           name,
		CreatorID:      creatorID,
		CreatedAt:      now,
		LastActivityTS: now,
	}
	if err := s.putJSON(convKey(conv.ID), conv); err != nil {
		return nil, err
	}
	for _, uid := range participants {
		if err := s.addMember(conv.ID, uid, now); err != nil {
			return nil, err
		}
	}
	if typ == model.ConvDM {
		if err := s.db.Put(dmKey(participants[0], participants[1]), []byte(conv.ID)); err != nil {
			return nil, err
		}
	}
	if err := s.convs.persist(mustID(conv.ID)); err != nil {
		return nil, err
	}
	return conv, nil
}

func (s *Store) addMember(cid, uid string, now int64) error {
	if err := s.putJSON(memberKey(cid, uid), &model.Member{JoinedAt: now}); err != nil {
		return err
	}
	return s.db.Put(userMemberKey(uid, cid), nil)
}

// AddMember adds a user to a group conversation.
func (s *Store) AddMember(cid, uid string) error {
	var conv model.Conversation
	if err := s.getJSON(convKey(cid), &conv); err != nil {
		return err
	}
	if conv.Type != model.ConvGroup {
		return fmt.Errorf("store: members can only be added to groups")
	}
	if _, err := s.GetUser(uid); err != nil {
		return err
	}
	return s.addMember(cid, uid, time.Now().UnixMilli())
}

// GetConversation loads conversation metadata.
func (s *Store) GetConversation(cid string) (*model.Conversation, error) {
	var c model.Conversation
	if err := s.getJSON(convKey(cid), &c); err != nil {
		return nil, err
	}
	return &c, nil
}

// IsMember reports whether uid belongs to conversation cid.
func (s *Store) IsMember(cid, uid string) (bool, error) {
	_, err := s.db.Get(memberKey(cid, uid))
	if errors.Is(err, petdb.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// ListMembers returns the user IDs of a conversation's members.
func (s *Store) ListMembers(cid string) ([]string, error) {
	prefix := memberPrefix(cid)
	it, err := s.db.Scan(prefix, prefixEnd(prefix))
	if err != nil {
		return nil, err
	}
	defer it.Close()
	var uids []string
	for ; it.Valid(); it.Next() {
		uids = append(uids, lastSegment(it.Key()))
	}
	return uids, it.Err()
}

// ListUserConversations returns the user's conversations, newest
// activity first: one usermember prefix scan, then a Get per
// conversation (µs-scale each; per-user counts are small at MVP scale).
func (s *Store) ListUserConversations(uid string) ([]model.Conversation, error) {
	prefix := userMemberPrefix(uid)
	it, err := s.db.Scan(prefix, prefixEnd(prefix))
	if err != nil {
		return nil, err
	}
	var cids []string
	for ; it.Valid(); it.Next() {
		cids = append(cids, lastSegment(it.Key()))
	}
	scanErr := it.Err()
	it.Close()
	if scanErr != nil {
		return nil, scanErr
	}

	convs := make([]model.Conversation, 0, len(cids))
	for _, cid := range cids {
		var c model.Conversation
		if err := s.getJSON(convKey(cid), &c); err != nil {
			if errors.Is(err, ErrNotFound) {
				continue // crash mid-create can leave a dangling index; skip
			}
			return nil, err
		}
		convs = append(convs, c)
	}
	sort.Slice(convs, func(i, j int) bool {
		return convs[i].LastActivityTS > convs[j].LastActivityTS
	})
	return convs, nil
}

func dedupe(ids []string) []string {
	seen := make(map[string]bool, len(ids))
	out := ids[:0]
	for _, id := range ids {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}
