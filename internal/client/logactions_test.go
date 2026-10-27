package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestReorderKeepsDropFiltersBehindTheirMetrics(t *testing.T) {
	list := `{"data":{"app":{"logs":{"logLineActions":[
		{"__typename":"LogLineActionFilter","id":"f","name":"filter"},
		{"__typename":"LogLineActionMetrics","id":"m","name":"jobs","dropLogLineAfterMetricExtraction":true},
		{"__typename":"LogLineActionFilter","id":"d","name":"jobs (drop after metric extraction)"},
		{"__typename":"LogLineActionMetrics","id":"k","name":"keep","dropLogLineAfterMetricExtraction":false},
		{"__typename":"LogLineActionFilter","id":"x","name":"keep (drop after metric extraction)"},
		{"__typename":"LogLineActionTrigger","id":"t","name":"trigger"}]}}}}`
	var sent []any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Query     string
			Variables map[string]any
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if strings.Contains(body.Query, "reorderLogLineActions") {
			sent = body.Variables["ids"].([]any)
			_, _ = w.Write([]byte(`{"data":{"reorderLogLineActions":[]}}`))
			return
		}
		_, _ = w.Write([]byte(list))
	}))
	defer srv.Close()

	if err := New(srv.URL, "t", "test").ReorderLogActions(context.Background(), "a1", []string{"t", "m", "k", "f"}); err != nil {
		t.Fatal(err)
	}
	// "x" only looks like a drop filter: its metrics action does not drop lines.
	want := []any{"t", "m", "d", "k", "f"}
	if !reflect.DeepEqual(sent, want) {
		t.Fatalf("sent %v, want %v", sent, want)
	}
}

func TestDropFilterNeedsItsMetricsActionRightBefore(t *testing.T) {
	actions := []LogAction{
		{Type: ActionFilter, Name: "jobs (drop after metric extraction)"},
		{Type: ActionMetrics, Name: "jobs", DropLines: true},
		{Type: ActionTrigger, Name: "t"},
		{Type: ActionFilter, Name: "jobs (drop after metric extraction)"},
		{Type: ActionMetrics, Name: "jobs", DropLines: true},
		{Type: ActionFilter, Name: "jobs (drop after metric extraction)"},
	}
	for i, want := range []bool{false, false, false, false, false, true} {
		if got := IsDropFilter(actions, i); got != want {
			t.Errorf("IsDropFilter(%d) = %v, want %v", i, got, want)
		}
	}
}

func TestLogMetricsActionSendsEveryField(t *testing.T) {
	c, seen := fake(t, 200, `{"data":{"updateLogLineAction":{"__typename":"LogLineActionMetrics","id":"m"}}}`)
	_, err := c.UpdateLogAction(context.Background(), "a1", LogAction{Type: ActionMetrics, ID: "m", Name: "n", Query: "q",
		Metrics: []LogMetric{{Name: "c", MetricType: "COUNTER"}}})
	if err != nil {
		t.Fatal(err)
	}
	vars := (*seen)[0].Variables
	if vars["drop"] != false {
		t.Fatalf("drop = %#v, want false", vars["drop"])
	}
	if s, ok := vars["sourceIds"].([]any); !ok || len(s) != 0 {
		t.Fatalf("sourceIds = %#v, want []", vars["sourceIds"])
	}
	m := vars["metrics"].([]any)[0].(map[string]any)
	if tags, ok := m["tags"].(map[string]any); !ok || len(tags) != 0 {
		t.Fatalf("tags = %#v, want {}", m["tags"])
	}
}
