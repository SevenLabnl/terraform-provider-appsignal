package client

import (
	"context"
	"testing"
)

func TestUpdateLogViewSendsEmptyFields(t *testing.T) {
	c, seen := fake(t, 200, `{"data":{"updateLogView":{"id":"v","name":"n","sourceIds":[],"query":"","columns":[],"lineHeight":"0"}}}`)
	if _, err := c.UpdateLogView(context.Background(), "a1", LogView{ID: "v", Name: "n", LineHeight: "0"}); err != nil {
		t.Fatal(err)
	}
	vars := (*seen)[0].Variables
	for _, k := range []string{"sourceIds", "columns"} {
		if s, ok := vars[k].([]any); !ok || len(s) != 0 {
			t.Fatalf("%s = %#v, want []", k, vars[k])
		}
	}
	if q, ok := vars["query"].(string); !ok || q != "" {
		t.Fatalf("query = %#v, want \"\"", vars["query"])
	}
}

func TestGetLogViewMissingIsNotFound(t *testing.T) {
	c, _ := fake(t, 400, `{"errors":[{"message":"Object not found."}],"data":{"app":{"logView":null}}}`)
	if _, err := c.GetLogView(context.Background(), "a1", "v"); !IsNotFound(err) {
		t.Fatalf("err = %v, want not found", err)
	}
}
