package ws

import (
	"testing"
	"time"

	"jmessage/internal/model"
)

func wsAttachment(t *testing.T, e *wsEnv, id, owner string) {
	t.Helper()
	err := e.st.CreateAttachment(&model.Attachment{
		ID: id, OwnerID: owner, Filename: "pic.png", MimeType: "image/png",
		Size: 99, SHA256: "cafe", State: model.AttachmentPending,
		CreatedAt: time.Now().UnixMilli(),
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestSendWithAttachment(t *testing.T) {
	e := newWSEnv(t)
	alice, bob := e.user("alice"), e.user("bob")
	conv, _ := e.st.CreateConversation(model.ConvDM, "", alice.ID, []string{bob.ID})
	wsAttachment(t, e, "att-ws-1", alice.ID)

	ca, cb := e.dial(alice.ID), e.dial(bob.ID)

	// Empty body is fine when attachments are present.
	ca.send(Frame{Type: TypeSend, TempID: "t-att", ConvID: conv.ID, AttachmentIDs: []string{"att-ws-1"}})

	ack := ca.recv(TypeAck)
	if ack.Seq != 1 || len(ack.Attachments) != 1 || ack.Attachments[0].Filename != "pic.png" {
		t.Fatalf("ack = %+v", ack)
	}
	msg := cb.recv(TypeMessage)
	if len(msg.Attachments) != 1 || msg.Attachments[0].ID != "att-ws-1" || msg.Attachments[0].MimeType != "image/png" {
		t.Fatalf("delivered = %+v", msg)
	}

	a, _ := e.st.GetAttachment("att-ws-1")
	if a.State != model.AttachmentCommitted || a.ConvID != conv.ID {
		t.Fatalf("attachment after send: %+v", a)
	}

	// Duplicate send: same seq, refs intact, no double effects.
	ca.send(Frame{Type: TypeSend, TempID: "t-att", ConvID: conv.ID, AttachmentIDs: []string{"att-ws-1"}})
	ack2 := ca.recv(TypeAck)
	if ack2.Seq != 1 || len(ack2.Attachments) != 1 {
		t.Fatalf("dup ack = %+v", ack2)
	}
	if last, _ := e.st.LastSeq(conv.ID); last != 1 {
		t.Fatalf("dup send appended: last=%d", last)
	}
}

func TestForeignAttachmentRejected(t *testing.T) {
	e := newWSEnv(t)
	alice, bob := e.user("alice"), e.user("bob")
	conv, _ := e.st.CreateConversation(model.ConvDM, "", alice.ID, []string{bob.ID})
	wsAttachment(t, e, "att-bobs", bob.ID)

	ca := e.dial(alice.ID)
	ca.send(Frame{Type: TypeSend, TempID: "t-steal", ConvID: conv.ID, Body: "mine now", AttachmentIDs: []string{"att-bobs"}})
	errF := ca.recv(TypeError)
	if errF.Code != "not_owner" || errF.TempID != "t-steal" {
		t.Fatalf("error = %+v", errF)
	}
	// Nothing persisted; attachment untouched.
	if last, _ := e.st.LastSeq(conv.ID); last != 0 {
		t.Fatalf("message persisted: %d", last)
	}
	a, _ := e.st.GetAttachment("att-bobs")
	if a.State != model.AttachmentPending {
		t.Fatalf("attachment state = %s", a.State)
	}

	// Unknown attachment → bad_attachment.
	ca.send(Frame{Type: TypeSend, TempID: "t-ghost", ConvID: conv.ID, Body: "x", AttachmentIDs: []string{"att-ghost"}})
	errF = ca.recv(TypeError)
	if errF.Code != "bad_attachment" {
		t.Fatalf("error = %+v", errF)
	}
}
