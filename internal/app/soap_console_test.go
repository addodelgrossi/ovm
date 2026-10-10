package app

import (
	"context"
	"strings"
	"testing"

	"github.com/addodelgrossi/ovm/pkg/onvif/soap"
)

func TestSOAPConsoleRecordsRedactedExchangesWhenEnabled(t *testing.T) {
	t.Parallel()
	_, host := encoderFakeHost(t, "", true)
	console := NewSOAPConsole(500)
	services := NewProductionMediaServices(ProductionOptions{Console: console})
	if _, err := services.ReadEncoder(context.Background(), encoderRequest(t, host)); err != nil {
		t.Fatalf("ReadEncoder() error = %v", err)
	}
	if entries := console.Entries(0); len(entries) != 0 {
		t.Fatalf("disabled console recorded %d entries", len(entries))
	}
	console.SetEnabled(true)
	if _, err := services.ReadEncoder(context.Background(), encoderRequest(t, host)); err != nil {
		t.Fatalf("ReadEncoder() error = %v", err)
	}
	entries := console.Entries(0)
	if len(entries) < 4 {
		t.Fatalf("recorded %d entries", len(entries))
	}
	var sawEncoder bool
	for _, entry := range entries {
		all := entry.RequestXML + entry.ResponseXML
		if strings.Contains(all, "encoder-secret") || strings.Contains(all, ">admin<") {
			t.Fatalf("entry %d leaked credentials", entry.ID)
		}
		if strings.HasSuffix(entry.Action, "/GetVideoEncoderConfigurations") && strings.Contains(entry.ResponseXML, "venc_main") {
			sawEncoder = true
		}
	}
	if !sawEncoder {
		t.Fatal("the encoder exchange was not recorded")
	}
	last := entries[len(entries)-1].ID
	if newer := console.Entries(last); len(newer) != 0 {
		t.Fatalf("Entries(after last) = %d", len(newer))
	}
	console.Clear()
	if len(console.Entries(0)) != 0 {
		t.Fatal("Clear() kept entries")
	}
}

func TestSOAPConsoleKeepsOnlyTheNewestEntries(t *testing.T) {
	t.Parallel()
	console := NewSOAPConsole(3)
	console.SetEnabled(true)
	for i := 0; i < 5; i++ {
		console.Record(context.Background(), soap.TraceEvent{Action: "a"})
	}
	entries := console.Entries(0)
	if len(entries) != 3 || entries[0].ID != 3 || entries[2].ID != 5 {
		t.Fatalf("entries = %+v", entries)
	}
	console.SetEnabled(false)
	if len(console.Entries(0)) != 0 {
		t.Fatal("disabling did not clear the console")
	}
}
