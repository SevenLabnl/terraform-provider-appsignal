package client

import (
	"context"
	"errors"
)

type CheckIn struct {
	ID              string     `json:"id"`
	Identifier      string     `json:"identifier"`
	Kind            string     `json:"kind"`
	Syntax          *string    `json:"syntax"`
	Description     *string    `json:"description"`
	Timezone        string     `json:"timezone"`
	WaitTimeMinutes int64      `json:"waitTimeMinutes"`
	Notifiers       []Notifier `json:"notifiers"`
}

type CheckInInput struct {
	CheckIn
	NotifierIDs []string
}

const checkInFields = `id identifier kind syntax description timezone waitTimeMinutes notifiers { id name icon }`

func (c *Client) GetCheckIn(ctx context.Context, appID, id string) (*CheckIn, error) {
	var out struct {
		App *struct {
			CheckIns struct{ Trigger *CheckIn }
		}
	}
	if err := c.Do(ctx, `query($app: String!, $id: String!) { app(id: $app) { checkIns { trigger(id: $id) { `+checkInFields+` } } } }`,
		map[string]any{"app": appID, "id": id}, &out); err != nil {
		return nil, err
	}
	if out.App == nil || out.App.CheckIns.Trigger == nil {
		return nil, &Error{Messages: []string{"Object not found."}}
	}
	return out.App.CheckIns.Trigger, nil
}

func (c *Client) ListCheckIns(ctx context.Context, appID string) ([]CheckIn, error) {
	var out struct {
		App *struct {
			CheckIns struct{ Triggers []CheckIn }
		}
	}
	if err := c.Do(ctx, `query($app: String!) { app(id: $app) { checkIns { triggers { `+checkInFields+` } } } }`,
		map[string]any{"app": appID}, &out); err != nil {
		return nil, err
	}
	if out.App == nil {
		return nil, &Error{Messages: []string{"Object not found."}}
	}
	return out.App.CheckIns.Triggers, nil
}

// checkInVars sends empty strings and lists to clear: updateCheckInTrigger keeps any
// argument that is null.
func checkInVars(appID string, ci CheckInInput) map[string]any {
	empty := func(s *string) string {
		if s == nil {
			return ""
		}
		return *s
	}
	notifiers := ci.NotifierIDs
	if notifiers == nil {
		notifiers = []string{}
	}
	return map[string]any{
		"app": appID, "identifier": ci.Identifier, "syntax": empty(ci.Syntax), "description": empty(ci.Description),
		"wait": ci.WaitTimeMinutes, "timezone": ci.Timezone, "notifierIds": notifiers,
	}
}

func (c *Client) CreateCheckIn(ctx context.Context, appID string, ci CheckInInput) (*CheckIn, error) {
	vars := checkInVars(appID, ci)
	vars["kind"] = ci.Kind
	var out struct{ CreateCheckInTrigger *CheckIn }
	err := c.Do(ctx, `mutation($app: String!, $identifier: String!, $kind: CheckInTriggerKindEnum!, $syntax: String,
		$description: String, $wait: Int!, $timezone: String, $notifierIds: [String!]) {
		createCheckInTrigger(appId: $app, identifier: $identifier, kind: $kind, syntax: $syntax, description: $description,
			waitTimeMinutes: $wait, timezone: $timezone, notifierIds: $notifierIds) { `+checkInFields+` } }`, vars, &out)
	if err != nil {
		return nil, err
	}
	if out.CreateCheckInTrigger == nil {
		return nil, errors.New("appsignal: createCheckInTrigger returned no check-in")
	}
	return out.CreateCheckInTrigger, nil
}

func (c *Client) UpdateCheckIn(ctx context.Context, appID string, ci CheckInInput) (*CheckIn, error) {
	vars := checkInVars(appID, ci)
	vars["id"] = ci.ID
	var out struct{ UpdateCheckInTrigger *CheckIn }
	err := c.Do(ctx, `mutation($app: String!, $id: String!, $identifier: String, $syntax: String, $description: String,
		$wait: Int, $timezone: String, $notifierIds: [String!]) {
		updateCheckInTrigger(appId: $app, id: $id, identifier: $identifier, syntax: $syntax, description: $description,
			waitTimeMinutes: $wait, timezone: $timezone, notifierIds: $notifierIds) { `+checkInFields+` } }`, vars, &out)
	if err != nil {
		return nil, err
	}
	if out.UpdateCheckInTrigger == nil {
		return nil, errors.New("appsignal: updateCheckInTrigger returned no check-in")
	}
	return out.UpdateCheckInTrigger, nil
}

func (c *Client) DeleteCheckIn(ctx context.Context, appID, id string) error {
	return c.Do(ctx, `mutation($app: String!, $id: String!) { deleteCheckInTrigger(appId: $app, id: $id) { id } }`,
		map[string]any{"app": appID, "id": id}, nil)
}
