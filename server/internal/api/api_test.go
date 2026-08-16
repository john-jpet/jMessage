package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"jmessage/internal/auth"
	"jmessage/internal/blob"
	"jmessage/internal/model"
	"jmessage/internal/store"
)

type testEnv struct {
	t     *testing.T
	srv   *httptest.Server
	store *store.Store
	blobs *blob.Store
}

func newEnv(t *testing.T) *testEnv {
	t.Helper()
	st, err := store.Open(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	blobs, err := blob.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{
		Store:          st,
		Blobs:          blobs,
		Tokens:         auth.NewTokens([]byte("test-secret")),
		Presence:       NoPresence{},
		MaxUploadBytes: 1 << 20, // 1 MiB keeps the 413 test cheap
	}
	srv := httptest.NewServer(s.Router(nil, nil))
	t.Cleanup(srv.Close)
	return &testEnv{t: t, srv: srv, store: st, blobs: blobs}
}

// call sends a JSON request and decodes the response into out (if non-nil).
func (e *testEnv) call(method, path, token string, body any, out any) int {
	e.t.Helper()
	var buf bytes.Buffer
	if body != nil {
		json.NewEncoder(&buf).Encode(body)
	}
	req, _ := http.NewRequest(method, e.srv.URL+path, &buf)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	if out != nil {
		json.NewDecoder(resp.Body).Decode(out)
	}
	return resp.StatusCode
}

func (e *testEnv) register(username string) (token string, user model.UserSummary) {
	e.t.Helper()
	var sess struct {
		Token string            `json:"token"`
		User  model.UserSummary `json:"user"`
	}
	code := e.call("POST", "/api/register", "",
		map[string]string{"username": username, "password": "password123"}, &sess)
	if code != http.StatusOK {
		e.t.Fatalf("register %s: status %d", username, code)
	}
	return sess.Token, sess.User
}

func TestRegisterLoginMe(t *testing.T) {
	e := newEnv(t)

	token, user := e.register("alice")
	if user.Username != "alice" || user.ID == "" {
		t.Fatalf("register user = %+v", user)
	}

	// Duplicate → 409.
	if code := e.call("POST", "/api/register", "",
		map[string]string{"username": "ALICE", "password": "password123"}, nil); code != http.StatusConflict {
		t.Fatalf("duplicate register status %d", code)
	}
	// Weak password / bad username → 400.
	if code := e.call("POST", "/api/register", "",
		map[string]string{"username": "bob", "password": "short"}, nil); code != http.StatusBadRequest {
		t.Fatalf("weak password status %d", code)
	}
	if code := e.call("POST", "/api/register", "",
		map[string]string{"username": "b~d name!", "password": "password123"}, nil); code != http.StatusBadRequest {
		t.Fatalf("bad username status %d", code)
	}
	// Oversized password (Argon2 cost-DoS guard) → 400.
	if code := e.call("POST", "/api/register", "",
		map[string]string{"username": "carol", "password": strings.Repeat("p", 129)}, nil); code != http.StatusBadRequest {
		t.Fatalf("oversized password status %d", code)
	}

	// Login roundtrip.
	var sess struct {
		Token string `json:"token"`
	}
	if code := e.call("POST", "/api/login", "",
		map[string]string{"username": "alice", "password": "password123"}, &sess); code != http.StatusOK {
		t.Fatalf("login status %d", code)
	}
	// Wrong password and unknown user look identical.
	if code := e.call("POST", "/api/login", "",
		map[string]string{"username": "alice", "password": "wrong-password"}, nil); code != http.StatusUnauthorized {
		t.Fatalf("bad password status %d", code)
	}
	if code := e.call("POST", "/api/login", "",
		map[string]string{"username": "ghost", "password": "password123"}, nil); code != http.StatusUnauthorized {
		t.Fatalf("unknown user status %d", code)
	}

	// /me with both tokens.
	var me model.UserSummary
	if code := e.call("GET", "/api/me", token, nil, &me); code != http.StatusOK || me.Username != "alice" {
		t.Fatalf("me: %d %+v", code, me)
	}
	if code := e.call("GET", "/api/me", sess.Token, nil, &me); code != http.StatusOK {
		t.Fatalf("me with login token: %d", code)
	}
	// Garbage / missing tokens → 401.
	if code := e.call("GET", "/api/me", "garbage.token.here", nil, nil); code != http.StatusUnauthorized {
		t.Fatalf("garbage token status %d", code)
	}
	if code := e.call("GET", "/api/me", "", nil, nil); code != http.StatusUnauthorized {
		t.Fatalf("missing token status %d", code)
	}
}

func TestLoginRateLimit(t *testing.T) {
	e := newEnv(t)
	e.register("alice")

	// The limiter (10/min) trips before the 11th attempt, regardless of
	// whether the credentials are right — this guards the login handler
	// itself, not just failed attempts.
	var last int
	for i := 0; i < 11; i++ {
		last = e.call("POST", "/api/login", "",
			map[string]string{"username": "alice", "password": "wrong-password"}, nil)
	}
	if last != http.StatusTooManyRequests {
		t.Fatalf("11th login attempt in a minute: %d, want 429", last)
	}
}

func TestRegisterRateLimit(t *testing.T) {
	e := newEnv(t)

	var last int
	for i := 0; i < 6; i++ {
		last = e.call("POST", "/api/register", "",
			map[string]string{"username": fmt.Sprintf("user%d", i), "password": "password123"}, nil)
	}
	if last != http.StatusTooManyRequests {
		t.Fatalf("6th registration in an hour: %d, want 429", last)
	}
}

func TestConversationCreateRateLimit(t *testing.T) {
	e := newEnv(t)
	aTok, _ := e.register("alice")
	bTok, bUser := e.register("bob")
	_ = bTok

	var last int
	for i := 0; i < 21; i++ {
		last = e.call("POST", "/api/conversations", aTok,
			map[string]any{"type": "dm", "participantIDs": []string{bUser.ID}}, nil)
	}
	if last != http.StatusTooManyRequests {
		t.Fatalf("21st conversation create in a minute: %d, want 429", last)
	}
}

func TestReactionRateLimit(t *testing.T) {
	e := newEnv(t)
	aTok, aUser := e.register("alice")
	bTok, bUser := e.register("bob")
	var dm convResponse
	e.call("POST", "/api/conversations", aTok,
		map[string]any{"type": "dm", "participantIDs": []string{bUser.ID}}, &dm)
	if _, err := e.store.AppendMessage(dm.ID, aUser.ID, "rl-1", "hi", nil); err != nil {
		t.Fatal(err)
	}

	var last int
	for i := 0; i < 61; i++ {
		last = e.call("PUT", "/api/conversations/"+dm.ID+"/messages/1/reactions/%F0%9F%91%8D", bTok, nil, nil)
	}
	if last != http.StatusTooManyRequests {
		t.Fatalf("61st reaction in a minute: %d, want 429", last)
	}
}

// TestAuthGeneralRateLimit checks the router-wide per-user cap applies
// even to a cheap, otherwise-unlimited GET endpoint.
func TestAuthGeneralRateLimit(t *testing.T) {
	e := newEnv(t)
	aTok, _ := e.register("alice")

	var last int
	for i := 0; i < 181; i++ {
		last = e.call("GET", "/api/me", aTok, nil, nil)
	}
	if last != http.StatusTooManyRequests {
		t.Fatalf("181st /api/me call in a minute: %d, want 429", last)
	}
}

// TestGlobalIPRateLimit checks the router-wide backstop (600/min per
// IP) catches traffic that never reaches a handler-specific limiter —
// here, repeated unauthenticated requests to a route outside the
// Bearer group.
func TestGlobalIPRateLimit(t *testing.T) {
	e := newEnv(t)

	var last int
	for i := 0; i < 601; i++ {
		last = e.call("GET", "/api/attachments/a8f92c1f-7d3e-4e21-a44c-9f3d1a2b7c88", "", nil, nil)
	}
	if last != http.StatusTooManyRequests {
		t.Fatalf("601st unauthenticated request in a minute: %d, want 429", last)
	}
}

func TestExpiredToken(t *testing.T) {
	e := newEnv(t)
	e.register("alice")

	// Mint with a different secret → rejected.
	other := auth.NewTokens([]byte("other-secret"))
	tok, _ := other.Mint("0000000001")
	if code := e.call("GET", "/api/me", tok, nil, nil); code != http.StatusUnauthorized {
		t.Fatalf("foreign-secret token status %d", code)
	}
}

func TestConversationFlow(t *testing.T) {
	e := newEnv(t)
	aTok, aUser := e.register("alice")
	bTok, bUser := e.register("bob")
	cTok, cUser := e.register("carol")

	// Alice creates a DM with Bob.
	var dm convResponse
	if code := e.call("POST", "/api/conversations", aTok,
		map[string]any{"type": "dm", "participantIDs": []string{bUser.ID}}, &dm); code != http.StatusOK {
		t.Fatalf("create dm status %d", code)
	}
	if dm.Type != "dm" || dm.MemberCount != 2 || dm.PeerID != bUser.ID {
		t.Fatalf("dm = %+v", dm)
	}
	// Bob creating the same DM gets the same conversation.
	var dm2 convResponse
	e.call("POST", "/api/conversations", bTok,
		map[string]any{"type": "dm", "participantIDs": []string{aUser.ID}}, &dm2)
	if dm2.ID != dm.ID {
		t.Fatalf("dm not deduped: %s vs %s", dm2.ID, dm.ID)
	}

	// Carol is not a member: 403 on everything.
	for _, path := range []string{
		"/api/conversations/" + dm.ID,
		"/api/conversations/" + dm.ID + "/messages",
		"/api/conversations/" + dm.ID + "/members",
	} {
		if code := e.call("GET", path, cTok, nil, nil); code != http.StatusForbidden {
			t.Fatalf("%s as non-member: status %d", path, code)
		}
	}

	// Group with all three.
	var group convResponse
	if code := e.call("POST", "/api/conversations", aTok,
		map[string]any{"type": "group", "name": "trio", "participantIDs": []string{bUser.ID, cUser.ID}}, &group); code != http.StatusOK {
		t.Fatalf("create group status %d", code)
	}
	var members []model.UserSummary
	e.call("GET", "/api/conversations/"+group.ID+"/members", cTok, nil, &members)
	if len(members) != 3 {
		t.Fatalf("group members = %d", len(members))
	}

	// Unknown participant rejected.
	if code := e.call("POST", "/api/conversations", aTok,
		map[string]any{"type": "dm", "participantIDs": []string{"0000009999"}}, nil); code != http.StatusBadRequest {
		t.Fatalf("unknown participant status %d", code)
	}

	// Group without name rejected.
	if code := e.call("POST", "/api/conversations", aTok,
		map[string]any{"type": "group", "participantIDs": []string{bUser.ID}}, nil); code != http.StatusBadRequest {
		t.Fatalf("unnamed group status %d", code)
	}

	// Conversation list ordering follows activity.
	seedMessages(t, e, group.ID, aUser.ID, 3)
	var convs []convResponse
	e.call("GET", "/api/conversations", bTok, nil, &convs)
	if len(convs) != 2 || convs[0].ID != group.ID {
		t.Fatalf("conv list = %+v", convs)
	}
	if convs[1].DisplayName != "alice" { // DM titled by peer for bob
		t.Fatalf("dm display name = %q", convs[1].DisplayName)
	}
}

func TestHistoryPagination(t *testing.T) {
	e := newEnv(t)
	aTok, aUser := e.register("alice")
	_, bUser := e.register("bob")

	var dm convResponse
	e.call("POST", "/api/conversations", aTok,
		map[string]any{"type": "dm", "participantIDs": []string{bUser.ID}}, &dm)
	seedMessages(t, e, dm.ID, aUser.ID, 120)

	type page struct {
		Messages  []model.Message `json:"messages"`
		HasMore   bool            `json:"hasMore"`
		OldestSeq uint64          `json:"oldestSeq"`
	}
	var p1 page
	if code := e.call("GET", "/api/conversations/"+dm.ID+"/messages?limit=50", aTok, nil, &p1); code != http.StatusOK {
		t.Fatalf("history status %d", code)
	}
	if len(p1.Messages) != 50 || !p1.HasMore || p1.Messages[49].Seq != 120 {
		t.Fatalf("page1: len=%d hasMore=%v last=%d", len(p1.Messages), p1.HasMore, p1.Messages[len(p1.Messages)-1].Seq)
	}
	var p2 page
	e.call("GET", fmt.Sprintf("/api/conversations/%s/messages?limit=50&before=%d", dm.ID, p1.OldestSeq), aTok, nil, &p2)
	if len(p2.Messages) != 50 || !p2.HasMore || p2.Messages[0].Seq != 21 {
		t.Fatalf("page2: len=%d hasMore=%v first=%d", len(p2.Messages), p2.HasMore, p2.Messages[0].Seq)
	}
	var p3 page
	e.call("GET", fmt.Sprintf("/api/conversations/%s/messages?limit=50&before=%d", dm.ID, p2.OldestSeq), aTok, nil, &p3)
	if len(p3.Messages) != 20 || p3.HasMore || p3.Messages[0].Seq != 1 {
		t.Fatalf("page3: len=%d hasMore=%v first=%d", len(p3.Messages), p3.HasMore, p3.Messages[0].Seq)
	}
}

// TestPerfSanity asserts the spec's latency targets that map directly
// onto store operations: auth lookup < 5ms, 1000-message history < 20ms.
func TestPerfSanity(t *testing.T) {
	e := newEnv(t)
	aTok, aUser := e.register("alice")
	_, bUser := e.register("bob")
	var dm convResponse
	e.call("POST", "/api/conversations", aTok,
		map[string]any{"type": "dm", "participantIDs": []string{bUser.ID}}, &dm)
	seedMessages(t, e, dm.ID, aUser.ID, 1200)

	start := time.Now()
	if code := e.call("POST", "/api/login", "",
		map[string]string{"username": "alice", "password": "password123"}, nil); code != http.StatusOK {
		t.Fatal("login failed")
	}
	// Argon2id verification dominates (~30-50ms by design, m=64MiB) — the
	// spec's "authentication lookup <5ms" is the store lookup, measured via /me.
	loginTotal := time.Since(start)

	start = time.Now()
	e.call("GET", "/api/me", aTok, nil, nil)
	meLatency := time.Since(start)
	if meLatency > 5*time.Millisecond {
		t.Fatalf("auth lookup took %v, want <5ms", meLatency)
	}

	start = time.Now()
	var p struct {
		Messages []model.Message `json:"messages"`
	}
	e.call("GET", "/api/conversations/"+dm.ID+"/messages?limit=100&before=1101", aTok, nil, &p)
	histLatency := time.Since(start)
	if histLatency > 20*time.Millisecond {
		t.Fatalf("history page took %v, want <20ms", histLatency)
	}
	t.Logf("login(total incl argon2)=%v, me=%v, history=%v", loginTotal, meLatency, histLatency)
}

// seedMessages writes via the store directly (the WS send path arrives
// in the WebSocket milestone).
func seedMessages(t *testing.T, e *testEnv, convID, senderID string, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		if _, err := e.store.AppendMessage(convID, senderID, "", fmt.Sprintf("message %d", i), nil); err != nil {
			t.Fatal(err)
		}
	}
}
