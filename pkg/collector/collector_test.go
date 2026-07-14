package collector

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	browserv1 "github.com/alcounit/browser-controller/apis/browser/v1"
	browserconfigv1 "github.com/alcounit/browser-controller/apis/browserconfig/v1"
	browserclient "github.com/alcounit/browser-service/pkg/client/browser"
	browserconfigclient "github.com/alcounit/browser-service/pkg/client/browserconfig"
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
	stream   *fakeStream
	err      error
	browsers []*browserv1.Browser
	listErr  error
}

func (c *fakeClient) Events(ctx context.Context, namespace string, opts ...event.EventsOption) (browserclient.EventStream, error) {
	if c.err != nil {
		return nil, c.err
	}
	return c.stream, nil
}

func (c *fakeClient) List(context.Context, string) ([]*browserv1.Browser, error) {
	return c.browsers, c.listErr
}

func (c *fakeClient) Create(context.Context, string, *browserv1.Browser) (*browserv1.Browser, error) {
	panic("not used")
}
func (c *fakeClient) Get(context.Context, string, string) (*browserv1.Browser, error) {
	panic("not used")
}
func (c *fakeClient) Delete(context.Context, string, string) error {
	panic("not used")
}

type fakeConfigStream struct {
	eventsCh chan *event.BrowserConfigEvent
	errorsCh chan error
}

func (s *fakeConfigStream) Events() <-chan *event.BrowserConfigEvent { return s.eventsCh }
func (s *fakeConfigStream) Errors() <-chan error                     { return s.errorsCh }
func (s *fakeConfigStream) Close() {
	// Close channels safely.
	select {
	case <-s.eventsCh:
	default:
	}
}

// fakeConfigClient implements browserconfigclient.Client with configurable behavior.
type fakeConfigClient struct {
	listErr   error
	listData  []*browserconfigv1.BrowserConfig
	stream    *fakeConfigStream
	eventsErr error
}

func (c *fakeConfigClient) Create(context.Context, string, *browserconfigv1.BrowserConfig) (*browserconfigv1.BrowserConfig, error) {
	panic("not used")
}
func (c *fakeConfigClient) Get(context.Context, string, string) (*browserconfigv1.BrowserConfig, error) {
	panic("not used")
}
func (c *fakeConfigClient) Delete(context.Context, string, string) error {
	panic("not used")
}
func (c *fakeConfigClient) List(context.Context, string) ([]*browserconfigv1.BrowserConfig, error) {
	return c.listData, c.listErr
}
func (c *fakeConfigClient) Events(ctx context.Context, namespace string, opts ...event.EventsOption) (browserconfigclient.EventStream, error) {
	if c.eventsErr != nil {
		return nil, c.eventsErr
	}
	if c.stream != nil {
		return c.stream, nil
	}
	// Default: create a stream that closes when ctx is done.
	s := &fakeConfigStream{
		eventsCh: make(chan *event.BrowserConfigEvent),
		errorsCh: make(chan error),
	}
	go func() {
		<-ctx.Done()
		close(s.eventsCh)
		close(s.errorsCh)
	}()
	return s, nil
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

func newConfigEvent(eventType event.EventType, name string, browsers map[string]map[string]*browserconfigv1.BrowserVersionConfigSpec) *event.BrowserConfigEvent {
	return &event.BrowserConfigEvent{
		EventType: eventType,
		BrowserConfig: &browserconfigv1.BrowserConfig{
			ObjectMeta: metav1.ObjectMeta{Name: name},
			Spec: browserconfigv1.BrowserConfigSpec{
				Browsers: browsers,
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

	col := NewCollector(client, &fakeConfigClient{}, "default", store.NewDefaultStore[*types.Session](), store.NewDefaultStore[types.BrowserVersions](), nil)
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
	col := NewCollector(client, &fakeConfigClient{}, "default", store.NewDefaultStore[*types.Session](), store.NewDefaultStore[types.BrowserVersions](), nil)

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
	col := NewCollector(client, &fakeConfigClient{}, "default", store.NewDefaultStore[*types.Session](), store.NewDefaultStore[types.BrowserVersions](), nil)

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
	st := store.NewDefaultStore[*types.Session]()
	st.Set("browser-1", &types.Session{BrowserId: "browser-1"})
	col := NewCollector(client, &fakeConfigClient{}, "default", st, store.NewDefaultStore[types.BrowserVersions](), nil)

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
	st := store.NewDefaultStore[*types.Session]()
	col := NewCollector(client, &fakeConfigClient{}, "default", st, store.NewDefaultStore[types.BrowserVersions](), nil)

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
	col := NewCollector(client, &fakeConfigClient{}, "default", store.NewDefaultStore[*types.Session](), store.NewDefaultStore[types.BrowserVersions](), nil)

	stream.eventsCh <- newBrowserEvent(event.EventTypeAdded, "browser-1", "not-an-ip")

	err := col.Run(context.Background())
	if err == nil || err.Error() != "failed to convert IP to UUID" {
		t.Fatalf("expected convert error, got %v", err)
	}
}

func TestCollectorRunListBrowsersError(t *testing.T) {
	cl := &fakeClient{listErr: errors.New("list error")}
	col := NewCollector(cl, &fakeConfigClient{}, "default", store.NewDefaultStore[*types.Session](), store.NewDefaultStore[types.BrowserVersions](), nil)

	err := col.Run(context.Background())
	if err == nil || err.Error() != "list error" {
		t.Fatalf("expected list error, got %v", err)
	}
}

func TestCollectorRunListBrowsersPopulatesStore(t *testing.T) {
	stream := &fakeStream{
		eventsCh: make(chan *event.BrowserEvent),
		errorsCh: make(chan error),
	}
	close(stream.eventsCh)

	now := metav1.NewTime(time.Unix(0, 0).UTC())
	existing := &browserv1.Browser{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "browser-existing",
			CreationTimestamp: now,
		},
		Spec: browserv1.BrowserSpec{
			BrowserName:    "chrome",
			BrowserVersion: "123",
		},
		Status: browserv1.BrowserStatus{
			PodIP: "127.0.0.1",
			Phase: corev1.PodRunning,
		},
	}

	st := store.NewDefaultStore[*types.Session]()
	cl := &fakeClient{stream: stream, browsers: []*browserv1.Browser{existing}}
	col := NewCollector(cl, &fakeConfigClient{}, "default", st, store.NewDefaultStore[types.BrowserVersions](), nil)

	col.Run(context.Background()) //nolint:errcheck

	sess, ok := st.Get("browser-existing")
	if !ok {
		t.Fatalf("expected browser-existing to be in store after ListBrowsers")
	}
	expectedID, _ := ipuuid.IPToUUID(net.ParseIP("127.0.0.1"))
	if sess.SessionId != expectedID.String() {
		t.Fatalf("expected sessionId %s, got %s", expectedID.String(), sess.SessionId)
	}
}

func TestCollectorRunAddsSession(t *testing.T) {
	stream := &fakeStream{
		eventsCh: make(chan *event.BrowserEvent, 1),
		errorsCh: make(chan error, 1),
	}
	client := &fakeClient{stream: stream}
	st := store.NewDefaultStore[*types.Session]()
	col := NewCollector(client, &fakeConfigClient{}, "default", st, store.NewDefaultStore[types.BrowserVersions](), nil)

	stream.eventsCh <- newBrowserEvent(event.EventTypeAdded, "browser-1", "127.0.0.1")
	close(stream.eventsCh)

	err := col.Run(context.Background())
	if err == nil || err.Error() != "browser event stream closed unexpectedly" {
		t.Fatalf("expected stream closed error, got %v", err)
	}

	sess, ok := st.Get("browser-1")
	if !ok {
		t.Fatalf("expected browser-1 to be stored")
	}

	expectedID, _ := ipuuid.IPToUUID(net.ParseIP("127.0.0.1"))
	if sess.SessionId != expectedID.String() {
		t.Fatalf("expected sessionId %s, got %s", expectedID.String(), sess.SessionId)
	}
	if sess.BrowserIP != "127.0.0.1" {
		t.Fatalf("expected browserIP 127.0.0.1, got %s", sess.BrowserIP)
	}
}

// --- New tests for collector ---

func TestCollectorRunListConfigsError(t *testing.T) {
	// browser stream that is ready
	stream := &fakeStream{
		eventsCh: make(chan *event.BrowserEvent),
		errorsCh: make(chan error),
	}
	cl := &fakeClient{stream: stream}
	cfgClient := &fakeConfigClient{listErr: errors.New("config list error")}

	col := NewCollector(cl, cfgClient, "default", store.NewDefaultStore[*types.Session](), store.NewDefaultStore[types.BrowserVersions](), nil)
	err := col.Run(context.Background())
	if err == nil || err.Error() != "config list error" {
		t.Fatalf("expected config list error, got %v", err)
	}
}

func TestCollectorRunListConfigsPopulatesStore(t *testing.T) {
	// browser stream immediately closed so we get an error
	stream := &fakeStream{
		eventsCh: make(chan *event.BrowserEvent),
		errorsCh: make(chan error),
	}
	close(stream.eventsCh)

	cfg := &browserconfigv1.BrowserConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "cfg-1"},
		Spec: browserconfigv1.BrowserConfigSpec{
			Browsers: map[string]map[string]*browserconfigv1.BrowserVersionConfigSpec{
				"chrome": {
					"123": {Image: "chrome:123"},
				},
			},
		},
	}

	cl := &fakeClient{stream: stream}
	cfgClient := &fakeConfigClient{listData: []*browserconfigv1.BrowserConfig{cfg}}
	cfgStore := store.NewDefaultStore[types.BrowserVersions]()

	col := NewCollector(cl, cfgClient, "default", store.NewDefaultStore[*types.Session](), cfgStore, nil)
	col.Run(context.Background()) //nolint:errcheck

	bv, ok := cfgStore.Get("cfg-1")
	if !ok {
		t.Fatalf("expected cfg-1 to be in config store")
	}
	versions, ok := bv["chrome"]
	if !ok {
		t.Fatalf("expected chrome in browser versions")
	}
	if len(versions) != 1 || versions[0] != "123" {
		t.Fatalf("expected version 123, got %v", versions)
	}
}

func TestCollectorRunConfigEventsError(t *testing.T) {
	browserStream := &fakeStream{
		eventsCh: make(chan *event.BrowserEvent, 1),
		errorsCh: make(chan error, 1),
	}
	configStream := &fakeConfigStream{
		eventsCh: make(chan *event.BrowserConfigEvent, 1),
		errorsCh: make(chan error, 1),
	}

	cl := &fakeClient{stream: browserStream}
	cfgClient := &fakeConfigClient{stream: configStream}

	col := NewCollector(cl, cfgClient, "default", store.NewDefaultStore[*types.Session](), store.NewDefaultStore[types.BrowserVersions](), nil)

	configStream.errorsCh <- errors.New("config stream error")

	err := col.Run(context.Background())
	if err == nil || err.Error() != "config stream error" {
		t.Fatalf("expected config stream error, got %v", err)
	}
}

func TestCollectorRunConfigStreamClosed(t *testing.T) {
	browserStream := &fakeStream{
		eventsCh: make(chan *event.BrowserEvent, 1),
		errorsCh: make(chan error, 1),
	}
	configStream := &fakeConfigStream{
		eventsCh: make(chan *event.BrowserConfigEvent),
		errorsCh: make(chan error),
	}
	close(configStream.eventsCh)

	cl := &fakeClient{stream: browserStream}
	cfgClient := &fakeConfigClient{stream: configStream}

	col := NewCollector(cl, cfgClient, "default", store.NewDefaultStore[*types.Session](), store.NewDefaultStore[types.BrowserVersions](), nil)

	err := col.Run(context.Background())
	if err == nil || err.Error() != "browser config event stream closed unexpectedly" {
		t.Fatalf("expected config stream closed error, got %v", err)
	}
}

func TestCollectorRunConfigEventAdded(t *testing.T) {
	browserStream := &fakeStream{
		eventsCh: make(chan *event.BrowserEvent, 1),
		errorsCh: make(chan error, 1),
	}
	configStream := &fakeConfigStream{
		eventsCh: make(chan *event.BrowserConfigEvent, 2),
		errorsCh: make(chan error, 1),
	}

	cfgStore := store.NewDefaultStore[types.BrowserVersions]()
	cl := &fakeClient{stream: browserStream}
	cfgClient := &fakeConfigClient{stream: configStream}

	col := NewCollector(cl, cfgClient, "default", store.NewDefaultStore[*types.Session](), cfgStore, nil)

	browsers := map[string]map[string]*browserconfigv1.BrowserVersionConfigSpec{
		"firefox": {"100": {Image: "firefox:100"}},
	}
	configStream.eventsCh <- newConfigEvent(event.EventTypeAdded, "cfg-added", browsers)
	// After config event, close browser stream to end the loop.
	close(configStream.eventsCh)

	col.Run(context.Background()) //nolint:errcheck

	bv, ok := cfgStore.Get("cfg-added")
	if !ok {
		t.Fatalf("expected cfg-added to be in config store")
	}
	if _, ok := bv["firefox"]; !ok {
		t.Fatalf("expected firefox in browser versions")
	}
}

func TestCollectorRunConfigEventModified(t *testing.T) {
	browserStream := &fakeStream{
		eventsCh: make(chan *event.BrowserEvent, 1),
		errorsCh: make(chan error, 1),
	}
	configStream := &fakeConfigStream{
		eventsCh: make(chan *event.BrowserConfigEvent, 2),
		errorsCh: make(chan error, 1),
	}

	cfgStore := store.NewDefaultStore[types.BrowserVersions]()
	cfgStore.Set("cfg-modified", types.BrowserVersions{"old-browser": {"1.0"}})

	cl := &fakeClient{stream: browserStream}
	cfgClient := &fakeConfigClient{stream: configStream}

	col := NewCollector(cl, cfgClient, "default", store.NewDefaultStore[*types.Session](), cfgStore, nil)

	browsers := map[string]map[string]*browserconfigv1.BrowserVersionConfigSpec{
		"new-browser": {"2.0": {Image: "new:2.0"}},
	}
	configStream.eventsCh <- newConfigEvent(event.EventTypeModified, "cfg-modified", browsers)
	close(configStream.eventsCh)

	col.Run(context.Background()) //nolint:errcheck

	bv, ok := cfgStore.Get("cfg-modified")
	if !ok {
		t.Fatalf("expected cfg-modified to be in config store")
	}
	if _, ok := bv["new-browser"]; !ok {
		t.Fatalf("expected new-browser in browser versions after modify")
	}
	if _, ok := bv["old-browser"]; ok {
		t.Fatalf("expected old-browser to be replaced after modify")
	}
}

func TestCollectorRunConfigEventDeleted(t *testing.T) {
	browserStream := &fakeStream{
		eventsCh: make(chan *event.BrowserEvent, 1),
		errorsCh: make(chan error, 1),
	}
	configStream := &fakeConfigStream{
		eventsCh: make(chan *event.BrowserConfigEvent, 2),
		errorsCh: make(chan error, 1),
	}

	cfgStore := store.NewDefaultStore[types.BrowserVersions]()
	cfgStore.Set("cfg-del", types.BrowserVersions{"chrome": {"123"}})

	cl := &fakeClient{stream: browserStream}
	cfgClient := &fakeConfigClient{stream: configStream}

	col := NewCollector(cl, cfgClient, "default", store.NewDefaultStore[*types.Session](), cfgStore, nil)

	configStream.eventsCh <- newConfigEvent(event.EventTypeDeleted, "cfg-del", nil)
	close(configStream.eventsCh)

	col.Run(context.Background()) //nolint:errcheck

	if _, ok := cfgStore.Get("cfg-del"); ok {
		t.Fatalf("expected cfg-del to be deleted from config store")
	}
}

func TestCollectorRunContextCancelled(t *testing.T) {
	browserStream := &fakeStream{
		eventsCh: make(chan *event.BrowserEvent),
		errorsCh: make(chan error),
	}
	cl := &fakeClient{stream: browserStream}

	col := NewCollector(cl, &fakeConfigClient{}, "default", store.NewDefaultStore[*types.Session](), store.NewDefaultStore[types.BrowserVersions](), nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before Run

	err := col.Run(ctx)
	if err != context.Canceled {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestCollectorRunInitialListBrowsersInvalidIP(t *testing.T) {
	// The initial list of browsers contains one with an invalid IP.
	now := metav1.NewTime(time.Unix(0, 0).UTC())
	existing := &browserv1.Browser{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "browser-bad",
			CreationTimestamp: now,
		},
		Spec: browserv1.BrowserSpec{
			BrowserName:    "chrome",
			BrowserVersion: "123",
		},
		Status: browserv1.BrowserStatus{
			PodIP: "not-an-ip",
			Phase: corev1.PodRunning,
		},
	}

	cl := &fakeClient{browsers: []*browserv1.Browser{existing}}
	col := NewCollector(cl, &fakeConfigClient{}, "default", store.NewDefaultStore[*types.Session](), store.NewDefaultStore[types.BrowserVersions](), nil)

	err := col.Run(context.Background())
	if err == nil || err.Error() != "failed to convert IP to UUID" {
		t.Fatalf("expected convert error, got %v", err)
	}
}

func TestStoreSessionSetsOwnerFromLabel(t *testing.T) {
	stream := &fakeStream{
		eventsCh: make(chan *event.BrowserEvent, 1),
		errorsCh: make(chan error, 1),
	}
	client := &fakeClient{stream: stream}
	st := store.NewDefaultStore[*types.Session]()
	col := NewCollector(client, &fakeConfigClient{}, "default", st, store.NewDefaultStore[types.BrowserVersions](), nil)

	now := metav1.NewTime(time.Unix(0, 0).UTC())
	ev := &event.BrowserEvent{
		EventType: event.EventTypeAdded,
		Browser: &browserv1.Browser{
			ObjectMeta: metav1.ObjectMeta{
				Name:              "browser-owned",
				CreationTimestamp: now,
				Labels:            map[string]string{browserv1.SelenosisOwnerLabelKey: "alice"},
			},
			Spec:   browserv1.BrowserSpec{BrowserName: "chrome", BrowserVersion: "123"},
			Status: browserv1.BrowserStatus{PodIP: "127.0.0.1", Phase: corev1.PodRunning},
		},
	}
	stream.eventsCh <- ev
	close(stream.eventsCh)

	col.Run(context.Background()) //nolint:errcheck

	sess, ok := st.Get("browser-owned")
	if !ok {
		t.Fatal("expected browser-owned to be stored")
	}
	if sess.Owner != "alice" {
		t.Fatalf("expected owner alice, got %q", sess.Owner)
	}
}

func TestStoreSessionNoOwnerLabel(t *testing.T) {
	stream := &fakeStream{
		eventsCh: make(chan *event.BrowserEvent, 1),
		errorsCh: make(chan error, 1),
	}
	client := &fakeClient{stream: stream}
	st := store.NewDefaultStore[*types.Session]()
	col := NewCollector(client, &fakeConfigClient{}, "default", st, store.NewDefaultStore[types.BrowserVersions](), nil)

	stream.eventsCh <- newBrowserEvent(event.EventTypeAdded, "browser-noowner", "127.0.0.1")
	close(stream.eventsCh)

	col.Run(context.Background()) //nolint:errcheck

	sess, ok := st.Get("browser-noowner")
	if !ok {
		t.Fatal("expected browser-noowner to be stored")
	}
	if sess.Owner != "" {
		t.Fatalf("expected empty owner, got %q", sess.Owner)
	}
}

func TestCollectorRunConfigEventsStreamError(t *testing.T) {
	// configClient.Events returns an error.
	stream := &fakeStream{
		eventsCh: make(chan *event.BrowserEvent),
		errorsCh: make(chan error),
	}
	cl := &fakeClient{stream: stream}
	cfgClient := &fakeConfigClient{eventsErr: errors.New("config events error")}

	col := NewCollector(cl, cfgClient, "default", store.NewDefaultStore[*types.Session](), store.NewDefaultStore[types.BrowserVersions](), nil)

	err := col.Run(context.Background())
	if err == nil || err.Error() != "config events error" {
		t.Fatalf("expected config events error, got %v", err)
	}
}
