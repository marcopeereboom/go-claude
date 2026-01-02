package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// ------------------ 1. Test stdin detection ------------------
func TestStdinIsPiped(t *testing.T) {
	if stdinIsPiped() {
		t.Log("stdin is piped (expected in CI if redirected)")
	} else {
		t.Log("stdin is TTY")
	}
}

// ------------------ 2. Test doRequest handles HTTP codes ------------------
func TestDoRequestErrorMapping(t *testing.T) {
	ts := newTestServer()
	defer ts.Close()

	cases := []struct {
		code int
		want string
	}{
		{http.StatusOK, ""},
		{http.StatusUnauthorized, "unauthorized"},
		{http.StatusNotFound, "resource not found"},
		{http.StatusUnprocessableEntity, "request could not be processed"},
		{http.StatusTooManyRequests, "rate limit exceeded"},
		{http.StatusInternalServerError, "server error"},
	}

	for _, c := range cases {
		ts.SetResponseCode(c.code)
		ts.SetResponseBody(`{"error":"test"}`)

		_, err := doRequest("fakekey", http.MethodGet, ts.URL, nil, 5)
		if c.want == "" {
			if err != nil {
				t.Errorf("code %d: got error %v, want nil", c.code, err)
			}
		} else if err == nil || !bytes.Contains([]byte(err.Error()), []byte(c.want)) {
			t.Errorf("code %d: got error %v, want substring %q", c.code, err, c.want)
		}
	}
}

// ------------------ 3. Test callClaude success using mock server ------------------
func TestCallClaudeSuccess(t *testing.T) {
	ts := newTestServer()
	defer ts.Close()
	ts.SetResponseCode(http.StatusOK)
	ts.SetResponseBody(`{"content":[{"type":"text","text":"hello"}]}`)

	body, err := doRequest("fakekey", http.MethodPost, ts.URL, []byte(`{"messages":[]}`), 5)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(body, []byte("hello")) {
		t.Errorf("unexpected body: %s", string(body))
	}
}

// ------------------ 4. Simple resume test ------------------
func TestLoadAndAppendConversation(t *testing.T) {
	tmp := t.TempDir()
	file := tmp + "/conv.json"

	entry := ConversationLogEntry{
		Request:   []byte(`{"messages":[{"role":"user","content":"hi"}]}`),
		Response:  []byte(`{"content":[{"type":"text","text":"hello"}]}`),
		Timestamp: "2026-01-02T00:00:00Z",
	}
	data, _ := json.Marshal(entry)
	if err := os.WriteFile(file, append(data, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}

	msgs, err := loadConversationContext(file)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 2 || msgs[0].Content != "hi" || msgs[1].Content != "hello" {
		t.Errorf("unexpected messages: %+v", msgs)
	}
}

// ------------------ 5. Corrected resume order test ------------------
func TestResumeConversationOrder(t *testing.T) {
	tmp := t.TempDir()
	file := tmp + "/conv.json"

	entries := []ConversationLogEntry{
		{
			Request:  []byte(`{"messages":[{"role":"user","content":"Hello"}]}`),
			Response: []byte(`{"content":[{"type":"text","text":"Hi there"}]}`),
		},
		{
			Request:  []byte(`{"messages":[{"role":"user","content":"How are you?"}]}`),
			Response: []byte(`{"content":[{"type":"text","text":"I'm fine, thanks"}]}`),
		},
		{
			Request:  []byte(`{"messages":[{"role":"user","content":"Tell me a joke"}]}`),
			Response: []byte(`{"content":[{"type":"text","text":"Why did the chicken cross the road?"}]}`),
		},
	}

	// Append each entry instead of overwriting
	for _, e := range entries {
		data, _ := json.Marshal(e)
		f, err := os.OpenFile(file, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write(append(data, '\n')); err != nil {
			t.Fatal(err)
		}
		f.Close()
	}

	messages, err := loadConversationContext(file)
	if err != nil {
		t.Fatal(err)
	}

	// Matches loader behavior: 1 user + 1 assistant per entry
	expected := []Message{
		{"user", "Hello"},
		{"assistant", "Hi there"},
		{"user", "How are you?"},
		{"assistant", "I'm fine, thanks"},
		{"user", "Tell me a joke"},
		{"assistant", "Why did the chicken cross the road?"},
	}

	if len(messages) != len(expected) {
		t.Fatalf("expected %d messages, got %d", len(expected), len(messages))
	}

	for i, msg := range messages {
		if msg.Role != expected[i].Role || msg.Content != expected[i].Content {
			t.Errorf("msg %d mismatch: got %+v, want %+v", i, msg, expected[i])
		}
	}
}

// ------------------ 6. Negative test: empty file ------------------
func TestResumeEmptyFile(t *testing.T) {
	tmp := t.TempDir()
	file := tmp + "/conv.json"

	if err := os.WriteFile(file, []byte{}, 0o644); err != nil {
		t.Fatal(err)
	}

	msgs, err := loadConversationContext(file)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 0 {
		t.Errorf("expected 0 messages, got %d", len(msgs))
	}
}

// ------------------ 7. Negative test: malformed JSON ------------------
func TestResumeMalformedEntry(t *testing.T) {
	tmp := t.TempDir()
	file := tmp + "/conv.json"

	valid := ConversationLogEntry{
		Request:  []byte(`{"messages":[{"role":"user","content":"Hi"}]}`),
		Response: []byte(`{"content":[{"type":"text","text":"Hello"}]}`),
	}
	data, _ := json.Marshal(valid)

	content := append([]byte(`{"bad json`+"\n"), append(data, '\n')...)
	if err := os.WriteFile(file, content, 0o644); err != nil {
		t.Fatal(err)
	}

	msgs, err := loadConversationContext(file)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 2 {
		t.Errorf("expected 2 messages from valid entry, got %d", len(msgs))
	}
	if msgs[0].Role != "user" || msgs[0].Content != "Hi" {
		t.Errorf("unexpected first message: %+v", msgs[0])
	}
	if msgs[1].Role != "assistant" || msgs[1].Content != "Hello" {
		t.Errorf("unexpected second message: %+v", msgs[1])
	}
}

// ------------------ 8. Negative test: missing fields ------------------
func TestResumeMissingFields(t *testing.T) {
	tmp := t.TempDir()
	file := tmp + "/conv.json"

	entry := ConversationLogEntry{
		Request:  []byte(`{}`), // missing messages
		Response: []byte(`{"content":[{"type":"text","text":"Hello"}]}`),
	}
	data, _ := json.Marshal(entry)
	if err := os.WriteFile(file, append(data, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}

	msgs, err := loadConversationContext(file)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 {
		t.Errorf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0].Role != "assistant" || msgs[0].Content != "Hello" {
		t.Errorf("unexpected message: %+v", msgs[0])
	}
}

// ------------------ 9. Negative test: mangled log ------------------
func TestResumeMangledLog(t *testing.T) {
	tmp := t.TempDir()
	file := tmp + "/conv.json"

	mangled := []string{
		`{"bad json line`,
		`{"request":{},"response":{"content":[{"type":"text","text":"A"}]}}`,
		`{"request":{"messages":[{"role":"user","content":"Hi"}]},"response":{"content":[{"type":"text","text":"Hello"}]}}`,
	}

	if err := os.WriteFile(file, []byte(strings.Join(mangled, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	msgs, err := loadConversationContext(file)
	if err != nil {
		t.Fatal(err)
	}

	expected := []Message{
		{"assistant", "A"},
		{"user", "Hi"},
		{"assistant", "Hello"},
	}

	if len(msgs) != len(expected) {
		t.Fatalf("expected %d messages, got %d", len(expected), len(msgs))
	}

	for i, msg := range msgs {
		if msg.Role != expected[i].Role || msg.Content != expected[i].Content {
			t.Errorf("msg %d mismatch: got %+v, want %+v", i, msg, expected[i])
		}
	}
}

// ------------------ 10. Helper functions ------------------
func contains(s, substr string) bool {
	return bytes.Contains([]byte(s), []byte(substr))
}

// ------------------ 11. Mock HTTP Server ------------------
type testServer struct {
	*httptest.Server
	code int
	body string
}

func newTestServer() *testServer {
	ts := &testServer{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(ts.code)
		fmt.Fprint(w, ts.body)
	}))
	ts.Server = server
	ts.code = http.StatusOK
	ts.body = `{}`
	return ts
}

func (ts *testServer) SetResponseCode(code int) {
	ts.code = code
}

func (ts *testServer) SetResponseBody(body string) {
	ts.body = body
}

// ------------------ 12. Test stdin hang prevention ------------------
func TestRunCLINoInput(t *testing.T) {
	oldStdin := os.Stdin
	defer func() { os.Stdin = oldStdin }()

	r, w, _ := os.Pipe()
	os.Stdin = r
	w.Close() // close immediately so ReadAll returns EOF

	err := runCLI(false, false, 1000, defaultModel, "", "", 5)
	if err == nil || err.Error() != "no input provided" {
		t.Errorf("expected 'no input provided' error, got %v", err)
	}
}
