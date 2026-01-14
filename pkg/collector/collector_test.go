package collector

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	browserv1 "github.com/alcounit/browser-controller/apis/browser/v1"
	"github.com/alcounit/browser-service/pkg/client"
	"github.com/alcounit/browser-service/pkg/event"
	"github.com/alcounit/browser-ui/pkg/types"
	"github.com/alcounit/seleniferous/v2/pkg/store"
	"github.com/alcounit/selenosis/v2/pkg/ipuuid"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type fakeStream struct {
	eventsCh chan *event.BrowserEvent
	errorsCh chan error
	closed   bool
}

func (s *fakeStream) Events() <-chan *event.BrowserEvent { return s.eventsCh }
func (s *fakeStream) Errors() <-chan error               { return s.errorsCh }
func (s *fakeStream) Close()                             { s.closed = true }

type fakeClient struct {
	stream *fakeStream
	err    error
}

func (c *fakeClient) Events(ctx context.Context, namespace string) (client.BrowserEventStream, error) {
	if c.err != nil {
		return nil, c.err
	}
	return c.stream, nil
}

func (c *fakeClient) CreateBrowser(context.Context, string, *browserv1.Browser) (*browserv1.Browser, error) {
	panic("not used")
}
func (c *fakeClient) GetBrowser(context.Context, string, string) (*browserv1.Browser, error) {
	panic("not used")
}
func (c *fakeClient) DeleteBrowser(context.Context, string, string) error {
	panic("not used")
}
func (c *fakeClient) ListBrowsers(context.Context, string) ([]*browserv1.Browser, error) {
	panic("not used")
}

func newBrowserEvent(eventType event.EventType, name, podIP string) *event.BrowserEvent {
	now := metav1.NewTime(time.Unix(0, 0).UTC())
	return &event.BrowserEvent{
		EventType: eventType,
		Browser: &browserv1.Browser{
			ObjectMeta: metav1.ObjectMeta{
				Name:              name,
				CreationTimestamp: now,
			},
			Spec: browserv1.BrowserSpec{
				BrowserName:    "chrome",
				BrowserVersion: "123",
			},
			Status: browserv1.BrowserStatus{
				PodIP: podIP,
				Phase: corev1.PodRunning,
			},
		},
	}
}

func TestCollectorRunStreamClosed(t *testing.T) {
	stream := &fakeStream{
		eventsCh: make(chan *event.BrowserEvent),
		errorsCh: make(chan error),
	}
	close(stream.eventsCh)
	client := &fakeClient{stream: stream}

	col := NewCollector(client, "default", store.NewDefaultStore(), nil)
	err := col.Run(context.Background())
	if err == nil || err.Error() != "browser event stream closed unexpectedly" {
		t.Fatalf("expected stream closed error, got %v", err)
	}
	if !stream.closed {
		t.Fatalf("expected stream to be closed")
	}
}

func TestCollectorRunEventsError(t *testing.T) {
	client := &fakeClient{err: errors.New("events error")}
	col := NewCollector(client, "default", store.NewDefaultStore(), nil)

	err := col.Run(context.Background())
	if err == nil || err.Error() != "events error" {
		t.Fatalf("expected events error, got %v", err)
	}
}

func TestCollectorRunStreamError(t *testing.T) {
	stream := &fakeStream{
		eventsCh: make(chan *event.BrowserEvent, 1),
		errorsCh: make(chan error, 1),
	}
	client := &fakeClient{stream: stream}
	col := NewCollector(client, "default", store.NewDefaultStore(), nil)

	stream.errorsCh <- errors.New("stream error")

	err := col.Run(context.Background())
	if err == nil || err.Error() != "stream error" {
		t.Fatalf("expected stream error, got %v", err)
	}
}

func TestCollectorRunDeletesSession(t *testing.T) {
	stream := &fakeStream{
		eventsCh: make(chan *event.BrowserEvent, 1),
		errorsCh: make(chan error, 1),
	}
	client := &fakeClient{stream: stream}
	st := store.NewDefaultStore()
	st.Set("browser-1", "value")
	col := NewCollector(client, "default", st, nil)

	stream.eventsCh <- newBrowserEvent(event.EventTypeDeleted, "browser-1", "")
	close(stream.eventsCh)

	err := col.Run(context.Background())
	if err == nil || err.Error() != "browser event stream closed unexpectedly" {
		t.Fatalf("expected stream closed error, got %v", err)
	}
	if _, ok := st.Get("browser-1"); ok {
		t.Fatalf("expected browser-1 to be deleted")
	}
}

func TestCollectorRunSkipsEmptyPodIP(t *testing.T) {
	stream := &fakeStream{
		eventsCh: make(chan *event.BrowserEvent, 1),
		errorsCh: make(chan error, 1),
	}
	client := &fakeClient{stream: stream}
	st := store.NewDefaultStore()
	col := NewCollector(client, "default", st, nil)

	stream.eventsCh <- newBrowserEvent(event.EventTypeAdded, "browser-1", "")
	close(stream.eventsCh)

	err := col.Run(context.Background())
	if err == nil || err.Error() != "browser event stream closed unexpectedly" {
		t.Fatalf("expected stream closed error, got %v", err)
	}
	if st.Len() != 0 {
		t.Fatalf("expected store to be empty")
	}
}

func TestCollectorRunInvalidIP(t *testing.T) {
	stream := &fakeStream{
		eventsCh: make(chan *event.BrowserEvent, 1),
		errorsCh: make(chan error, 1),
	}
	client := &fakeClient{stream: stream}
	col := NewCollector(client, "default", store.NewDefaultStore(), nil)

	stream.eventsCh <- newBrowserEvent(event.EventTypeAdded, "browser-1", "not-an-ip")

	err := col.Run(context.Background())
	if err == nil || err.Error() != "failed to convert IP to UUID" {
		t.Fatalf("expected convert error, got %v", err)
	}
}

func TestCollectorRunAddsSession(t *testing.T) {
	stream := &fakeStream{
		eventsCh: make(chan *event.BrowserEvent, 1),
		errorsCh: make(chan error, 1),
	}
	client := &fakeClient{stream: stream}
	st := store.NewDefaultStore()
	col := NewCollector(client, "default", st, nil)

	stream.eventsCh <- newBrowserEvent(event.EventTypeAdded, "browser-1", "127.0.0.1")
	close(stream.eventsCh)

	err := col.Run(context.Background())
	if err == nil || err.Error() != "browser event stream closed unexpectedly" {
		t.Fatalf("expected stream closed error, got %v", err)
	}

	val, ok := st.Get("browser-1")
	if !ok {
		t.Fatalf("expected browser-1 to be stored")
	}

	sess, ok := val.(*types.Session)
	if !ok {
		t.Fatalf("expected stored value to be *types.Session")
	}

	expectedID, _ := ipuuid.IPToUUID(net.ParseIP("127.0.0.1"))
	if sess.SessionId != expectedID.String() {
		t.Fatalf("expected sessionId %s, got %s", expectedID.String(), sess.SessionId)
	}
	if sess.BrowserIP != "127.0.0.1" {
		t.Fatalf("expected browserIP 127.0.0.1, got %s", sess.BrowserIP)
	}
}
