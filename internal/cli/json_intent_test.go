package cli

import "testing"

func TestDetectJSONIntentIsOrderIndependentAndValueAware(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		args    []string
		command string
		flags   []string
		want    bool
	}{
		{name: "before invalid flag", args: []string{"discover", "--json", "--unknown"}, command: "discover", flags: []string{"--iface", "--timeout"}, want: true},
		{name: "after invalid flag", args: []string{"discover", "--unknown", "--json"}, command: "discover", flags: []string{"--iface", "--timeout"}, want: true},
		{name: "explicit false wins", args: []string{"discover", "--json", "--json=false"}, command: "discover", want: false},
		{name: "json-like interface value", args: []string{"discover", "--iface", "--json", "--unknown"}, command: "discover", flags: []string{"--iface", "--timeout"}, want: false},
		{name: "after delimiter is positional", args: []string{"info", "--", "--json"}, command: "info", flags: []string{"--user", "-u", "--password-env", "--timeout"}, want: false},
		{name: "different command", args: []string{"version", "--json"}, command: "info", want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			intent := detectJSONIntent(test.args, test.command, test.flags...)
			if !intent.known || intent.requested != test.want {
				t.Fatalf("intent = %+v, want requested=%t", intent, test.want)
			}
		})
	}
}
