package session

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "nested", "hewn.db")
	store, err := Open(context.Background(), path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func TestOpen_CreatesParentDirectory(t *testing.T) {
	openTestStore(t) // panics/fails via t.Fatalf inside if the nested dir isn't created
}

func TestCreateSession(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	sess, err := store.CreateSession(ctx, "/repo", "anthropic", "claude-opus-4-8", "read main.go and summarize it")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	if len(sess.ID) != 26 {
		t.Errorf("session ID = %q, want a 26-char ULID", sess.ID)
	}
	if sess.CWD != "/repo" || sess.Provider != "anthropic" || sess.Model != "claude-opus-4-8" {
		t.Errorf("session = %+v, unexpected field values", sess)
	}
	if sess.Title != "read main.go and summarize it" {
		t.Errorf("Title = %q, want the untruncated prompt", sess.Title)
	}
	if sess.CreatedAt.IsZero() || sess.UpdatedAt.IsZero() {
		t.Error("CreatedAt/UpdatedAt were not set")
	}
}

func TestCreateSession_TruncatesLongTitle(t *testing.T) {
	store := openTestStore(t)
	long := strings.Repeat("x", 200)

	sess, err := store.CreateSession(context.Background(), "/repo", "anthropic", "m", long)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if len(sess.Title) != maxTitleRunes {
		t.Errorf("len(Title) = %d, want %d", len(sess.Title), maxTitleRunes)
	}
}

func TestAppendMessage_AssignsIncreasingSeq(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	sess, err := store.CreateSession(ctx, "/repo", "anthropic", "m", "hi")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	m1, err := store.AppendMessage(ctx, sess.ID, RoleUser, json.RawMessage(`[{"Kind":0,"Text":"hi"}]`), nil)
	if err != nil {
		t.Fatalf("AppendMessage #1: %v", err)
	}
	usage := &Usage{InputTokens: 10, OutputTokens: 5}
	m2, err := store.AppendMessage(ctx, sess.ID, RoleAssistant, json.RawMessage(`[{"Kind":0,"Text":"hello"}]`), usage)
	if err != nil {
		t.Fatalf("AppendMessage #2: %v", err)
	}

	if m1.Seq != 1 || m2.Seq != 2 {
		t.Errorf("seqs = %d, %d, want 1, 2", m1.Seq, m2.Seq)
	}

	loaded, err := store.LoadMessages(ctx, sess.ID)
	if err != nil {
		t.Fatalf("LoadMessages: %v", err)
	}
	if len(loaded) != 2 {
		t.Fatalf("LoadMessages returned %d messages, want 2", len(loaded))
	}
	if loaded[0].Seq != 1 || loaded[1].Seq != 2 {
		t.Errorf("LoadMessages order = %+v, want seq 1 then 2", loaded)
	}
	if loaded[1].Usage == nil || loaded[1].Usage.InputTokens != 10 || loaded[1].Usage.OutputTokens != 5 {
		t.Errorf("loaded[1].Usage = %+v, want {10 5}", loaded[1].Usage)
	}
	if loaded[0].Usage != nil {
		t.Errorf("loaded[0].Usage = %+v, want nil (user message)", loaded[0].Usage)
	}
}

func TestAppendMessage_SeqIsPerSession(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	sessA, _ := store.CreateSession(ctx, "/repo", "anthropic", "m", "a")
	sessB, _ := store.CreateSession(ctx, "/repo", "anthropic", "m", "b")

	if _, err := store.AppendMessage(ctx, sessA.ID, RoleUser, json.RawMessage(`[]`), nil); err != nil {
		t.Fatalf("AppendMessage sessA: %v", err)
	}
	mB, err := store.AppendMessage(ctx, sessB.ID, RoleUser, json.RawMessage(`[]`), nil)
	if err != nil {
		t.Fatalf("AppendMessage sessB: %v", err)
	}
	if mB.Seq != 1 {
		t.Errorf("sessB's first message seq = %d, want 1 (independent of sessA)", mB.Seq)
	}
}

func TestAppendToolCall(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	sess, _ := store.CreateSession(ctx, "/repo", "anthropic", "m", "run tests")
	msg, err := store.AppendMessage(ctx, sess.ID, RoleAssistant, json.RawMessage(`[]`), nil)
	if err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}

	allowOnce := 1
	tc, err := store.AppendToolCall(ctx, msg.ID, "bash", json.RawMessage(`{"command":"go test ./..."}`),
		"ok", false, &allowOnce, 250*time.Millisecond)
	if err != nil {
		t.Fatalf("AppendToolCall: %v", err)
	}

	if tc.Approved == nil || *tc.Approved != 1 {
		t.Errorf("Approved = %v, want pointer to 1", tc.Approved)
	}
	if tc.DurationMS != 250 {
		t.Errorf("DurationMS = %d, want 250", tc.DurationMS)
	}
	if tc.IsError {
		t.Error("IsError = true, want false")
	}
}

func TestAppendToolCall_ReadOnlyHasNilApproved(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	sess, _ := store.CreateSession(ctx, "/repo", "anthropic", "m", "read a file")
	msg, _ := store.AppendMessage(ctx, sess.ID, RoleAssistant, json.RawMessage(`[]`), nil)

	tc, err := store.AppendToolCall(ctx, msg.ID, "read", json.RawMessage(`{"path":"x"}`), "contents", false, nil, time.Millisecond)
	if err != nil {
		t.Fatalf("AppendToolCall: %v", err)
	}
	if tc.Approved != nil {
		t.Errorf("Approved = %v, want nil for an ungated read-only call", tc.Approved)
	}
}

func TestLoadToolCalls(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	sess, _ := store.CreateSession(ctx, "/repo", "anthropic", "m", "run tests")
	msg, err := store.AppendMessage(ctx, sess.ID, RoleAssistant, json.RawMessage(`[]`), nil)
	if err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}

	allowSession := 2
	if _, tcErr := store.AppendToolCall(ctx, msg.ID, "bash", json.RawMessage(`{"command":"go build ./..."}`),
		"ok", false, &allowSession, 10*time.Millisecond); tcErr != nil {
		t.Fatalf("AppendToolCall #1: %v", tcErr)
	}
	if _, tcErr := store.AppendToolCall(ctx, msg.ID, "bash", json.RawMessage(`{"command":"go vet ./..."}`),
		"boom", true, nil, 5*time.Millisecond); tcErr != nil {
		t.Fatalf("AppendToolCall #2: %v", tcErr)
	}

	calls, err := store.LoadToolCalls(ctx, msg.ID)
	if err != nil {
		t.Fatalf("LoadToolCalls: %v", err)
	}
	if len(calls) != 2 {
		t.Fatalf("LoadToolCalls returned %d, want 2", len(calls))
	}
	if calls[0].Approved == nil || *calls[0].Approved != 2 {
		t.Errorf("calls[0].Approved = %v, want pointer to 2", calls[0].Approved)
	}
	if calls[1].Approved != nil {
		t.Errorf("calls[1].Approved = %v, want nil", calls[1].Approved)
	}
	if !calls[1].IsError || calls[1].Result != "boom" {
		t.Errorf("calls[1] = %+v, want IsError=true, Result=%q", calls[1], "boom")
	}
}

func TestLoadToolCalls_Empty(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	sess, _ := store.CreateSession(ctx, "/repo", "anthropic", "m", "s")
	msg, _ := store.AppendMessage(ctx, sess.ID, RoleAssistant, json.RawMessage(`[]`), nil)

	calls, err := store.LoadToolCalls(ctx, msg.ID)
	if err != nil {
		t.Fatalf("LoadToolCalls: %v", err)
	}
	if len(calls) != 0 {
		t.Errorf("LoadToolCalls(no calls) = %v, want empty", calls)
	}
}

func TestLoadSession_ExactAndPrefixAndLatest(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	first, _ := store.CreateSession(ctx, "/repo", "anthropic", "m", "first")
	time.Sleep(2 * time.Millisecond) // ensure a distinct updated_at
	second, err := store.CreateSession(ctx, "/repo", "anthropic", "m", "second")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	byExact, err := store.LoadSession(ctx, first.ID)
	if err != nil {
		t.Fatalf("LoadSession(exact): %v", err)
	}
	if byExact.ID != first.ID {
		t.Errorf("LoadSession(exact) = %s, want %s", byExact.ID, first.ID)
	}

	byPrefix, err := store.LoadSession(ctx, second.ID[:8])
	if err != nil {
		t.Fatalf("LoadSession(prefix): %v", err)
	}
	if byPrefix.ID != second.ID {
		t.Errorf("LoadSession(prefix) = %s, want %s", byPrefix.ID, second.ID)
	}

	latest, err := store.LoadSession(ctx, "")
	if err != nil {
		t.Fatalf("LoadSession(latest): %v", err)
	}
	if latest.ID != second.ID {
		t.Errorf("LoadSession(\"\") = %s, want most recently updated %s", latest.ID, second.ID)
	}
}

func TestLoadSession_NotFound(t *testing.T) {
	store := openTestStore(t)
	_, err := store.LoadSession(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("LoadSession(nonexistent): expected error, got nil")
	}
}

func TestTouch_UpdatesUpdatedAtAndOrdering(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	older, _ := store.CreateSession(ctx, "/repo", "anthropic", "m", "older")
	time.Sleep(2 * time.Millisecond)
	newer, _ := store.CreateSession(ctx, "/repo", "anthropic", "m", "newer")

	// Touch the older session so it becomes the most recently updated.
	time.Sleep(2 * time.Millisecond)
	if err := store.Touch(ctx, older.ID); err != nil {
		t.Fatalf("Touch: %v", err)
	}

	latest, err := store.LoadSession(ctx, "")
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	if latest.ID != older.ID {
		t.Errorf("LoadSession(\"\") after touching %s = %s, want %s", older.ID, latest.ID, older.ID)
	}
	_ = newer
}

func TestListSessions_OrderedMostRecentFirst(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	var ids []string
	for i := 0; i < 3; i++ {
		sess, err := store.CreateSession(ctx, "/repo", "anthropic", "m", "s")
		if err != nil {
			t.Fatalf("CreateSession #%d: %v", i, err)
		}
		ids = append(ids, sess.ID)
		time.Sleep(2 * time.Millisecond)
	}

	sessions, err := store.ListSessions(ctx, 10)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions) != 3 {
		t.Fatalf("ListSessions returned %d, want 3", len(sessions))
	}
	for i, sess := range sessions {
		want := ids[len(ids)-1-i]
		if sess.ID != want {
			t.Errorf("ListSessions[%d] = %s, want %s (most recent first)", i, sess.ID, want)
		}
	}
}

func TestListSessions_RespectsLimit(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		if _, err := store.CreateSession(ctx, "/repo", "anthropic", "m", "s"); err != nil {
			t.Fatalf("CreateSession #%d: %v", i, err)
		}
	}

	sessions, err := store.ListSessions(ctx, 2)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions) != 2 {
		t.Errorf("ListSessions(limit=2) returned %d, want 2", len(sessions))
	}
}

func TestLoadMessages_EmptySession(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	sess, _ := store.CreateSession(ctx, "/repo", "anthropic", "m", "s")
	messages, err := store.LoadMessages(ctx, sess.ID)
	if err != nil {
		t.Fatalf("LoadMessages: %v", err)
	}
	if len(messages) != 0 {
		t.Errorf("LoadMessages(empty session) = %v, want empty", messages)
	}
}

func TestOpen_ReopenReusesData(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hewn.db")
	ctx := context.Background()

	store1, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("Open (first): %v", err)
	}
	sess, err := store1.CreateSession(ctx, "/repo", "anthropic", "m", "s")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if closeErr := store1.Close(); closeErr != nil {
		t.Fatalf("Close: %v", closeErr)
	}

	store2, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("Open (second): %v", err)
	}
	defer func() { _ = store2.Close() }()

	loaded, err := store2.LoadSession(ctx, sess.ID)
	if err != nil {
		t.Fatalf("LoadSession after reopen: %v", err)
	}
	if loaded.ID != sess.ID {
		t.Errorf("LoadSession after reopen = %s, want %s", loaded.ID, sess.ID)
	}
}
