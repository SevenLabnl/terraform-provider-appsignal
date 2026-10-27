package client

import (
	"context"
	"errors"
)

type ThresholdCondition struct {
	ComparisonOperator string  `json:"comparisonOperator"`
	Value              float64 `json:"value"`
}

// Trigger is a metric trigger. AppSignal never changes one in place: a new version
// replaces it and the old one is archived.
type Trigger struct {
	ID                 string             `json:"id"`
	Kind               string             `json:"kind"`
	Name               string             `json:"name"`
	Description        *string            `json:"description"`
	MetricName         string             `json:"metricName"`
	Field              string             `json:"field"`
	Tags               []KeyValue         `json:"tags"`
	ThresholdCondition ThresholdCondition `json:"thresholdCondition"`
	WarmupDuration     int64              `json:"warmupDuration"`
	CooldownDuration   int64              `json:"cooldownDuration"`
	NoMatchIsZero      bool               `json:"noMatchIsZero"`
	Format             *string            `json:"format"`
	FormatInput        *string            `json:"formatInput"`
	DashboardID        *string            `json:"dashboardId"`
	Notifiers          []Notifier         `json:"notifiers"`
}

// NotifierIDs is only sent, never read: the API returns notifiers as objects.
type TriggerInput struct {
	Trigger
	NotifierIDs []string
}

const triggerFields = `id kind name description metricName field tags { key value }
	thresholdCondition { comparisonOperator value } warmupDuration cooldownDuration noMatchIsZero
	format formatInput dashboardId notifiers { id name icon }`

// GetTrigger returns an active trigger. app.trigger(id) also returns archived versions,
// so the trigger is looked up in the list of active ones instead.
func (c *Client) GetTrigger(ctx context.Context, appID, id string) (*Trigger, error) {
	var out struct {
		App *struct{ Triggers []Trigger }
	}
	if err := c.Do(ctx, `query($app: String!) { app(id: $app) { triggers { `+triggerFields+` } } }`,
		map[string]any{"app": appID}, &out); err != nil {
		return nil, err
	}
	if out.App == nil {
		return nil, &Error{Messages: []string{"Object not found."}}
	}
	for i := range out.App.Triggers {
		if out.App.Triggers[i].ID == id {
			return &out.App.Triggers[i], nil
		}
	}
	return nil, &Error{Messages: []string{"Object not found."}}
}

// CreateTrigger creates a trigger. With previousID set, it replaces that trigger,
// which AppSignal then archives.
func (c *Client) CreateTrigger(ctx context.Context, appID, previousID string, t TriggerInput) (*Trigger, error) {
	tags := t.Tags
	if tags == nil {
		tags = []KeyValue{}
	}
	notifiers := t.NotifierIDs
	if notifiers == nil {
		notifiers = []string{}
	}
	vars := map[string]any{
		"app": appID, "kind": t.Kind, "name": t.Name, "description": t.Description,
		"metricName": t.MetricName, "field": t.Field, "tags": tags,
		"condition": t.ThresholdCondition, "warmup": t.WarmupDuration, "cooldown": t.CooldownDuration,
		"noMatchIsZero": t.NoMatchIsZero, "format": t.Format, "formatInput": t.FormatInput,
		"dashboardId": t.DashboardID, "notifierIds": notifiers, "previous": nil,
	}
	if previousID != "" {
		vars["previous"] = previousID
	}
	var out struct{ CreateTrigger *Trigger }
	err := c.Do(ctx, `mutation($app: String!, $previous: String, $kind: String!, $name: String, $description: String,
		$metricName: String!, $field: MetricFieldEnum!, $tags: [KeyStringValueInput!], $condition: ThresholdConditionInput!,
		$warmup: Int!, $cooldown: Int!, $noMatchIsZero: Boolean, $format: String, $formatInput: String,
		$dashboardId: String, $notifierIds: [String!]) {
		createTrigger(appId: $app, previousTriggerId: $previous, kind: $kind, name: $name, description: $description,
			metricName: $metricName, field: $field, tags: $tags, condition: $condition,
			warmupDuration: $warmup, cooldownDuration: $cooldown, noMatchIsZero: $noMatchIsZero,
			format: $format, formatInput: $formatInput, dashboardId: $dashboardId, notifierIds: $notifierIds) {
			`+triggerFields+` } }`, vars, &out)
	if err != nil {
		return nil, err
	}
	if out.CreateTrigger == nil {
		return nil, errors.New("appsignal: createTrigger returned no trigger")
	}
	return out.CreateTrigger, nil
}

func (c *Client) ArchiveTrigger(ctx context.Context, appID, id string) error {
	return c.Do(ctx, `mutation($app: String!, $id: String!) { archiveTrigger(appId: $app, id: $id) { id } }`,
		map[string]any{"app": appID, "id": id}, nil)
}

type LogTrigger struct {
	ID                       string     `json:"id"`
	Name                     string     `json:"name"`
	Query                    string     `json:"query"`
	Description              *string    `json:"description"`
	SourceIDs                []string   `json:"sourceIds"`
	Notifiers                []Notifier `json:"notifiers"`
	NotificationOptions      string     `json:"notificationOptions"`
	NotificationTriggerValue int64      `json:"notificationTriggerValue"`
}

type LogTriggerInput struct {
	LogTrigger
	NotifierIDs []string
}

const logTriggerFields = `id name query description sourceIds notifiers { id name icon } notificationOptions notificationTriggerValue`

func (c *Client) GetLogTrigger(ctx context.Context, appID, id string) (*LogTrigger, error) {
	var out struct {
		App *struct {
			Logs struct{ Triggers []LogTrigger }
		}
	}
	if err := c.Do(ctx, `query($app: String!) { app(id: $app) { logs { triggers { `+logTriggerFields+` } } } }`,
		map[string]any{"app": appID}, &out); err != nil {
		return nil, err
	}
	if out.App == nil {
		return nil, &Error{Messages: []string{"Object not found."}}
	}
	for i := range out.App.Logs.Triggers {
		if out.App.Logs.Triggers[i].ID == id {
			return &out.App.Logs.Triggers[i], nil
		}
	}
	return nil, &Error{Messages: []string{"Object not found."}}
}

// logTriggerVars sends every field: updateLogTrigger keeps any argument that is left
// out or null, so clearing needs an explicit empty value.
func logTriggerVars(appID string, t LogTriggerInput) map[string]any {
	description := ""
	if t.Description != nil {
		description = *t.Description
	}
	sources := t.SourceIDs
	if sources == nil {
		sources = []string{}
	}
	notifiers := t.NotifierIDs
	if notifiers == nil {
		notifiers = []string{}
	}
	return map[string]any{
		"app": appID, "name": t.Name, "query": t.Query, "description": description,
		"sourceIds": sources, "notifierIds": notifiers,
		"options": t.NotificationOptions, "value": t.NotificationTriggerValue,
	}
}

func (c *Client) CreateLogTrigger(ctx context.Context, appID string, t LogTriggerInput) (*LogTrigger, error) {
	var out struct{ CreateLogTrigger *LogTrigger }
	err := c.Do(ctx, `mutation($app: String!, $name: String!, $query: String!, $description: String,
		$sourceIds: [String!], $notifierIds: [String!], $options: IncidentNotificationFrequencyEnum, $value: Int) {
		createLogTrigger(appId: $app, name: $name, query: $query, description: $description, sourceIds: $sourceIds,
			notifierIds: $notifierIds, notificationOptions: $options, notificationTriggerValue: $value) {
			`+logTriggerFields+` } }`, logTriggerVars(appID, t), &out)
	if err != nil {
		return nil, err
	}
	if out.CreateLogTrigger == nil {
		return nil, errors.New("appsignal: createLogTrigger returned no trigger")
	}
	return out.CreateLogTrigger, nil
}

func (c *Client) UpdateLogTrigger(ctx context.Context, appID string, t LogTriggerInput) (*LogTrigger, error) {
	vars := logTriggerVars(appID, t)
	vars["id"] = t.ID
	var out struct{ UpdateLogTrigger *LogTrigger }
	err := c.Do(ctx, `mutation($app: String!, $id: String!, $name: String!, $query: String!, $description: String,
		$sourceIds: [String!], $notifierIds: [String!], $options: IncidentNotificationFrequencyEnum, $value: Int) {
		updateLogTrigger(appId: $app, id: $id, name: $name, query: $query, description: $description, sourceIds: $sourceIds,
			notifierIds: $notifierIds, notificationOptions: $options, notificationTriggerValue: $value) {
			`+logTriggerFields+` } }`, vars, &out)
	if err != nil {
		return nil, err
	}
	if out.UpdateLogTrigger == nil {
		return nil, errors.New("appsignal: updateLogTrigger returned no trigger")
	}
	return out.UpdateLogTrigger, nil
}

func (c *Client) DeleteLogTrigger(ctx context.Context, appID, id string) error {
	return c.Do(ctx, `mutation($app: String!, $id: String!) { deleteLogTrigger(appId: $app, id: $id) { id } }`,
		map[string]any{"app": appID, "id": id}, nil)
}
