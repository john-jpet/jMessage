package api

import (
	"net/http"
	"testing"

	"jmessage/internal/model"
)

type syncTestPage struct {
	Changes []struct {
		Conversation convResponse    `json:"conversation"`
		Messages     []model.Message `json:"messages"`
		HasMore      bool            `json:"hasMore"`
		LatestSeq    uint64          `json:"latestSeq"`
	} `json:"changes"`
}

func TestSync(t *testing.T) {
	e := newEnv(t)
	aTok, aUser := e.register("alice")
	bTok, bUser := e.register("bob")
	cTok, _ := e.register("carol")

	var dm convResponse
	e.call("POST", "/api/conversations", aTok,
		map[string]any{"type": "dm", "participantIDs": []string{bUser.ID}}, &dm)
	seedMessages(t, e, dm.ID, aUser.ID, 10)

	// Client knows nothing: the conversation arrives whole.
	var p syncTestPage
	if code := e.call("POST", "/api/sync", bTok,
		map[string]any{"conversations": []any{}}, &p); code != http.StatusOK {
		t.Fatalf("sync status %d", code)
	}
	if len(p.Changes) != 1 || len(p.Changes[0].Messages) != 10 || p.Changes[0].LatestSeq != 10 {
		t.Fatalf("cold sync = %+v", p)
	}
	if p.Changes[0].Conversation.ID != dm.ID {
		t.Fatalf("sync conv = %s", p.Changes[0].Conversation.ID)
	}

	// Client holds through seq 4: gets 5..10.
	p = syncTestPage{}
	e.call("POST", "/api/sync", bTok,
		map[string]any{"conversations": []any{map[string]any{"id": dm.ID, "lastSeq": 4}}}, &p)
	if len(p.Changes) != 1 || len(p.Changes[0].Messages) != 6 || p.Changes[0].Messages[0].Seq != 5 {
		t.Fatalf("partial sync = %+v", p.Changes)
	}

	// Fully caught up: no changes at all.
	p = syncTestPage{}
	e.call("POST", "/api/sync", bTok,
		map[string]any{"conversations": []any{map[string]any{"id": dm.ID, "lastSeq": 10}}}, &p)
	if len(p.Changes) != 0 {
		t.Fatalf("caught-up sync returned %d changes", len(p.Changes))
	}

	// Carol names a conversation she is not a member of: silently
	// skipped, nothing leaks.
	p = syncTestPage{}
	e.call("POST", "/api/sync", cTok,
		map[string]any{"conversations": []any{map[string]any{"id": dm.ID, "lastSeq": 0}}}, &p)
	if len(p.Changes) != 0 {
		t.Fatalf("non-member sync leaked %d changes", len(p.Changes))
	}
}

func TestConversationUnreadCounts(t *testing.T) {
	e := newEnv(t)
	aTok, aUser := e.register("alice")
	bTok, bUser := e.register("bob")

	var dm convResponse
	e.call("POST", "/api/conversations", aTok,
		map[string]any{"type": "dm", "participantIDs": []string{bUser.ID}}, &dm)
	seedMessages(t, e, dm.ID, aUser.ID, 5)

	var convs []convResponse
	e.call("GET", "/api/conversations", bTok, nil, &convs)
	if len(convs) != 1 || convs[0].LastSeq != 5 || convs[0].Unread != 5 || convs[0].MyReadSeq != 0 {
		t.Fatalf("unread convs = %+v", convs)
	}

	// Bob reads through seq 3.
	if _, _, err := e.store.SetRead(bUser.ID, dm.ID, 3); err != nil {
		t.Fatal(err)
	}
	e.call("GET", "/api/conversations", bTok, nil, &convs)
	if convs[0].Unread != 2 || convs[0].MyReadSeq != 3 {
		t.Fatalf("after read: %+v", convs[0])
	}
}

func TestReceiptsEndpoint(t *testing.T) {
	e := newEnv(t)
	aTok, aUser := e.register("alice")
	_, bUser := e.register("bob")
	cTok, _ := e.register("carol")

	var dm convResponse
	e.call("POST", "/api/conversations", aTok,
		map[string]any{"type": "dm", "participantIDs": []string{bUser.ID}}, &dm)
	seedMessages(t, e, dm.ID, aUser.ID, 5)
	e.store.SetRead(bUser.ID, dm.ID, 4)

	var receipts []struct {
		UserID       string `json:"userID"`
		DeliveredSeq uint64 `json:"deliveredSeq"`
		ReadSeq      uint64 `json:"readSeq"`
	}
	if code := e.call("GET", "/api/conversations/"+dm.ID+"/receipts", aTok, nil, &receipts); code != http.StatusOK {
		t.Fatalf("receipts status %d", code)
	}
	if len(receipts) != 2 {
		t.Fatalf("receipts = %+v", receipts)
	}
	byUser := map[string]uint64{}
	for _, r := range receipts {
		byUser[r.UserID] = r.ReadSeq
	}
	if byUser[bUser.ID] != 4 || byUser[aUser.ID] != 0 {
		t.Fatalf("watermarks = %v", byUser)
	}

	// Non-members get 403.
	if code := e.call("GET", "/api/conversations/"+dm.ID+"/receipts", cTok, nil, nil); code != http.StatusForbidden {
		t.Fatalf("non-member receipts status %d", code)
	}
}
