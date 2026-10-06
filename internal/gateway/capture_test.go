package gateway

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yetone/magpie/internal/redact"
	"github.com/yetone/magpie/internal/settings"
)

func TestCaptureRequestBodyLimit(t *testing.T) {
	body := []byte(strings.Repeat("x", callBodyLimit+17))
	got, truncated := captureRequestBody(body)
	if len(got) != callBodyLimit || !truncated {
		t.Fatalf("captured %d bytes, truncated=%v", len(got), truncated)
	}
}

func TestCaptureResponseWriterPreservesResponse(t *testing.T) {
	r := httptest.NewRecorder()
	w := &captureResponseWriter{ResponseWriter: r}
	_, _ = w.Write([]byte(`{"ok":true}`))
	if got := w.body.text(); got != `{"ok":true}` {
		t.Fatalf("capture = %q", got)
	}
	if got := r.Body.String(); got != `{"ok":true}` {
		t.Fatalf("response = %q", got)
	}
}

// The bodies the OTLP export carries take the user's own masking rules out
// with the secrets, as the request archive does (#195): a relay key of a
// format magpie's rules don't know is not in what is sent to the collector.
func TestBodyForExportKeepsTheUsersRules(t *testing.T) {
	fresh(t)
	if err := settings.Save(settings.Settings{Redact: true,
		RedactRules: []redact.Rule{{Kind: "RELAY", Prefix: "rz_"}}}); err != nil {
		t.Fatal(err)
	}
	body := `{"choices":[{"message":{"content":"using ` + relayKey + `"}}]}`
	if got := bodyForExport(body, false); strings.Contains(got, relayKey) || !strings.Contains(got, "[REDACTED:RELAY]") {
		t.Errorf("exported body: %s", got)
	}
}
