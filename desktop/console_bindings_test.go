package main

import (
	"context"
	"testing"
	"time"

	core "github.com/addodelgrossi/ovm/internal/app"
	"github.com/addodelgrossi/ovm/pkg/onvif/soap"
)

func TestConsoleBindingsCaptureOnlyWhenEnabled(t *testing.T) {
	console := core.NewSOAPConsole(10)
	host, err := NewApp(AppDependencies{Manager: newFakeManager(), Console: console})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = host.close(context.Background()) }()

	event := soap.TraceEvent{Action: "GetUsers", Duration: time.Millisecond, RequestXML: []byte("<x/>")}
	console.Record(context.Background(), event)
	if got := host.ConsoleEntries(ConsoleEntriesRequest{}); got.Error != nil || len(got.Result.Entries) != 0 || got.Result.Enabled {
		t.Fatalf("disabled console = %+v", got)
	}

	if got := host.SetConsoleEnabled(SetConsoleEnabledRequest{Enabled: true}); got.Error != nil || !got.Result.Enabled {
		t.Fatalf("enable = %+v", got)
	}
	console.Record(context.Background(), event)
	console.Record(context.Background(), event)
	all := host.ConsoleEntries(ConsoleEntriesRequest{})
	if all.Error != nil || len(all.Result.Entries) != 2 {
		t.Fatalf("entries = %+v", all)
	}
	newer := host.ConsoleEntries(ConsoleEntriesRequest{AfterID: all.Result.Entries[0].ID})
	if len(newer.Result.Entries) != 1 || newer.Result.Entries[0].RequestXML != "<x/>" {
		t.Fatalf("newer entries = %+v", newer.Result.Entries)
	}

	if got := host.ClearConsole(); got.Error != nil || len(got.Result.Entries) != 0 || !got.Result.Enabled {
		t.Fatalf("clear = %+v", got)
	}
	if got := host.ConsoleEntries(ConsoleEntriesRequest{}); len(got.Result.Entries) != 0 {
		t.Fatalf("entries after clear = %+v", got.Result.Entries)
	}
}

func TestConsoleBindingsUnavailableWithoutConsole(t *testing.T) {
	host, err := NewApp(AppDependencies{Manager: newFakeManager()})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = host.close(context.Background()) }()
	if got := host.ConsoleEntries(ConsoleEntriesRequest{}); got.Error == nil || got.Error.Code != "console_unavailable" {
		t.Fatalf("ConsoleEntries without console = %+v", got)
	}
}
