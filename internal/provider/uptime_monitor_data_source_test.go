package provider

import (
	"strings"
	"testing"

	"github.com/SevenLabnl/terraform-provider-appsignal/internal/client"
)

func TestPickMonitor(t *testing.T) {
	all := []client.UptimeMonitor{
		{ID: "a", Name: "web", URL: "https://example.com/"},
		{ID: "b", Name: "api", URL: "https://example.com/"},
		{ID: "c", Name: "api", URL: "https://api.example.com/"},
	}
	cases := []struct {
		field, want, id, err string
	}{
		{"name", "web", "a", ""},
		{"url", "https://api.example.com/", "c", ""},
		{"name", "api", "", `2 uptime monitors have name "api" (b, c)`},
		{"url", "https://example.com/", "", `2 uptime monitors have url`},
		{"name", "Web", "", `no uptime monitor with name "Web". It has: "web", "api", "api"`},
	}
	for _, c := range cases {
		got, err := pickMonitor(all, c.field, c.want)
		if c.err != "" {
			if err == nil || !strings.Contains(err.Error(), c.err) {
				t.Errorf("%s=%q: error %v, want %q", c.field, c.want, err, c.err)
			}
			continue
		}
		if err != nil || got.ID != c.id {
			t.Errorf("%s=%q: got %v, %v; want %s", c.field, c.want, got, err, c.id)
		}
	}
	if _, err := pickMonitor(nil, "name", "web"); err == nil || !strings.Contains(err.Error(), "has no uptime monitors") {
		t.Errorf("empty app: %v", err)
	}
}
