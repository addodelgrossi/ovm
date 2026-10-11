// Package events implements a typed client for the ONVIF Events (ver10)
// real-time pull point interface: CreatePullPointSubscription, PullMessages,
// Renew, and Unsubscribe.
//
// Subscription operations are addressed with WS-Addressing To and Action
// headers. Simple-text reference parameters from the subscription reference
// are echoed back as headers after re-serialization, so camera XML is never
// copied into a request verbatim.
//
// Responses are decoded namespace-strictly and bounded: topics, item names, and
// values are length-limited and stripped of control characters. Errors never
// echo response or request values.
//
// Normative sources:
//   - ONVIF Event WSDL (ver10): https://www.onvif.org/ver10/events/wsdl/event.wsdl
//   - OASIS WS-BaseNotification 1.3: http://docs.oasis-open.org/wsn/b-2.xsd
//   - ONVIF Core Specification, section "Event handling".
//
// This package does not import Wails, Cobra, persistence, or UI packages.
package events

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/addodelgrossi/ovm/pkg/onvif/soap"
)

const (
	// Namespace is the ONVIF Events (ver10) WSDL target namespace.
	Namespace = "http://www.onvif.org/ver10/events/wsdl"
	// NotificationNamespace is the WS-BaseNotification namespace.
	NotificationNamespace = "http://docs.oasis-open.org/wsn/b-2"
	// AddressingNamespace is the WS-Addressing 1.0 namespace.
	AddressingNamespace = "http://www.w3.org/2005/08/addressing"
	// SchemaNamespace is the shared ONVIF schema namespace.
	SchemaNamespace = "http://www.onvif.org/ver10/schema"
	// TopicsNamespace is the ONVIF topic namespace, conventionally tns1.
	TopicsNamespace = "http://www.onvif.org/ver10/topics"
	// ConcreteSetDialect is the ONVIF topic expression dialect.
	ConcreteSetDialect = "http://www.onvif.org/ver10/tev/topicExpression/ConcreteSet"

	ActionCreatePullPointSubscription = Namespace + "/EventPortType/CreatePullPointSubscriptionRequest"
	ActionPullMessages                = Namespace + "/PullPointSubscription/PullMessagesRequest"
	ActionRenew                       = "http://docs.oasis-open.org/wsn/bw-2/SubscriptionManager/RenewRequest"
	ActionUnsubscribe                 = "http://docs.oasis-open.org/wsn/bw-2/SubscriptionManager/UnsubscribeRequest"

	// MaxMessageLimit bounds one PullMessages request.
	MaxMessageLimit = 1000
	// MaxTopicFilterLength bounds a topic filter expression.
	MaxTopicFilterLength = 256
	maxTextLength        = 256
	maxItems             = 32
	maxReferenceParams   = 8
)

var (
	// ErrInvalidInput reports a caller value that cannot be sent safely.
	ErrInvalidInput = errors.New("events: invalid request input")
	// ErrInvalidResponse reports a response that is malformed.
	ErrInvalidResponse = errors.New("events: invalid response")
	// ErrNilResponse reports a transport that returned neither a response
	// nor an error.
	ErrNilResponse = errors.New("events: transport returned no response")
)

// Transport is the SOAP seam, satisfied by *soap.Client.
type Transport interface {
	Call(ctx context.Context, endpoint string, request soap.Request) (*soap.Response, error)
}

// RequestOptions carries an optional SOAP header such as WS-Security.
type RequestOptions struct {
	HeaderXML []byte
}

// ReferenceParameter is one simple-text WS-Addressing reference parameter.
type ReferenceParameter struct {
	Namespace string
	Local     string
	Value     string
}

// Subscription is a pull point created by CreatePullPointSubscription.
type Subscription struct {
	// Address is the subscription manager endpoint. Callers validate it
	// before use.
	Address             string
	ReferenceParameters []ReferenceParameter
	CurrentTime         time.Time
	TerminationTime     time.Time
}

// SimpleItem is tt:SimpleItem.
type SimpleItem struct {
	Name  string
	Value string
}

// Message is one notification. Topic has namespace prefixes removed, for
// example "VideoSource/MotionAlarm".
type Message struct {
	Topic             string
	UTCTime           time.Time
	PropertyOperation string
	Source            []SimpleItem
	Data              []SimpleItem
}

// PullResult is a PullMessages response.
type PullResult struct {
	CurrentTime     time.Time
	TerminationTime time.Time
	Messages        []Message
}

// Client issues Events operations. It is safe for concurrent use when its
// Transport is.
type Client struct {
	transport Transport
	endpoint  string
}

// NewClient binds a client to an Events service endpoint.
func NewClient(transport Transport, endpoint string) (*Client, error) {
	if transport == nil {
		return nil, errors.New("events: transport is required")
	}
	if endpoint == "" {
		return nil, errors.New("events: endpoint is required")
	}
	return &Client{transport: transport, endpoint: endpoint}, nil
}

func (c *Client) call(ctx context.Context, endpoint, action, header, body string, opts []RequestOptions) ([]byte, error) {
	var headerXML []byte
	for _, opt := range opts {
		if len(opt.HeaderXML) > 0 {
			headerXML = opt.HeaderXML
		}
	}
	if header != "" {
		headerXML = append(append([]byte(nil), headerXML...), header...)
	}
	response, err := c.transport.Call(ctx, endpoint, soap.Request{Action: action, HeaderXML: headerXML, BodyXML: []byte(body)})
	if err != nil {
		return nil, err
	}
	if response == nil {
		return nil, ErrNilResponse
	}
	return response.BodyXML, nil
}

// addressing builds the WS-Addressing headers of a subscription operation.
func addressing(subscription Subscription, action string) string {
	var builder strings.Builder
	builder.WriteString(`<wsa:Action xmlns:wsa="` + AddressingNamespace + `">` + escape(action) + `</wsa:Action>`)
	builder.WriteString(`<wsa:To xmlns:wsa="` + AddressingNamespace + `">` + escape(subscription.Address) + `</wsa:To>`)
	for _, parameter := range subscription.ReferenceParameters {
		builder.WriteString(`<rp:` + parameter.Local + ` xmlns:rp="` + escape(parameter.Namespace) + `" xmlns:wsa="` +
			AddressingNamespace + `" wsa:IsReferenceParameter="true">` + escape(parameter.Value) + `</rp:` + parameter.Local + `>`)
	}
	return builder.String()
}

// Duration formats d as an xs:duration in whole seconds, such as PT60S.
func Duration(d time.Duration) string {
	return "PT" + strconv.FormatInt(int64(d/time.Second), 10) + "S"
}

// ValidateTopicFilter accepts an empty filter or a ConcreteSet expression of
// tns1-prefixed topic paths joined by "|", such as
// "tns1:VideoSource//.|tns1:Device//.".
func ValidateTopicFilter(filter string) error {
	if filter == "" {
		return nil
	}
	if len(filter) > MaxTopicFilterLength {
		return fmt.Errorf("%w: topic filter", ErrInvalidInput)
	}
	for _, r := range filter {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && !strings.ContainsRune(":/._|*-", r) {
			return fmt.Errorf("%w: topic filter", ErrInvalidInput)
		}
	}
	return nil
}

// CreatePullPointSubscription creates a pull point that terminates after
// termination unless renewed. A non-empty topicFilter is sent as a
// ConcreteSet topic expression with tns1 bound to the ONVIF topic namespace.
func (c *Client) CreatePullPointSubscription(ctx context.Context, topicFilter string, termination time.Duration, opts ...RequestOptions) (Subscription, error) {
	if err := ValidateTopicFilter(topicFilter); err != nil {
		return Subscription{}, err
	}
	if termination < 10*time.Second || termination > time.Hour {
		return Subscription{}, fmt.Errorf("%w: termination", ErrInvalidInput)
	}
	filter := ""
	if topicFilter != "" {
		filter = `<tev:Filter><wsnt:TopicExpression Dialect="` + ConcreteSetDialect + `" xmlns:tns1="` + TopicsNamespace + `">` +
			escape(topicFilter) + `</wsnt:TopicExpression></tev:Filter>`
	}
	body, err := c.call(ctx, c.endpoint, ActionCreatePullPointSubscription, "",
		`<tev:CreatePullPointSubscription xmlns:tev="`+Namespace+`" xmlns:wsnt="`+NotificationNamespace+`">`+filter+
			`<tev:InitialTerminationTime>`+Duration(termination)+`</tev:InitialTerminationTime></tev:CreatePullPointSubscription>`, opts)
	if err != nil {
		return Subscription{}, err
	}
	var response struct {
		XMLName   xml.Name `xml:"http://www.onvif.org/ver10/events/wsdl CreatePullPointSubscriptionResponse"`
		Reference *struct {
			Address    string `xml:"http://www.w3.org/2005/08/addressing Address"`
			Parameters *struct {
				Items []genericElement `xml:",any"`
			} `xml:"http://www.w3.org/2005/08/addressing ReferenceParameters"`
		} `xml:"http://www.onvif.org/ver10/events/wsdl SubscriptionReference"`
		CurrentTime     string `xml:"http://docs.oasis-open.org/wsn/b-2 CurrentTime"`
		TerminationTime string `xml:"http://docs.oasis-open.org/wsn/b-2 TerminationTime"`
	}
	if err := xml.Unmarshal(body, &response); err != nil || response.Reference == nil {
		return Subscription{}, fmt.Errorf("%w: CreatePullPointSubscriptionResponse", ErrInvalidResponse)
	}
	address := strings.TrimSpace(response.Reference.Address)
	if address == "" || len(address) > 2048 || containsControl(address) {
		return Subscription{}, fmt.Errorf("%w: subscription address", ErrInvalidResponse)
	}
	subscription := Subscription{
		Address:         address,
		CurrentTime:     parseTime(response.CurrentTime),
		TerminationTime: parseTime(response.TerminationTime),
	}
	if response.Reference.Parameters != nil {
		for _, item := range response.Reference.Parameters.Items {
			if len(subscription.ReferenceParameters) == maxReferenceParams {
				return Subscription{}, fmt.Errorf("%w: too many reference parameters", ErrInvalidResponse)
			}
			parameter, ok := item.referenceParameter()
			if !ok {
				return Subscription{}, fmt.Errorf("%w: unsupported reference parameter", ErrInvalidResponse)
			}
			subscription.ReferenceParameters = append(subscription.ReferenceParameters, parameter)
		}
	}
	return subscription, nil
}

// PullMessages waits up to timeout for at most limit messages.
func (c *Client) PullMessages(ctx context.Context, subscription Subscription, timeout time.Duration, limit int, opts ...RequestOptions) (PullResult, error) {
	if timeout < time.Second || timeout > time.Minute || limit < 1 || limit > MaxMessageLimit {
		return PullResult{}, fmt.Errorf("%w: pull parameters", ErrInvalidInput)
	}
	body, err := c.call(ctx, subscription.Address, ActionPullMessages, addressing(subscription, ActionPullMessages),
		`<tev:PullMessages xmlns:tev="`+Namespace+`"><tev:Timeout>`+Duration(timeout)+`</tev:Timeout><tev:MessageLimit>`+
			strconv.Itoa(limit)+`</tev:MessageLimit></tev:PullMessages>`, opts)
	if err != nil {
		return PullResult{}, err
	}
	var response struct {
		XMLName         xml.Name           `xml:"http://www.onvif.org/ver10/events/wsdl PullMessagesResponse"`
		CurrentTime     string             `xml:"http://www.onvif.org/ver10/events/wsdl CurrentTime"`
		TerminationTime string             `xml:"http://www.onvif.org/ver10/events/wsdl TerminationTime"`
		Messages        []notificationWire `xml:"http://docs.oasis-open.org/wsn/b-2 NotificationMessage"`
	}
	if err := xml.Unmarshal(body, &response); err != nil {
		return PullResult{}, fmt.Errorf("%w: PullMessagesResponse", ErrInvalidResponse)
	}
	if len(response.Messages) > limit {
		return PullResult{}, fmt.Errorf("%w: more messages than requested", ErrInvalidResponse)
	}
	result := PullResult{
		CurrentTime:     parseTime(response.CurrentTime),
		TerminationTime: parseTime(response.TerminationTime),
		Messages:        make([]Message, 0, len(response.Messages)),
	}
	for _, wire := range response.Messages {
		result.Messages = append(result.Messages, wire.decode())
	}
	return result, nil
}

// Renew extends the subscription by termination and returns the new
// termination time when the camera reports it.
func (c *Client) Renew(ctx context.Context, subscription Subscription, termination time.Duration, opts ...RequestOptions) (time.Time, error) {
	if termination < 10*time.Second || termination > time.Hour {
		return time.Time{}, fmt.Errorf("%w: termination", ErrInvalidInput)
	}
	body, err := c.call(ctx, subscription.Address, ActionRenew, addressing(subscription, ActionRenew),
		`<wsnt:Renew xmlns:wsnt="`+NotificationNamespace+`"><wsnt:TerminationTime>`+Duration(termination)+
			`</wsnt:TerminationTime></wsnt:Renew>`, opts)
	if err != nil {
		return time.Time{}, err
	}
	var response struct {
		XMLName         xml.Name `xml:"http://docs.oasis-open.org/wsn/b-2 RenewResponse"`
		TerminationTime string   `xml:"http://docs.oasis-open.org/wsn/b-2 TerminationTime"`
	}
	if err := xml.Unmarshal(body, &response); err != nil {
		return time.Time{}, fmt.Errorf("%w: RenewResponse", ErrInvalidResponse)
	}
	return parseTime(response.TerminationTime), nil
}

// Unsubscribe deletes the subscription.
func (c *Client) Unsubscribe(ctx context.Context, subscription Subscription, opts ...RequestOptions) error {
	body, err := c.call(ctx, subscription.Address, ActionUnsubscribe, addressing(subscription, ActionUnsubscribe),
		`<wsnt:Unsubscribe xmlns:wsnt="`+NotificationNamespace+`"/>`, opts)
	if err != nil {
		return err
	}
	var response struct {
		XMLName xml.Name `xml:"http://docs.oasis-open.org/wsn/b-2 UnsubscribeResponse"`
	}
	if err := xml.Unmarshal(body, &response); err != nil {
		return fmt.Errorf("%w: UnsubscribeResponse", ErrInvalidResponse)
	}
	return nil
}

type genericElement struct {
	XMLName  xml.Name
	Children []struct {
		XMLName xml.Name
	} `xml:",any"`
	Value string `xml:",chardata"`
}

func (e genericElement) referenceParameter() (ReferenceParameter, bool) {
	value := strings.TrimSpace(e.Value)
	if len(e.Children) > 0 || e.XMLName.Space == "" || !validNCName(e.XMLName.Local) ||
		len(e.XMLName.Space) > 512 || containsControl(e.XMLName.Space) || len(value) > maxTextLength || containsControl(value) {
		return ReferenceParameter{}, false
	}
	return ReferenceParameter{Namespace: e.XMLName.Space, Local: e.XMLName.Local, Value: value}, true
}

type notificationWire struct {
	Topic   string `xml:"http://docs.oasis-open.org/wsn/b-2 Topic"`
	Message struct {
		Inner *struct {
			UtcTime           string    `xml:"UtcTime,attr"`
			PropertyOperation string    `xml:"PropertyOperation,attr"`
			Source            itemsWire `xml:"http://www.onvif.org/ver10/schema Source"`
			Data              itemsWire `xml:"http://www.onvif.org/ver10/schema Data"`
		} `xml:"http://www.onvif.org/ver10/schema Message"`
	} `xml:"http://docs.oasis-open.org/wsn/b-2 Message"`
}

type itemsWire struct {
	Items []struct {
		Name  string `xml:"Name,attr"`
		Value string `xml:"Value,attr"`
	} `xml:"http://www.onvif.org/ver10/schema SimpleItem"`
}

func (w notificationWire) decode() Message {
	message := Message{Topic: normalizeTopic(w.Topic), Source: []SimpleItem{}, Data: []SimpleItem{}}
	if inner := w.Message.Inner; inner != nil {
		message.UTCTime = parseTime(inner.UtcTime)
		message.PropertyOperation = clean(inner.PropertyOperation)
		message.Source = inner.Source.decode()
		message.Data = inner.Data.decode()
	}
	return message
}

func (w itemsWire) decode() []SimpleItem {
	items := make([]SimpleItem, 0, min(len(w.Items), maxItems))
	for _, item := range w.Items {
		if len(items) == maxItems {
			break
		}
		items = append(items, SimpleItem{Name: clean(item.Name), Value: clean(item.Value)})
	}
	return items
}

// normalizeTopic removes namespace prefixes from each path segment.
func normalizeTopic(topic string) string {
	segments := strings.Split(strings.TrimSpace(topic), "/")
	for i, segment := range segments {
		if index := strings.IndexByte(segment, ':'); index >= 0 {
			segments[i] = segment[index+1:]
		}
	}
	return clean(strings.Join(segments, "/"))
}

func parseTime(value string) time.Time {
	parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(value))
	if err != nil {
		return time.Time{}
	}
	return parsed.UTC()
}

// clean drops control characters and bounds the length of untrusted text.
func clean(value string) string {
	value = strings.Map(func(r rune) rune {
		if r < 0x20 || (r >= 0x7f && r <= 0x9f) || r == utf8.RuneError {
			return -1
		}
		return r
	}, strings.TrimSpace(value))
	if len(value) > maxTextLength {
		value = value[:maxTextLength]
		for !utf8.ValidString(value) {
			value = value[:len(value)-1]
		}
	}
	return value
}

func containsControl(value string) bool {
	return strings.ContainsFunc(value, func(r rune) bool { return r < 0x20 || r == 0x7f })
}

func validNCName(name string) bool {
	if name == "" || len(name) > 64 {
		return false
	}
	for i, r := range name {
		letter := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r == '_'
		if !letter && (i == 0 || ((r < '0' || r > '9') && r != '-' && r != '.')) {
			return false
		}
	}
	return true
}

func escape(value string) string {
	var builder strings.Builder
	_ = xml.EscapeText(&builder, []byte(value))
	return builder.String()
}
