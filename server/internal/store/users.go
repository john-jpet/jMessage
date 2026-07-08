package store

import (
	"errors"
	"time"

	petdb "petdb"

	"jmessage/internal/model"
)

// CreateUser registers a user, enforcing username uniqueness. The
// check-then-put is safe because regMu serializes registrations (a
// single process owns the DB). Write order: user doc BEFORE the
// username index — a crash in between leaves an unreachable doc (the
// name stays registerable), never an index pointing at nothing.
func (s *Store) CreateUser(username, displayName, passwordHash string) (*model.User, error) {
	s.regMu.Lock()
	defer s.regMu.Unlock()

	if _, err := s.db.Get(usernameKey(username)); err == nil {
		return nil, ErrUsernameTaken
	} else if !errors.Is(err, petdb.ErrNotFound) {
		return nil, err
	}

	u := &model.User{
		ID:           pad10(s.users.alloc()),
		Username:     username,
		DisplayName:  displayName,
		PasswordHash: passwordHash,
		CreatedAt:    time.Now().UnixMilli(),
	}
	if err := s.putJSON(userKey(u.ID), u); err != nil {
		return nil, err
	}
	if err := s.db.Put(usernameKey(username), []byte(u.ID)); err != nil {
		return nil, err
	}
	if err := s.users.persist(mustID(u.ID)); err != nil {
		return nil, err
	}
	return u, nil
}

// GetUser loads a user by ID.
func (s *Store) GetUser(uid string) (*model.User, error) {
	var u model.User
	if err := s.getJSON(userKey(uid), &u); err != nil {
		return nil, err
	}
	return &u, nil
}

// GetUserByUsername resolves the username index then loads the user.
func (s *Store) GetUserByUsername(name string) (*model.User, error) {
	v, err := s.db.Get(usernameKey(name))
	if errors.Is(err, petdb.ErrNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return s.GetUser(string(v))
}
