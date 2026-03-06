package collector

import (
	"context"
	"errors"
	"net"
	"strings"

	"github.com/alcounit/browser-service/pkg/broadcast"
	"github.com/alcounit/browser-service/pkg/client"
	"github.com/alcounit/browser-service/pkg/event"
	"github.com/alcounit/browser-ui/pkg/types"
	"github.com/alcounit/seleniferous/v2/pkg/store"
	"github.com/alcounit/selenosis/v2/pkg/ipuuid"

	browserv1 "github.com/alcounit/browser-controller/apis/browser/v1"
	logctx "github.com/alcounit/browser-controller/pkg/log"
	corev1 "k8s.io/api/core/v1"
)

type Collector struct {
	client      client.Client
	namespace   string
	store       store.Store
	broadcaster broadcast.Broadcaster[event.BrowserEvent]
}

func NewCollector(client client.Client, namespace string, store store.Store, broadcaster broadcast.Broadcaster[event.BrowserEvent]) *Collector {
	return &Collector{client, namespace, store, broadcaster}
}

func (c *Collector) Run(ctx context.Context) error {
	log := logctx.FromContext(ctx)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	browsers, err := c.client.ListBrowsers(ctx, c.namespace)
	if err != nil {
		return err

	}

	for _, browser := range browsers {
		sessionId, err := parseIp(browser.Status.PodIP)
		if err != nil {
			log.Error().Err(err).Str("sessionId", sessionId).Msgf("sessionId not found")
			return errors.New("failed to convert IP to UUID")
		}

		storeSession(sessionId, browser, c)
		log.Info().Str("sessionId", sessionId).Msg("add session to store")

	}

	stream, err := c.client.Events(ctx, c.namespace)
	if err != nil {
		return err
	}
	defer stream.Close()

	log.Info().Msg("starting collector")

	for {
		select {
		case browserEvent, ok := <-stream.Events():
			if !ok {
				log.Error().Msg("browser event stream closed unexpectedly")
				return errors.New("browser event stream closed unexpectedly")
			}

			switch browserEvent.EventType {
			case event.EventTypeDeleted:
				c.store.Delete(browserEvent.Browser.Name)
				log.Info().Str("eventType", "deleted").Str("sessionId", browserEvent.Browser.Name).Msg("delete session from store")
				continue
			case event.EventTypeAdded, event.EventTypeModified:
				if browserEvent.Browser.Status.PodIP == "" {
					continue
				}
				sessionId, err := parseIp(browserEvent.Browser.Status.PodIP)
				if err != nil {
					log.Error().Err(err).Str("sessionId", sessionId).Msgf("sessionId not found")
					return errors.New("failed to convert IP to UUID")
				}

				storeSession(sessionId, browserEvent.Browser, c)

				eventType := strings.ToLower(string(browserEvent.EventType))
				log.Info().Str("eventType", eventType).Str("sessionId", sessionId).Msg("add/update session in store")
				continue
			}

		case err, ok := <-stream.Errors():
			if ok && err != nil {
				return err
			}

		case <-ctx.Done():
			return context.Canceled
		}
	}
}

func parseIp(ip string) (string, error) {
	netIp := net.ParseIP(ip)
	rawId, err := ipuuid.IPToUUID(netIp)
	return rawId.String(), err

}

func storeSession(sessionId string, browser *browserv1.Browser, c *Collector) {
	sess := &types.Session{
		SessionId:      sessionId,
		BrowserId:      browser.Name,
		BrowserIP:      browser.Status.PodIP,
		BrowserName:    browser.Spec.BrowserName,
		BrowserVersion: browser.Spec.BrowserVersion,
		StartTime:      browser.CreationTimestamp.DeepCopy(),
		Phase:          corev1.PodPhase(browser.Status.Phase),
	}
	c.store.Set(browser.Name, sess)
}
