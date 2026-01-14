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

				sess := &types.Session{
					SessionId:      sessionId,
					BrowserId:      browserEvent.Browser.Name,
					BrowserIP:      browserEvent.Browser.Status.PodIP,
					BrowserName:    browserEvent.Browser.Spec.BrowserName,
					BrowserVersion: browserEvent.Browser.Spec.BrowserVersion,
					StartTime:      browserEvent.Browser.CreationTimestamp.DeepCopy(),
					Phase:          corev1.PodPhase(browserEvent.Browser.Status.Phase),
				}
				eventType := strings.ToLower(string(browserEvent.EventType))

				c.store.Set(browserEvent.Browser.Name, sess)
				log.Info().Str("eventType", eventType).Str("sessionId", sess.SessionId).Msg("add/update session in store")
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
