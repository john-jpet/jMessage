package ws

import (
	"testing"

	"jmessage/internal/model"
)

func TestReactionOverWS(t *testing.T) {
	e := newWSEnv(t)
	alice, bob := e.user("alice"), e.user("bob")
	conv, _ := e.st.CreateConversation(model.ConvDM, "", alice.ID, []string{bob.ID})
	if _, err := e.st.AppendMessage(conv.ID, alice.ID, "", "hello", nil); err != nil {
		t.Fatal(err)
	}

	ca, cb := e.dial(alice.ID), e.dial(bob.ID)

	// Bob reacts: both sides (including bob) get the update.
	cb.send(Frame{Type: TypeReactionAdd, ConvID: conv.ID, Seq: 1, Emoji: "👍"})
	upA := ca.recv(TypeReactionUpdate)
	upB := cb.recv(TypeReactionUpdate)
	if upA.Emoji != "👍" || upA.Count == nil || *upA.Count != 1 || !upA.Added || upA.UserID != bob.ID {
		t.Fatalf("alice update = %+v", upA)
	}
	if upB.Count == nil || *upB.Count != 1 {
		t.Fatalf("bob update = %+v", upB)
	}

	// Duplicate add: count stays 1 (idempotent key overwrite).
	cb.send(Frame{Type: TypeReactionAdd, ConvID: conv.ID, Seq: 1, Emoji: "👍"})
	if up := ca.recv(TypeReactionUpdate); *up.Count != 1 {
		t.Fatalf("dup add count = %d", *up.Count)
	}
	cb.recv(TypeReactionUpdate) // bob's own copy

	// Remove: count 0 must be present in the frame.
	cb.send(Frame{Type: TypeReactionRemove, ConvID: conv.ID, Seq: 1, Emoji: "👍"})
	if up := ca.recv(TypeReactionUpdate); up.Count == nil || *up.Count != 0 || up.Added {
		t.Fatalf("remove update = %+v", up)
	}
	cb.recv(TypeReactionUpdate) // bob's own copy

	// Unsupported emoji rejected.
	cb.send(Frame{Type: TypeReactionAdd, ConvID: conv.ID, Seq: 1, Emoji: "🎉"})
	if errF := cb.recv(TypeError); errF.Code != "bad_reaction" {
		t.Fatalf("error = %+v", errF)
	}

	// Non-member rejected, nothing broadcast.
	mallory := e.user("mallory")
	cm := e.dial(mallory.ID)
	cm.send(Frame{Type: TypeReactionAdd, ConvID: conv.ID, Seq: 1, Emoji: "👍"})
	if errF := cm.recv(TypeError); errF.Code != "not_member" {
		t.Fatalf("mallory error = %+v", errF)
	}
	ca.expectNone(TypeReactionUpdate)
}
