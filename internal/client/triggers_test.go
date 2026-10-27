package client

import (
	"context"
	"testing"
)

func TestNewTriggerHasNoPreviousVersionAndSendsEmptyLists(t *testing.T) {
	c, seen := fake(t, 200, `{"data":{"createTrigger":{"id":"t1"}}}`)
	_, err := c.CreateTrigger(context.Background(), "a1", "", TriggerInput{Trigger: Trigger{Kind: "Advanced", Name: "n", MetricName: "m", Field: "MEAN"}})
	if err != nil {
		t.Fatal(err)
	}
	vars := (*seen)[0].Variables
	if v, ok := vars["previous"]; !ok || v != nil {
		t.Fatalf("previous = %#v, want null", v)
	}
	for _, k := range []string{"tags", "notifierIds"} {
		if l, ok := vars[k].([]any); !ok || len(l) != 0 {
			t.Fatalf("%s = %#v, want []", k, vars[k])
		}
	}
	if vars["description"] != nil || vars["format"] != nil || vars["formatInput"] != nil || vars["dashboardId"] != nil {
		t.Fatalf("unset optional fields must be null: %v", vars)
	}
}

func TestTriggerUpdateNamesThePreviousVersion(t *testing.T) {
	c, seen := fake(t, 200, `{"data":{"createTrigger":{"id":"t2"}}}`)
	got, err := c.CreateTrigger(context.Background(), "a1", "t1", TriggerInput{Trigger: Trigger{Kind: "Advanced", Name: "n", MetricName: "m", Field: "MEAN"}})
	if err != nil || got.ID != "t2" {
		t.Fatalf("got %+v, err %v", got, err)
	}
	if (*seen)[0].Variables["previous"] != "t1" {
		t.Fatalf("previous = %#v", (*seen)[0].Variables["previous"])
	}
}

func TestArchivedTriggersAreNotFound(t *testing.T) {
	c, _ := fake(t, 200, `{"data":{"app":{"triggers":[{"id":"t2"}]}}}`)
	if _, err := c.GetTrigger(context.Background(), "a1", "t1"); !IsNotFound(err) {
		t.Fatalf("err = %v, want not found", err)
	}
	if got, err := c.GetTrigger(context.Background(), "a1", "t2"); err != nil || got.ID != "t2" {
		t.Fatalf("got %+v, err %v", got, err)
	}
}

func TestLogTriggerUpdateSendsEmptyValuesToClear(t *testing.T) {
	c, seen := fake(t, 200, `{"data":{"updateLogTrigger":{"id":"l1"}}}`)
	_, err := c.UpdateLogTrigger(context.Background(), "a1", LogTriggerInput{LogTrigger: LogTrigger{
		ID: "l1", Name: "n", Query: "q", NotificationOptions: "ALWAYS", NotificationTriggerValue: 1,
	}})
	if err != nil {
		t.Fatal(err)
	}
	vars := (*seen)[0].Variables
	if vars["id"] != "l1" || vars["description"] != "" {
		t.Fatalf("vars = %v", vars)
	}
	for _, k := range []string{"sourceIds", "notifierIds"} {
		if l, ok := vars[k].([]any); !ok || len(l) != 0 {
			t.Fatalf("%s = %#v, want []", k, vars[k])
		}
	}
}

func TestDeletedLogTriggerIsNotFound(t *testing.T) {
	c, _ := fake(t, 200, `{"data":{"app":{"logs":{"triggers":[]}}}}`)
	if _, err := c.GetLogTrigger(context.Background(), "a1", "l1"); !IsNotFound(err) {
		t.Fatalf("err = %v, want not found", err)
	}
}
