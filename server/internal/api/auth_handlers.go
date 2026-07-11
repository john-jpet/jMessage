package api

import (
	"net/http"

	"jmessage/internal/auth"
	"jmessage/internal/model"
	"jmessage/internal/validation"
)

type credentials struct {
	Username    string `json:"username"`
	DisplayName string `json:"displayName"`
	Password    string `json:"password"`
}

type sessionResponse struct {
	Token string            `json:"token"`
	User  model.UserSummary `json:"user"`
}

func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	var req credentials
	if !readJSON(w, r, &req) {
		return
	}
	username, err := validation.Username(req.Username)
	if err != nil {
		writeErr(w, http.StatusBadRequest, userMsg(err))
		return
	}
	req.Username = username
	// Display name is optional; empty means the username is shown.
	displayName, err := validation.DisplayName(req.DisplayName)
	if err != nil {
		writeErr(w, http.StatusBadRequest, userMsg(err))
		return
	}
	req.DisplayName = displayName
	if len(req.Password) < 8 {
		writeErr(w, http.StatusBadRequest, "password must be at least 8 characters")
		return
	}

	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		storeErr(w, err)
		return
	}
	u, err := s.Store.CreateUser(req.Username, req.DisplayName, hash)
	if err != nil {
		storeErr(w, err)
		return
	}
	s.session(w, u)
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req credentials
	if !readJSON(w, r, &req) {
		return
	}
	u, err := s.Store.GetUserByUsername(req.Username)
	if err != nil {
		// Same response as a wrong password: no username probing.
		writeErr(w, http.StatusUnauthorized, "invalid credentials")
		return
	}
	ok, err := auth.VerifyPassword(req.Password, u.PasswordHash)
	if err != nil || !ok {
		writeErr(w, http.StatusUnauthorized, "invalid credentials")
		return
	}
	s.session(w, u)
}

func (s *Server) session(w http.ResponseWriter, u *model.User) {
	token, err := s.Tokens.Mint(u.ID)
	if err != nil {
		storeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, sessionResponse{Token: token, User: u.Summary()})
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	u, err := s.Store.GetUser(auth.UserID(r.Context()))
	if err != nil {
		storeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, u.Summary())
}

func (s *Server) handleUserLookup(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("username")
	u, err := s.Store.GetUserByUsername(name)
	if err != nil {
		storeErr(w, err)
		return
	}
	sum := u.Summary()
	sum.Online = s.Presence.IsOnline(u.ID)
	writeJSON(w, http.StatusOK, sum)
}
