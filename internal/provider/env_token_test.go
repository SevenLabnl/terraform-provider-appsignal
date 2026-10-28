package provider

import "testing"

func TestEnvTokenPrefersAPIToken(t *testing.T) {
	for _, c := range []struct{ api, old, want string }{
		{"new", "old", "new"},
		{"", "old", "old"},
		{"new", "", "new"},
		{"", "", ""},
	} {
		t.Setenv("APPSIGNAL_API_TOKEN", c.api)
		t.Setenv("APPSIGNAL_TOKEN", c.old)
		if got := envToken(); got != c.want {
			t.Errorf("API=%q TOKEN=%q: got %q, want %q", c.api, c.old, got, c.want)
		}
	}
}
