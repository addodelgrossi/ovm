package fakedevice

import (
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	eventsNamespace       = "http://www.onvif.org/ver10/events/wsdl"
	notificationNamespace = "http://docs.oasis-open.org/wsn/b-2"
	addressingNamespace   = "http://www.w3.org/2005/08/addressing"

	// pullPointPath prefixes subscription manager addresses.
	pullPointPath = "/onvif/pullpoint/"
	// maxPullWait caps how long the fake holds a PullMessages request.
	maxPullWait = 10 * time.Second
	// maxQueuedEvents bounds each subscription's undelivered queue.
	maxQueuedEvents = 256
	// maxPullPoints bounds concurrent subscriptions.
	maxPullPoints = 16
)

// Events ver10 and WS-BaseNotification actions modeled by the synthetic device.
const (
	ActionCreatePullPointSubscription = eventsNamespace + "/EventPortType/CreatePullPointSubscriptionRequest"
	ActionPullMessages                = eventsNamespace + "/PullPointSubscription/PullMessagesRequest"
	ActionRenew                       = "http://docs.oasis-open.org/wsn/bw-2/SubscriptionManager/RenewRequest"
	ActionUnsubscribe                 = "http://docs.oasis-open.org/wsn/bw-2/SubscriptionManager/UnsubscribeRequest"
)

// EventItem is one tt:SimpleItem of a published event.
type EventItem struct {
	Name  string
	Value string
}

// Event is a synthetic notification. Topic is a path such as
// "VideoSource/MotionAlarm" and is published in the tns1 namespace.
type Event struct {
	Topic             string
	PropertyOperation string
	Source            []EventItem
	Data              []EventItem
}

type queuedEvent struct {
	event Event
	at    time.Time
}

type pullPoint struct {
	filters    []string
	expires    time.Time
	queue      []queuedEvent
	signal     chan struct{}
	terminated bool
}

type eventBroker struct {
	mu     sync.Mutex
	points map[int]*pullPoint
	nextID int
}

func init() {
	extraOperations[eventsNamespace] = map[string]string{
		"CreatePullPointSubscription": ActionCreatePullPointSubscription,
		"PullMessages":                ActionPullMessages,
	}
	extraOperations[notificationNamespace] = map[string]string{
		"Renew":       ActionRenew,
		"Unsubscribe": ActionUnsubscribe,
	}
	extraRequestHandlers[ActionCreatePullPointSubscription] = (*Device).createPullPoint
	extraRequestHandlers[ActionPullMessages] = (*Device).pullMessages
	extraRequestHandlers[ActionRenew] = (*Device).renewPullPoint
	extraRequestHandlers[ActionUnsubscribe] = (*Device).unsubscribePullPoint
}

// PublishEvent delivers an event to every live subscription whose filter
// matches its topic.
func (d *Device) PublishEvent(event Event) {
	now := d.cfg().Clock()
	d.events.mu.Lock()
	defer d.events.mu.Unlock()
	for _, point := range d.events.points {
		if point.terminated || !topicMatches(point.filters, event.Topic) {
			continue
		}
		point.queue = append(point.queue, queuedEvent{event: event, at: now})
		if overflow := len(point.queue) - maxQueuedEvents; overflow > 0 {
			point.queue = point.queue[overflow:]
		}
		select {
		case point.signal <- struct{}{}:
		default:
		}
	}
}

// ActiveSubscriptions reports the number of live pull points.
func (d *Device) ActiveSubscriptions() int {
	now := d.cfg().Clock()
	d.events.mu.Lock()
	defer d.events.mu.Unlock()
	count := 0
	for _, point := range d.events.points {
		if !point.terminated && now.Before(point.expires) {
			count++
		}
	}
	return count
}

var topicFilterPattern = regexp.MustCompile(`^[A-Za-z0-9_:/.|*-]{1,256}$`)

// parseTopicFilter accepts ConcreteSet alternatives such as
// "tns1:VideoSource//.|tns1:Device/Trigger". Prefixes are ignored.
func parseTopicFilter(expression string) ([]string, bool) {
	expression = strings.TrimSpace(expression)
	if !topicFilterPattern.MatchString(expression) {
		return nil, false
	}
	var filters []string
	for _, alternative := range strings.Split(expression, "|") {
		filters = append(filters, stripTopicPrefixes(alternative))
	}
	return filters, true
}

func stripTopicPrefixes(topic string) string {
	segments := strings.Split(topic, "/")
	for i, segment := range segments {
		if index := strings.IndexByte(segment, ':'); index >= 0 {
			segments[i] = segment[index+1:]
		}
	}
	return strings.Join(segments, "/")
}

func topicMatches(filters []string, topic string) bool {
	if len(filters) == 0 {
		return true
	}
	for _, filter := range filters {
		if prefix, ok := strings.CutSuffix(filter, "//."); ok {
			if topic == prefix || strings.HasPrefix(topic, prefix+"/") {
				return true
			}
			continue
		}
		if filter == topic {
			return true
		}
	}
	return false
}

// parseTermination accepts PTnS durations and absolute UTC times.
func parseTermination(value string, now time.Time) (time.Time, bool) {
	value = strings.TrimSpace(value)
	if seconds, ok := strings.CutPrefix(value, "PT"); ok {
		if seconds, ok = strings.CutSuffix(seconds, "S"); ok {
			n, err := strconv.Atoi(seconds)
			if err == nil && n > 0 && n <= 86400 {
				return now.Add(time.Duration(n) * time.Second), true
			}
		}
		return time.Time{}, false
	}
	at, err := time.Parse(time.RFC3339, value)
	if err != nil || !at.After(now) {
		return time.Time{}, false
	}
	return at, true
}

func (d *Device) createPullPoint(response http.ResponseWriter, request *http.Request, version SOAPVersion, operation operationWire) {
	now := d.cfg().Clock()
	expires := now.Add(time.Minute)
	if value, ok := childText(operation.Elements, eventsNamespace, "InitialTerminationTime"); ok {
		parsed, valid := parseTermination(value, now)
		if !valid {
			d.writeFault(response, version, FaultSpec{Kind: FaultInvalidArgVal, Reason: "UnacceptableInitialTerminationTime"})
			return
		}
		expires = parsed
	}
	var filters []string
	if filter := childElement(operation.Elements, eventsNamespace, "Filter"); filter != nil {
		if expression, ok := childText(filter.Elements, notificationNamespace, "TopicExpression"); ok {
			parsed, valid := parseTopicFilter(expression)
			if !valid {
				d.writeFault(response, version, FaultSpec{Kind: FaultInvalidArgVal, Reason: "InvalidTopicExpressionFault"})
				return
			}
			filters = parsed
		}
	}
	d.events.mu.Lock()
	d.events.prune(now)
	if len(d.events.points) >= maxPullPoints {
		d.events.mu.Unlock()
		d.writeFault(response, version, FaultSpec{Kind: FaultInvalidArgVal, Reason: "MaxNotificationProducersExceeded"})
		return
	}
	if d.events.points == nil {
		d.events.points = make(map[int]*pullPoint)
	}
	d.events.nextID++
	id := d.events.nextID
	d.events.points[id] = &pullPoint{filters: filters, expires: expires, signal: make(chan struct{}, 1)}
	d.events.mu.Unlock()

	address := "http://" + request.Host + pullPointPath + strconv.Itoa(id)
	d.writeSuccess(response, version, `<tev:CreatePullPointSubscriptionResponse xmlns:tev="`+eventsNamespace+`" xmlns:wsa="`+
		addressingNamespace+`" xmlns:wsnt="`+notificationNamespace+`"><tev:SubscriptionReference><wsa:Address>`+escapeXML(address)+
		`</wsa:Address></tev:SubscriptionReference><wsnt:CurrentTime>`+formatEventTime(now)+`</wsnt:CurrentTime><wsnt:TerminationTime>`+
		formatEventTime(expires)+`</wsnt:TerminationTime></tev:CreatePullPointSubscriptionResponse>`)
}

// prune drops expired and terminated pull points. Callers hold mu.
func (b *eventBroker) prune(now time.Time) {
	for id, point := range b.points {
		if point.terminated || !now.Before(point.expires) {
			delete(b.points, id)
		}
	}
}

// pullPointFor resolves the live subscription addressed by the request path.
func (d *Device) pullPointFor(request *http.Request) (*pullPoint, int, bool) {
	raw, ok := strings.CutPrefix(request.URL.Path, pullPointPath)
	if !ok {
		return nil, 0, false
	}
	id, err := strconv.Atoi(raw)
	if err != nil {
		return nil, 0, false
	}
	now := d.cfg().Clock()
	d.events.mu.Lock()
	defer d.events.mu.Unlock()
	point, ok := d.events.points[id]
	if !ok || point.terminated || !now.Before(point.expires) {
		return nil, 0, false
	}
	return point, id, true
}

var resourceUnknownFault = FaultSpec{Kind: FaultInvalidArgVal, Reason: "ResourceUnknownFault: the subscription does not exist"}

func (d *Device) pullMessages(response http.ResponseWriter, request *http.Request, version SOAPVersion, operation operationWire) {
	point, _, ok := d.pullPointFor(request)
	if !ok {
		d.writeFault(response, version, resourceUnknownFault)
		return
	}
	limitText, _ := childText(operation.Elements, eventsNamespace, "MessageLimit")
	limit, err := strconv.Atoi(limitText)
	if err != nil || limit < 1 {
		d.writeFault(response, version, FaultSpec{Kind: FaultInvalidArgVal, Reason: "PullMessagesFaultResponse: invalid MessageLimit"})
		return
	}
	timeoutText, _ := childText(operation.Elements, eventsNamespace, "Timeout")
	wait := maxPullWait
	if deadline, valid := parseTermination(timeoutText, time.Time{}); valid {
		wait = min(deadline.Sub(time.Time{}), maxPullWait)
	}

	timer := time.NewTimer(wait)
	defer timer.Stop()
	for {
		d.events.mu.Lock()
		count := min(len(point.queue), limit)
		batch := append([]queuedEvent(nil), point.queue[:count]...)
		point.queue = point.queue[count:]
		terminated := point.terminated
		d.events.mu.Unlock()
		if count > 0 || terminated {
			d.writePullResponse(response, version, point, batch)
			return
		}
		select {
		case <-point.signal:
		case <-timer.C:
			d.writePullResponse(response, version, point, nil)
			return
		case <-request.Context().Done():
			return
		}
	}
}

func (d *Device) writePullResponse(response http.ResponseWriter, version SOAPVersion, point *pullPoint, batch []queuedEvent) {
	d.events.mu.Lock()
	expires := point.expires
	d.events.mu.Unlock()
	var body strings.Builder
	body.WriteString(`<tev:PullMessagesResponse xmlns:tev="` + eventsNamespace + `" xmlns:wsnt="` + notificationNamespace +
		`" xmlns:tns1="http://www.onvif.org/ver10/topics"><tev:CurrentTime>` + formatEventTime(d.cfg().Clock()) +
		`</tev:CurrentTime><tev:TerminationTime>` + formatEventTime(expires) + `</tev:TerminationTime>`)
	for _, queued := range batch {
		event := queued.event
		segments := strings.Split(event.Topic, "/")
		segments[0] = "tns1:" + segments[0]
		operation := event.PropertyOperation
		if operation == "" {
			operation = "Changed"
		}
		body.WriteString(`<wsnt:NotificationMessage><wsnt:Topic Dialect="http://www.onvif.org/ver10/tev/topicExpression/ConcreteSet">` +
			escapeXML(strings.Join(segments, "/")) + `</wsnt:Topic><wsnt:Message><tt:Message UtcTime="` + formatEventTime(queued.at) +
			`" PropertyOperation="` + escapeXML(operation) + `"><tt:Source>` + simpleItemsXML(event.Source) + `</tt:Source><tt:Data>` +
			simpleItemsXML(event.Data) + `</tt:Data></tt:Message></wsnt:Message></wsnt:NotificationMessage>`)
	}
	body.WriteString(`</tev:PullMessagesResponse>`)
	d.writeSuccess(response, version, body.String())
}

func simpleItemsXML(items []EventItem) string {
	var builder strings.Builder
	for _, item := range items {
		builder.WriteString(`<tt:SimpleItem Name="` + escapeXML(item.Name) + `" Value="` + escapeXML(item.Value) + `"/>`)
	}
	return builder.String()
}

func (d *Device) renewPullPoint(response http.ResponseWriter, request *http.Request, version SOAPVersion, operation operationWire) {
	point, _, ok := d.pullPointFor(request)
	if !ok {
		d.writeFault(response, version, resourceUnknownFault)
		return
	}
	now := d.cfg().Clock()
	value, _ := childText(operation.Elements, notificationNamespace, "TerminationTime")
	expires, valid := parseTermination(value, now)
	if !valid {
		d.writeFault(response, version, FaultSpec{Kind: FaultInvalidArgVal, Reason: "UnacceptableTerminationTimeFault"})
		return
	}
	d.events.mu.Lock()
	point.expires = expires
	d.events.mu.Unlock()
	d.writeSuccess(response, version, `<wsnt:RenewResponse xmlns:wsnt="`+notificationNamespace+`"><wsnt:TerminationTime>`+
		formatEventTime(expires)+`</wsnt:TerminationTime><wsnt:CurrentTime>`+formatEventTime(now)+`</wsnt:CurrentTime></wsnt:RenewResponse>`)
}

func (d *Device) unsubscribePullPoint(response http.ResponseWriter, request *http.Request, version SOAPVersion, _ operationWire) {
	point, id, ok := d.pullPointFor(request)
	if !ok {
		d.writeFault(response, version, resourceUnknownFault)
		return
	}
	d.events.mu.Lock()
	point.terminated = true
	delete(d.events.points, id)
	d.events.mu.Unlock()
	select {
	case point.signal <- struct{}{}:
	default:
	}
	d.writeSuccess(response, version, `<wsnt:UnsubscribeResponse xmlns:wsnt="`+notificationNamespace+`"/>`)
}

func formatEventTime(at time.Time) string {
	return at.UTC().Format("2006-01-02T15:04:05.000Z")
}
