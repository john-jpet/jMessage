package api

import (
	"net/http"
	"testing"

	"jmessage/internal/model"
)

type profileTestResp struct {
	User                model.UserSummary `json:"user"`
	CreatedAt           int64             `json:"createdAt"`
	LastSeen            int64             `json:"lastSeen"`
	SharedConversations []convResponse    `json:"sharedConversations"`
}

func TestProfile(t *testing.T) {
	e := newEnv(t)
	aTok, aUser := e.register("alice")
	_, bUser := e.register("bob")
	cTok, _ := e.register("carol")

	var dm convResponse
	e.call("POST", "/api/conversations", aTok,
		map[string]any{"type": "dm", "participantIDs": []string{bUser.ID}}, &dm)
	if err := e.store.UpsertDevice(bUser.ID, "dev-bob", "Test Browser"); err != nil {
		t.Fatal(err)
	}

	// Alice views bob: join date + last seen populated, shared DM listed.
	var p profileTestResp
	if code := e.call("GET", "/api/users/"+bUser.ID+"/profile", aTok, nil, &p); code != http.StatusOK {
		t.Fatalf("profile status %d", code)
	}
	if p.User.Username != "bob" || p.CreatedAt == 0 || p.LastSeen == 0 {
		t.Fatalf("profile = %+v", p)
	}
	if len(p.SharedConversations) != 1 || p.SharedConversations[0].ID != dm.ID {
		t.Fatalf("shared = %+v", p.SharedConversations)
	}

	// Carol shares nothing with bob: empty list, but the profile itself
	// is still visible (public identity).
	p = profileTestResp{}
	e.call("GET", "/api/users/"+bUser.ID+"/profile", cTok, nil, &p)
	if p.User.Username != "bob" || len(p.SharedConversations) != 0 {
		t.Fatalf("stranger profile = %+v", p)
	}

	// Unknown user → 404; own profile → empty shared list by design.
	if code := e.call("GET", "/api/users/0000009999/profile", aTok, nil, nil); code != http.StatusNotFound {
		t.Fatalf("unknown user status %d", code)
	}
	p = profileTestResp{}
	e.call("GET", "/api/users/"+aUser.ID+"/profile", aTok, nil, &p)
	if len(p.SharedConversations) != 0 {
		t.Fatalf("own profile shared = %+v", p.SharedConversations)
	}
}

func TestPeerLastSeenDecoration(t *testing.T) {
	e := newEnv(t)
	aTok, _ := e.register("alice")
	_, bUser := e.register("bob")
	e.store.UpsertDevice(bUser.ID, "dev-b", "x")

	var dm convResponse
	e.call("POST", "/api/conversations", aTok,
		map[string]any{"type": "dm", "participantIDs": []string{bUser.ID}}, &dm)
	if dm.PeerLastSeen == 0 {
		t.Fatalf("peerLastSeen missing: %+v", dm)
	}
}
