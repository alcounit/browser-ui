package types

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestSessionJSONOmitsBrowserIP(t *testing.T) {
	now := metav1.NewTime(time.Unix(0, 0).UTC())
	sess := Session{
		SessionId:      "sess-1",
		BrowserId:      "browser-1",
		BrowserIP:      "10.0.0.1",
		BrowserName:    "chrome",
		BrowserVersion: "123",
		StartTime:      &now,
		Phase:          corev1.PodRunning,
	}

	raw, err := json.Marshal(sess)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if strings.Contains(string(raw), "browserIP") {
		t.Fatalf("expected browserIP to be omitted, got %s", string(raw))
	}
	if !strings.Contains(string(raw), "\"sessionId\"") {
		t.Fatalf("expected sessionId in JSON, got %s", string(raw))
	}
}
