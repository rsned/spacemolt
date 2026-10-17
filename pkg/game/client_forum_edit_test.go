package game

import (
	"context"
	"testing"
	"time"

	"github.com/rsned/spacemolt/internal/protocol"
)

// forum_edit_thread(thread_id, content?, title?) takes title and content as
// OPTIONAL. Sending them as empty strings is not the same as omitting them:
// the server would save the blank, wiping a thread's title or body. So a
// caller editing only the body must not clear the title.
func TestForumEditThread_OmitsEmptyOptionalFields(t *testing.T) {
	c, sendCh := newSubmitTestClient(t)
	ctx := context.Background()

	done := make(chan error, 1)
	go func() { done <- c.ForumEditThread(ctx, "thread-1", "", "new body") }()

	sent := <-sendCh
	if sent.Type != "forum_edit_thread" {
		t.Fatalf("sent type = %q", sent.Type)
	}
	if got := sent.Payload["thread_id"]; got != "thread-1" {
		t.Errorf("thread_id = %v", got)
	}
	if got := sent.Payload["content"]; got != "new body" {
		t.Errorf("content = %v", got)
	}
	if _, ok := sent.Payload["title"]; ok {
		t.Error("empty title was sent; the server would save the blank and wipe the thread title")
	}

	c.router.dispatch(protocol.Response{
		Type: protocol.TypeOK, RequestID: sent.RequestID,
		Payload: map[string]any{"message": "Thread updated.", "thread_id": "thread-1"},
	})
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("ForumEditThread: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("ForumEditThread hung waiting for a terminal frame")
	}
}

// The mirror case: retitling without touching the body must not blank it.
func TestForumEditThread_TitleOnlyOmitsContent(t *testing.T) {
	c, sendCh := newSubmitTestClient(t)
	ctx := context.Background()

	done := make(chan error, 1)
	go func() { done <- c.ForumEditThread(ctx, "thread-2", "New Title", "") }()

	sent := <-sendCh
	if got := sent.Payload["title"]; got != "New Title" {
		t.Errorf("title = %v", got)
	}
	if _, ok := sent.Payload["content"]; ok {
		t.Error("empty content was sent; the server would wipe the thread body")
	}

	c.router.dispatch(protocol.Response{
		Type: protocol.TypeOK, RequestID: sent.RequestID,
		Payload: map[string]any{"message": "Thread updated.", "thread_id": "thread-2"},
	})
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("ForumEditThread: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("hung")
	}
}

// forum_edit_reply(content, reply_id) declares BOTH required, so content is
// sent as given -- there is no partial edit of a reply.
func TestForumEditReply_SendsBothFields(t *testing.T) {
	c, sendCh := newSubmitTestClient(t)
	ctx := context.Background()

	done := make(chan error, 1)
	go func() { done <- c.ForumEditReply(ctx, "reply-9", "corrected text") }()

	sent := <-sendCh
	if sent.Type != "forum_edit_reply" {
		t.Fatalf("sent type = %q", sent.Type)
	}
	if got := sent.Payload["reply_id"]; got != "reply-9" {
		t.Errorf("reply_id = %v", got)
	}
	if got := sent.Payload["content"]; got != "corrected text" {
		t.Errorf("content = %v", got)
	}

	c.router.dispatch(protocol.Response{
		Type: protocol.TypeOK, RequestID: sent.RequestID,
		Payload: map[string]any{"message": "Reply updated.", "reply_id": "reply-9"},
	})
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("ForumEditReply: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("hung")
	}
}
