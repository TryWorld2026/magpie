package gateway

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/provider"
)

// turnedAway is Antigravity's answer to Claude Code's and the Agent SDK's
// system prompt: a 429 that reads as a used-up plan whatever the quota
// left, on every model (#666: Claude Desktop's chats always 429 while its
// titles and Codex on the same account answer). magpie says why, and now
// also leaves the account alone, since nothing is wrong with it.
const turnedAway = `{"error":{"code":429,"message":"Resource has been exhausted (e.g. check quota).","status":"RESOURCE_EXHAUSTED"}}`

// claudeCodeSystem is Claude Code's, as 2.1.288 sends it: its billing line
// or the SDK's identity line anywhere in the system instruction.
const claudeCodeSystem = "x-anthropic-billing-header: cc_version=2.1.288.e3f; cc_entrypoint=sdk-cli;\nYou are Claude Code, Anthropic's official CLI for Claude."

// antigravityWithAMate is a group whose first member is the Antigravity
// account signed in at user, and whose second answers anything, so the
// first has someone after it to fail over to. say is what the Antigravity
// account's upstream answers. The second value counts how often the
// account's usage was asked of the vendor again.
func antigravityWithAMate(t *testing.T, say func(w http.ResponseWriter, r *http.Request)) (*Server, *int) {
	t.Helper()
	fresh(t)
	logins := []map[string]any{{
		"agent": "antigravity", "user": "u@example.com", "on": true,
		"auth": map[string]any{"access_token": "tok", "refresh_token": "ref", "project": "p1",
			"expiry_date": time.Now().Add(time.Hour).UnixMilli()},
	}}
	dir := filepath.Dir(provider.Path())
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "logins.json"), mustJSON(logins), 0o600); err != nil {
		t.Fatal(err)
	}
	old := http.DefaultClient
	http.DefaultClient = &http.Client{Transport: countTransport(func(r *http.Request) (*http.Response, error) {
		var body string
		switch {
		case strings.HasSuffix(r.URL.Path, "latest-arm64-mac.yml"):
			body = "version: 2.9.1\n"
		case strings.Contains(r.URL.Path, ":loadCodeAssist"):
			body = `{"currentTier":{"id":"standard-tier"},"cloudaicompanionProject":"p1"}`
		case strings.Contains(r.URL.Path, ":fetchAvailableModels"):
			body = `{"models":{"m":{}}}`
		default:
			return nil, fmt.Errorf("unexpected request to %s", r.URL)
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	t.Cleanup(func() { http.DefaultClient = old })
	p, err := provider.Find("antigravity")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Fetch(t.Context()); err != nil {
		t.Fatal(err)
	}
	mate := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, sse(
			`event: message_start`+"\n"+`data: {"type":"message_start","message":{"id":"m","type":"message","role":"assistant","model":"m1","content":[],"usage":{"input_tokens":1,"output_tokens":0}}}`,
			`event: content_block_start`+"\n"+`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
			`event: content_block_delta`+"\n"+`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"from the mate"}}`,
			`event: content_block_stop`+"\n"+`data: {"type":"content_block_stop","index":0}`,
			`event: message_delta`+"\n"+`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":3}}`,
			`event: message_stop`+"\n"+`data: {"type":"message_stop"}`))
	}))
	t.Cleanup(mate.Close)
	if err := provider.Save(provider.Provider{ID: "mate", Name: "Mate", Key: "k", Models: []string{"m"}, Anthropic: mate.URL}); err != nil {
		t.Fatal(err)
	}
	if err := provider.SaveGroup(provider.Group{Name: "G", Members: []string{"antigravity/m", "mate/m"}, Routing: provider.Ordered}); err != nil {
		t.Fatal(err)
	}
	askedUsage := new(int)
	oldAsk := staleAllowance
	staleAllowance = func(agent, user string) { *askedUsage++ }
	t.Cleanup(func() { staleAllowance = oldAsk })
	s := New()
	// the Antigravity account's own upstream, which only the gateway asks;
	// any other member is answered by its own test server
	s.client = &http.Client{Transport: countTransport(func(r *http.Request) (*http.Response, error) {
		if !strings.Contains(r.URL.Host, "googleapis.com") {
			return http.DefaultTransport.RoundTrip(r)
		}
		rec := httptest.NewRecorder()
		say(rec, r)
		return rec.Result(), nil
	})}
	return s, askedUsage
}

// claudeTurn is a turn as Claude Code sends it on the group, with system
// as the system instruction the client asked for.
func claudeTurn(system string) string {
	return `{"model":"group/g","max_tokens":100,"system":` + quote(system) + `,"messages":[{"role":"user","content":"hi"}]}`
}

// turnOn posts a turn to this gateway and says what came back.
func turnOn(t *testing.T, s *Server, body string) (int, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest("POST", "/v1/messages", strings.NewReader(body)))
	return rec.Code, rec.Body.String()
}

// the account Antigravity turned away is healthy: it rests no quarter of
// an hour for a request it refused, its usage isn't asked again, and the
// next turn asks it first again — as the refusal it is, not a spent quota.
func TestAntigravityTurnedAwayRestsNobody(t *testing.T) {
	s, askedUsage := antigravityWithAMate(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		io.WriteString(w, turnedAway)
	})
	code, out := turnOn(t, s, claudeTurn(claudeCodeSystem))
	if code != 200 || !strings.Contains(out, "from the mate") {
		t.Fatalf("the turn: %d %s", code, out)
	}
	r := lastRoute(s)
	if len(r.Tries) != 2 || r.Tries[0].Status != 429 || r.Tries[0].Fail != failRefused || r.Tries[0].Rest != nil {
		t.Fatalf("the turned-away try: %+v", r.Tries)
	}
	if !strings.Contains(r.Tries[0].Error, antigravityTurnedAwayHint) {
		t.Fatalf("the attempt isn't told why: %s", r.Tries[0].Error)
	}
	if *askedUsage != 0 {
		t.Fatalf("the account's usage was asked again %d times for a request it refused", *askedUsage)
	}
	if _, ok := restOf("antigravity@u@example.com"); ok {
		t.Fatalf("antigravity@u@example.com rests for a request it answered with a refusal: %v", rests())
	}
	// the same account is asked first again on the next turn
	code, out = turnOn(t, s, claudeTurn(claudeCodeSystem))
	if code != 200 || !strings.Contains(out, "from the mate") {
		t.Fatalf("the next turn: %d %s", code, out)
	}
	if r := lastRoute(s); len(r.Tries) != 2 || r.Tries[0].ID != "antigravity" || r.Tries[0].Status != 429 {
		t.Fatalf("the next turn asked %+v first", r.Tries)
	}
}

// A 429 that really is the plan used up — no such system prompt asking —
// still rests the account, as it did (#147, #530).
func TestAntigravityQuotaStillRests(t *testing.T) {
	s, askedUsage := antigravityWithAMate(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		io.WriteString(w, turnedAway)
	})
	code, out := turnOn(t, s, claudeTurn("You are a helpful assistant."))
	if code != 200 || !strings.Contains(out, "from the mate") {
		t.Fatalf("the turn: %d %s", code, out)
	}
	r := lastRoute(s)
	if len(r.Tries) != 2 || r.Tries[0].Status != 429 || r.Tries[0].Fail != failQuota || r.Tries[0].Rest == nil {
		t.Fatalf("the used-up try: %+v", r.Tries)
	}
	if d := time.Until(r.Tries[0].Rest.Until); d < 14*time.Minute || d > 15*time.Minute {
		t.Fatalf("rests %v, not the 15 minutes a used-up plan takes", d)
	}
	if *askedUsage != 1 {
		t.Fatalf("the account's usage was asked again %d times, once as it was", *askedUsage)
	}
	if _, ok := restOf("antigravity@u@example.com"); !ok {
		t.Fatal("antigravity@u@example.com doesn't rest for a plan used up")
	}
}

// rests is what rests now, to say so when nothing should.
func rests() map[string]time.Time {
	restingUntil.Lock()
	defer restingUntil.Unlock()
	out := map[string]time.Time{}
	for k, v := range restingUntil.m {
		if v.After(time.Now()) {
			out[k] = v
		}
	}
	return out
}
