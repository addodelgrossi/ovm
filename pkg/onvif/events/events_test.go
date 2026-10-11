package events_test

import (
	"context"
	"encoding/xml"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/addodelgrossi/ovm/pkg/onvif/events"
	"github.com/addodelgrossi/ovm/pkg/onvif/soap"
)

type exchange struct {
	path string
	body string
}

func serve(t *testing.T, payloads ...string) (*events.Client, *[]exchange) {
	t.Helper()
	var seen []exchange
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		if err := xml.Unmarshal(raw, new(struct{ XMLName xml.Name })); err != nil {
			t.Errorf("request is not well-formed XML: %v\n%s", err, raw)
		}
		seen = append(seen, exchange{path: r.URL.RequestURI(), body: string(raw)})
		payload := payloads[min(len(seen), len(payloads))-1]
		w.Header().Set("Content-Type", "application/soap+xml; charset=utf-8")
		_, _ = io.WriteString(w, `<?xml version="1.0"?><env:Envelope xmlns:env="http://www.w3.org/2003/05/soap-envelope"><env:Body>`+ //nolint:gosec // G705: a test server writing its own SOAP fixture
			strings.ReplaceAll(payload, "{{origin}}", "http://"+r.Host)+`</env:Body></env:Envelope>`)
	}))
	t.Cleanup(server.Close)
	transport, err := soap.NewClient(soap.Config{})
	if err != nil {
		t.Fatal(err)
	}
	client, err := events.NewClient(transport, server.URL+"/onvif/events")
	if err != nil {
		t.Fatal(err)
	}
	return client, &seen
}

const createPayload = `<e:CreatePullPointSubscriptionResponse xmlns:e="http://www.onvif.org/ver10/events/wsdl" ` +
	`xmlns:a="http://www.w3.org/2005/08/addressing" xmlns:n="http://docs.oasis-open.org/wsn/b-2" xmlns:v="urn:vendor">` +
	`<e:SubscriptionReference><a:Address>{{origin}}/onvif/pullpoint?id=7</a:Address>` +
	`<a:ReferenceParameters><v:SubscriptionId>7</v:SubscriptionId></a:ReferenceParameters></e:SubscriptionReference>` +
	`<n:CurrentTime>2026-10-10T12:00:00Z</n:CurrentTime><n:TerminationTime>2026-10-10T12:01:00Z</n:TerminationTime>` +
	`</e:CreatePullPointSubscriptionResponse>`

const pullPayload = `<e:PullMessagesResponse xmlns:e="http://www.onvif.org/ver10/events/wsdl" xmlns:n="http://docs.oasis-open.org/wsn/b-2" ` +
	`xmlns:tt="http://www.onvif.org/ver10/schema" xmlns:tns1="http://www.onvif.org/ver10/topics">` +
	`<e:CurrentTime>2026-10-10T12:00:05Z</e:CurrentTime><e:TerminationTime>2026-10-10T12:01:00Z</e:TerminationTime>` +
	`<n:NotificationMessage><n:Topic Dialect="http://www.onvif.org/ver10/tev/topicExpression/ConcreteSet">tns1:VideoSource/MotionAlarm</n:Topic>` +
	`<n:Message><tt:Message UtcTime="2026-10-10T12:00:04.5Z" PropertyOperation="Changed">` +
	`<tt:Source><tt:SimpleItem Name="Source" Value="vsrc0"/></tt:Source>` +
	`<tt:Data><tt:SimpleItem Name="State" Value="true&#x9;"/></tt:Data></tt:Message></n:Message></n:NotificationMessage>` +
	`</e:PullMessagesResponse>`

func TestPullPointLifecycle(t *testing.T) {
	t.Parallel()
	client, seen := serve(t, createPayload, pullPayload,
		`<n:RenewResponse xmlns:n="http://docs.oasis-open.org/wsn/b-2"><n:TerminationTime>2026-10-10T12:02:00Z</n:TerminationTime></n:RenewResponse>`,
		`<n:UnsubscribeResponse xmlns:n="http://docs.oasis-open.org/wsn/b-2"/>`)
	ctx := context.Background()

	subscription, err := client.CreatePullPointSubscription(ctx, "tns1:VideoSource//.", time.Minute)
	if err != nil {
		t.Fatalf("CreatePullPointSubscription() error = %v", err)
	}
	if !strings.HasSuffix(subscription.Address, "/onvif/pullpoint?id=7") || len(subscription.ReferenceParameters) != 1 ||
		subscription.ReferenceParameters[0] != (events.ReferenceParameter{Namespace: "urn:vendor", Local: "SubscriptionId", Value: "7"}) {
		t.Fatalf("subscription = %+v", subscription)
	}
	if !subscription.TerminationTime.Equal(time.Date(2026, 10, 10, 12, 1, 0, 0, time.UTC)) {
		t.Fatalf("termination = %v", subscription.TerminationTime)
	}
	create := (*seen)[0]
	for _, want := range []string{"<tev:InitialTerminationTime>PT60S<", "tns1:VideoSource//.", `xmlns:tns1="http://www.onvif.org/ver10/topics"`} {
		if !strings.Contains(create.body, want) {
			t.Fatalf("create request lacks %q:\n%s", want, create.body)
		}
	}

	result, err := client.PullMessages(ctx, subscription, 5*time.Second, 10)
	if err != nil {
		t.Fatalf("PullMessages() error = %v", err)
	}
	if len(result.Messages) != 1 {
		t.Fatalf("messages = %+v", result.Messages)
	}
	message := result.Messages[0]
	if message.Topic != "VideoSource/MotionAlarm" || message.PropertyOperation != "Changed" ||
		message.Source[0] != (events.SimpleItem{Name: "Source", Value: "vsrc0"}) ||
		message.Data[0] != (events.SimpleItem{Name: "State", Value: "true"}) ||
		!message.UTCTime.Equal(time.Date(2026, 10, 10, 12, 0, 4, 500_000_000, time.UTC)) {
		t.Fatalf("message = %+v", message)
	}
	pull := (*seen)[1]
	if pull.path != "/onvif/pullpoint?id=7" {
		t.Fatalf("pull path = %q", pull.path)
	}
	for _, want := range []string{"<tev:Timeout>PT5S<", "<tev:MessageLimit>10<", ">" + events.ActionPullMessages + "</wsa:Action>",
		"/onvif/pullpoint?id=7</wsa:To>", `wsa:IsReferenceParameter="true">7</rp:SubscriptionId>`} {
		if !strings.Contains(pull.body, want) {
			t.Fatalf("pull request lacks %q:\n%s", want, pull.body)
		}
	}

	renewed, err := client.Renew(ctx, subscription, time.Minute)
	if err != nil || !renewed.Equal(time.Date(2026, 10, 10, 12, 2, 0, 0, time.UTC)) {
		t.Fatalf("Renew() = %v, %v", renewed, err)
	}
	if err := client.Unsubscribe(ctx, subscription); err != nil {
		t.Fatalf("Unsubscribe() error = %v", err)
	}
	if !strings.Contains((*seen)[3].body, "<wsnt:Unsubscribe") {
		t.Fatalf("unsubscribe body = %s", (*seen)[3].body)
	}
}

func TestCreateWithoutFilterOmitsFilter(t *testing.T) {
	t.Parallel()
	client, seen := serve(t, createPayload)
	if _, err := client.CreatePullPointSubscription(context.Background(), "", time.Minute); err != nil {
		t.Fatal(err)
	}
	if strings.Contains((*seen)[0].body, "Filter") {
		t.Fatalf("request has a filter:\n%s", (*seen)[0].body)
	}
}

func TestRejectsUnsafeInput(t *testing.T) {
	t.Parallel()
	client, seen := serve(t, createPayload)
	ctx := context.Background()
	for _, filter := range []string{"tns1:Video<Source", "a b", strings.Repeat("a", events.MaxTopicFilterLength+1)} {
		if _, err := client.CreatePullPointSubscription(ctx, filter, time.Minute); !errors.Is(err, events.ErrInvalidInput) {
			t.Fatalf("filter %q error = %v", filter, err)
		}
	}
	if _, err := client.CreatePullPointSubscription(ctx, "", time.Second); !errors.Is(err, events.ErrInvalidInput) {
		t.Fatalf("short termination error = %v", err)
	}
	sub := events.Subscription{Address: "http://127.0.0.1:1/x"}
	if _, err := client.PullMessages(ctx, sub, 5*time.Second, 0); !errors.Is(err, events.ErrInvalidInput) {
		t.Fatalf("zero limit error = %v", err)
	}
	if len(*seen) != 0 {
		t.Fatalf("invalid input reached the camera: %d requests", len(*seen))
	}
}

func TestRejectsMalformedResponses(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"no reference": `<e:CreatePullPointSubscriptionResponse xmlns:e="http://www.onvif.org/ver10/events/wsdl"/>`,
		"nested reference parameter": `<e:CreatePullPointSubscriptionResponse xmlns:e="http://www.onvif.org/ver10/events/wsdl" xmlns:a="http://www.w3.org/2005/08/addressing">` +
			`<e:SubscriptionReference><a:Address>http://x/</a:Address><a:ReferenceParameters><v:Id xmlns:v="urn:v"><v:Inner/></v:Id></a:ReferenceParameters>` +
			`</e:SubscriptionReference></e:CreatePullPointSubscriptionResponse>`,
		"wrong namespace": `<e:CreatePullPointSubscriptionResponse xmlns:e="urn:other"><e:SubscriptionReference/></e:CreatePullPointSubscriptionResponse>`,
	}
	for name, payload := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			client, _ := serve(t, payload)
			if _, err := client.CreatePullPointSubscription(context.Background(), "", time.Minute); !errors.Is(err, events.ErrInvalidResponse) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestPullRejectsMoreMessagesThanRequested(t *testing.T) {
	t.Parallel()
	two := strings.Replace(pullPayload, "</e:PullMessagesResponse>",
		pullPayload[strings.Index(pullPayload, "<n:NotificationMessage>"):strings.Index(pullPayload, "</e:PullMessagesResponse>")]+"</e:PullMessagesResponse>", 1)
	client, _ := serve(t, createPayload, two)
	subscription, err := client.CreatePullPointSubscription(context.Background(), "", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.PullMessages(context.Background(), subscription, time.Second, 1); !errors.Is(err, events.ErrInvalidResponse) {
		t.Fatalf("PullMessages() over the limit error = %v", err)
	}
}
