package collector

import (
	"context"
	"errors"
	"net"
	"strings"

	"github.com/alcounit/browser-service/pkg/broadcast"
	browserclient "github.com/alcounit/browser-service/pkg/client/browser"
	browserconfigclient "github.com/alcounit/browser-service/pkg/client/browserconfig"
	"github.com/alcounit/browser-service/pkg/event"
	"github.com/alcounit/browser-ui/pkg/types"
	"github.com/alcounit/seleniferous/v2/pkg/store"
	"github.com/alcounit/selenosis/v2/pkg/ipuuid"

	browserv1 "github.com/alcounit/browser-controller/apis/browser/v1"
	browserconfigv1 "github.com/alcounit/browser-controller/apis/browserconfig/v1"
	logctx "github.com/alcounit/browser-controller/pkg/log"
	corev1 "k8s.io/api/core/v1"
)

type Collector struct {
	browserClient browserclient.Client
	configClient  browserconfigclient.Client
	namespace     string
	sessionStore  store.Store[*types.Session]
	configStore   store.Store[types.BrowserVersions]
	broadcaster   broadcast.Broadcaster[event.BrowserEvent]
}

func NewCollector(browserClient browserclient.Client, configClient browserconfigclient.Client, namespace string, sessionStore store.Store[*types.Session], configStore store.Store[types.BrowserVersions], broadcaster broadcast.Broadcaster[event.BrowserEvent]) *Collector {
	return &Collector{browserClient, configClient, namespace, sessionStore, configStore, broadcaster}
}

func (c *Collector) Run(ctx context.Context) error {
	log := logctx.FromContext(ctx)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	browsers, err := c.browserClient.List(ctx, c.namespace)
	if err != nil {
		log.Error().Err(err).Msg("failed to list browsers")
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

	configs, err := c.configClient.List(ctx, c.namespace)
	if err != nil {
		log.Error().Err(err).Msg("failed to list browser configs")
		return err
	}

	for _, cfg := range configs {
		storeBrowserConfig(cfg.Name, cfg, c)
		log.Info().Str("configName", cfg.Name).Msg("add browser config to store")
	}

	browserStream, err := c.browserClient.Events(ctx, c.namespace)
	if err != nil {
		return err
	}
	defer browserStream.Close()

	configStream, err := c.configClient.Events(ctx, c.namespace)
	if err != nil {
		return err
	}
	defer configStream.Close()

	log.Info().Msg("starting collector")

	for {
		select {
		case browserEvent, ok := <-browserStream.Events():
			if !ok {
				log.Error().Msg("browser event stream closed unexpectedly")
				return errors.New("browser event stream closed unexpectedly")
			}

			switch browserEvent.EventType {
			case event.EventTypeDeleted:
				c.sessionStore.Delete(browserEvent.Browser.Name)
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

		case configEvent, ok := <-configStream.Events():
			if !ok {
				log.Error().Msg("browser config event stream closed unexpectedly")
				return errors.New("browser config event stream closed unexpectedly")
			}

			cfg := configEvent.BrowserConfig
			switch configEvent.EventType {
			case event.EventTypeDeleted:
				c.configStore.Delete(cfg.Name)
				log.Info().Str("eventType", "deleted").Str("configName", cfg.Name).Msg("delete browser config from store")
			case event.EventTypeAdded, event.EventTypeModified:

				storeBrowserConfig(cfg.Name, cfg, c)
				eventType := strings.ToLower(string(configEvent.EventType))
				log.Info().Str("eventType", eventType).Str("configName", cfg.Name).Msg("add/update browser config in store")
			}

		case err, ok := <-browserStream.Errors():
			if ok && err != nil {
				return err
			}

		case err, ok := <-configStream.Errors():
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

func storeBrowserConfig(configName string, cfg *browserconfigv1.BrowserConfig, c *Collector) {
	result := make(types.BrowserVersions, len(cfg.Spec.Browsers))
	for browserName, versions := range cfg.Spec.Browsers {
		vs := make([]string, 0, len(versions))
		for v := range versions {
			vs = append(vs, v)
		}
		result[browserName] = vs
	}

	c.configStore.Set(configName, result)
}

func storeSession(sessionId string, browser *browserv1.Browser, c *Collector) {
	sess := &types.Session{
		SessionId:       sessionId,
		BrowserId:       browser.Name,
		BrowserIP:       browser.Status.PodIP,
		BrowserName:     browser.Spec.BrowserName,
		BrowserVersion:  browser.Spec.BrowserVersion,
		Owner:           browser.Labels[browserv1.SelenosisOwnerLabelKey],
		StartedManually: browser.Annotations["startedManually"] == "true",
		StartTime:       browser.CreationTimestamp.DeepCopy(),
		Phase:           corev1.PodPhase(browser.Status.Phase),
	}

	c.sessionStore.Set(browser.Name, sess)
}
