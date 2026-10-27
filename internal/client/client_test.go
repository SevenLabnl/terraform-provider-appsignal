package client

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type request struct {
	Token     string
	Query     string
	Variables map[string]any
}

// fake answers every request with reply and records what it was sent.
func fake(t *testing.T, status int, reply string) (*Client, *[]request) {
	t.Helper()
	var seen []request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body request
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("bad request body: %v", err)
		}
		body.Token = r.URL.Query().Get("token")
		seen = append(seen, body)
		w.WriteHeader(status)
		_, _ = w.Write([]byte(reply))
	}))
	t.Cleanup(srv.Close)
	c := New(srv.URL, "secret-token", "test")
	c.retryWait = 0
	return c, &seen
}

func TestTokenGoesInTheQueryString(t *testing.T) {
	c, seen := fake(t, 200, `{"data":{"app":{"id":"a1","name":"n","environment":"e"}}}`)
	if _, err := c.GetApp(context.Background(), "a1"); err != nil {
		t.Fatal(err)
	}
	if (*seen)[0].Token != "secret-token" {
		t.Fatalf("token = %q", (*seen)[0].Token)
	}
}

func TestNotFoundIsRecognised(t *testing.T) {
	// The API answers a missing object with HTTP 400, an error and a null field.
	c, _ := fake(t, 400, `{"errors":[{"message":"Object not found.","path":["app"]}],"data":{"app":null}}`)
	_, err := c.GetApp(context.Background(), "gone")
	if !IsNotFound(err) {
		t.Fatalf("want not found, got %v", err)
	}
}

func TestAGraphQLErrorWithA200IsAnError(t *testing.T) {
	c, _ := fake(t, 200, `{"errors":[{"message":"Url is invalid"}],"data":{"createUptimeMonitor":null}}`)
	_, err := c.CreateUptimeMonitor(context.Background(), "a1", UptimeMonitor{Name: "x", URL: "nope"})
	var e *Error
	if !errors.As(err, &e) || !strings.Contains(err.Error(), "Url is invalid") || IsNotFound(err) {
		t.Fatalf("got %v", err)
	}
}

func TestANonJSONResponseIsReportedWithItsStatus(t *testing.T) {
	c, _ := fake(t, 502, `<html>bad gateway</html>`)
	_, err := c.GetApp(context.Background(), "a1")
	if err == nil || !strings.Contains(err.Error(), "HTTP 502") {
		t.Fatalf("got %v", err)
	}
}

func TestTheTokenNeverAppearsInATransportError(t *testing.T) {
	c := New("http://127.0.0.1:1/graphql", "secret-token", "test")
	_, err := c.GetApp(context.Background(), "a1")
	if err == nil || strings.Contains(err.Error(), "secret-token") {
		t.Fatalf("got %v", err)
	}
}

func TestRateLimitedRequestsAreRetried(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write([]byte(`{"data":{"app":{"id":"a1"}}}`))
	}))
	defer srv.Close()
	c := New(srv.URL, "t", "test")
	c.retryWait = 0
	if _, err := c.GetApp(context.Background(), "a1"); err != nil || calls != 2 {
		t.Fatalf("err=%v calls=%d", err, calls)
	}
}

func TestMonitorInputHasNoIDAndSendsEmptyListsToClear(t *testing.T) {
	c, seen := fake(t, 200, `{"data":{"updateUptimeMonitor":{"id":"m1"}}}`)
	_, err := c.UpdateUptimeMonitor(context.Background(), "a1", UptimeMonitor{ID: "m1", Name: "n", URL: "https://x/", Regions: []string{"EUROPE"}})
	if err != nil {
		t.Fatal(err)
	}
	vars := (*seen)[0].Variables
	m := vars["m"].(map[string]any)
	if _, ok := m["id"]; ok || vars["id"] != "m1" {
		t.Fatalf("id must be an argument, not part of the input: %v", vars)
	}
	if n, ok := m["notifierIds"].([]any); !ok || len(n) != 0 {
		t.Fatalf("notifierIds = %#v, want []", m["notifierIds"])
	}
	if h, ok := m["headers"].([]any); !ok || len(h) != 0 {
		t.Fatalf("headers = %#v, want []", m["headers"])
	}
}
