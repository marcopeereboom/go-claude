package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
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
	ts.SetResponseBody(`{"content":[{"text":"hello"}]}`)

	// call doRequest directly with mock server URL
	body, err := doRequest("fakekey", http.MethodPost, ts.URL, []byte(`{"messages":[]}`), 5)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(body, []byte("hello")) {
		t.Errorf("unexpected body: %s", string(body))
	}
}

// ------------------ 4. Test resume logic ------------------
func TestLoadAndAppendConversation(t *testing.T) {
	tmp := t.TempDir()
	file := tmp + "/conv.json"

	entry := ConversationLogEntry{
		Request:   []byte(`{"messages":[{"role":"user","content":"hi"}]}`),
		Response:  []byte(`{"content":[{"text":"hello"}]}`),
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

// ------------------ 5. Helper functions ------------------
func contains(s, substr string) bool {
	return bytes.Contains([]byte(s), []byte(substr))
}

// ------------------ 6. Mock HTTP Server ------------------
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

// ------------------ 7. Test stdin hang prevention ------------------
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
