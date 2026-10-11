package app

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/addodelgrossi/ovm/pkg/onvif/events"
)

const eventsWSDLNamespace = "http://www.onvif.org/ver10/events/wsdl"

const (
	// eventsTermination is the subscription lifetime requested on create and
	// on every renewal.
	eventsTermination = time.Minute
	// eventsRenewAfter renews well before the termination time.
	eventsRenewAfter = 30 * time.Second
	// eventsPullTimeout is how long one PullMessages waits on the camera.
	eventsPullTimeout = 5 * time.Second
	// eventsPullLimit bounds one pull.
	eventsPullLimit = 100
	// eventsCallTimeout bounds one events SOAP exchange, including the
	// camera-side wait.
	eventsCallTimeout = eventsPullTimeout + 15*time.Second
)

// EventItemDTO is one name/value pair of an event.
type EventItemDTO struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// EventDTO is one camera notification. Topic has namespace prefixes removed.
type EventDTO struct {
	Topic             string         `json:"topic"`
	UTCTime           string         `json:"utc_time,omitempty"`
	PropertyOperation string         `json:"property_operation,omitempty"`
	Source            []EventItemDTO `json:"source"`
	Data              []EventItemDTO `json:"data"`
}

// EventsRequest opens a PullPoint subscription. An empty TopicFilter
// subscribes to every topic.
type EventsRequest struct {
	DeviceRequest
	TopicFilter string
}

// EventSubscription is a live PullPoint subscription. Pull and Close must not
// be called concurrently with each other.
type EventSubscription interface {
	// Pull waits briefly for events, renewing the subscription when due.
	Pull(ctx context.Context) ([]EventDTO, error)
	// Close unsubscribes, best effort.
	Close(ctx context.Context) error
}

// EventsUseCase subscribes to camera events.
type EventsUseCase interface {
	SubscribeEvents(ctx context.Context, request EventsRequest) (EventSubscription, error)
}

// NewProductionEvents returns the production EventsUseCase.
func NewProductionEvents() EventsUseCase { return &productionMedia{} }

type pullPointSubscription struct {
	session      *session
	client       *events.Client
	subscription events.Subscription
	now          func() time.Time
	renewedAt    time.Time

	closeOnce sync.Once
}

// SubscribeEvents creates a PullPoint subscription on the camera's Events
// service. The service and subscription addresses must be same-origin with
// the camera.
func (p *productionMedia) SubscribeEvents(ctx context.Context, request EventsRequest) (EventSubscription, error) {
	if err := events.ValidateTopicFilter(request.TopicFilter); err != nil {
		return nil, usageError("the topic filter is not valid")
	}
	cameraURL, seam, _, err := p.prepare(request.Host, eventsCallTimeout, request.Credentials)
	if err != nil {
		return nil, err
	}
	timeout := request.Timeout
	if timeout <= 0 {
		timeout = defaultMediaTimeout
	}
	opCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	seam.negotiate(opCtx)

	services, err := seam.Services(opCtx)
	if err != nil {
		return nil, mapMediaError(err)
	}
	var endpoints []string
	for _, service := range services {
		if service.Namespace == eventsWSDLNamespace {
			endpoints = append(endpoints, service.OperationalXAddr())
		}
	}
	if len(endpoints) == 0 {
		return nil, newError(CategoryProtocol, "the camera does not offer the Events service", nil)
	}
	if len(endpoints) > 1 {
		return nil, securityError("the camera advertises duplicate events services")
	}
	endpoint, err := validateSameOriginEndpoint(cameraURL, endpoints[0])
	if err != nil {
		return nil, securityError("the events service endpoint is not same-origin with the camera")
	}
	client, err := events.NewClient(seam.session.soapClient, endpoint.String())
	if err != nil {
		return nil, securityError("the events client could not be constructed for the advertised endpoint")
	}
	var subscription events.Subscription
	err = seam.session.invokeWithAuth(opCtx, func(ctx context.Context, header []byte) error {
		var callErr error
		subscription, callErr = client.CreatePullPointSubscription(ctx, request.TopicFilter, eventsTermination, eventsOptions(header)...)
		return callErr
	})
	if err != nil {
		return nil, mapConfigError(err)
	}
	address, err := validateSameOriginEndpoint(cameraURL, subscription.Address)
	if err != nil {
		return nil, securityError("the subscription address is not same-origin with the camera")
	}
	subscription.Address = address.String()
	return &pullPointSubscription{
		session: seam.session, client: client, subscription: subscription, now: p.now, renewedAt: p.now(),
	}, nil
}

func eventsOptions(header []byte) []events.RequestOptions {
	if header == nil {
		return nil
	}
	return []events.RequestOptions{{HeaderXML: header}}
}

// Pull renews the subscription when due, then pulls once.
func (s *pullPointSubscription) Pull(ctx context.Context) ([]EventDTO, error) {
	if s.now().Sub(s.renewedAt) >= eventsRenewAfter {
		err := s.session.invokeWithAuth(ctx, func(ctx context.Context, header []byte) error {
			_, callErr := s.client.Renew(ctx, s.subscription, eventsTermination, eventsOptions(header)...)
			return callErr
		})
		if err != nil {
			return nil, mapConfigError(err)
		}
		s.renewedAt = s.now()
	}
	var result events.PullResult
	err := s.session.invokeWithAuth(ctx, func(ctx context.Context, header []byte) error {
		var callErr error
		result, callErr = s.client.PullMessages(ctx, s.subscription, eventsPullTimeout, eventsPullLimit, eventsOptions(header)...)
		return callErr
	})
	if err != nil {
		return nil, mapConfigError(err)
	}
	out := make([]EventDTO, 0, len(result.Messages))
	for _, message := range result.Messages {
		dto := EventDTO{
			Topic: message.Topic, PropertyOperation: message.PropertyOperation,
			Source: eventItems(message.Source), Data: eventItems(message.Data),
		}
		if !message.UTCTime.IsZero() {
			dto.UTCTime = message.UTCTime.Format(time.RFC3339Nano)
		}
		out = append(out, dto)
	}
	return out, nil
}

func eventItems(items []events.SimpleItem) []EventItemDTO {
	out := make([]EventItemDTO, 0, len(items))
	for _, item := range items {
		out = append(out, EventItemDTO{Name: item.Name, Value: item.Value})
	}
	return out
}

// Close unsubscribes once. A camera that is gone lets the subscription expire.
func (s *pullPointSubscription) Close(ctx context.Context) error {
	var err error
	s.closeOnce.Do(func() {
		closeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		err = s.session.invokeWithAuth(closeCtx, func(ctx context.Context, header []byte) error {
			return s.client.Unsubscribe(ctx, s.subscription, eventsOptions(header)...)
		})
		if err != nil && !errors.Is(err, context.Canceled) {
			err = mapConfigError(err)
		}
	})
	return err
}

// SubscribeEvents opens a PullPoint subscription on a stored device. The
// subscription keeps the device credentials until it is closed.
func (m *Manager) SubscribeEvents(ctx context.Context, deviceID, topicFilter string) (EventSubscription, error) {
	return withDevice(ctx, m, deviceID, m.events != nil, "events", func(host string, credentials *Credentials) (EventSubscription, error) {
		return m.events.SubscribeEvents(ctx, EventsRequest{DeviceRequest: newDeviceRequest(host, credentials), TopicFilter: topicFilter})
	})
}
