package client

import (
	"context"
	"errors"
)

const (
	ActionFilter  = "LogLineActionFilter"
	ActionMetrics = "LogLineActionMetrics"
	ActionTrigger = "LogLineActionTrigger"
)

// LogMetric tags are free JSON in the API; values made in the UI need not be strings.
type LogMetric struct {
	Name       string         `json:"name"`
	Field      *string        `json:"field"`
	MetricType string         `json:"metricType"`
	Tags       map[string]any `json:"tags"`
}

// LogAction is a filter, metrics or trigger action; Type is the GraphQL type name.
type LogAction struct {
	Type      string      `json:"__typename"`
	ID        string      `json:"id"`
	Name      string      `json:"name"`
	Query     string      `json:"query"`
	SourceIDs []string    `json:"sourceIds"`
	DropLines bool        `json:"dropLogLineAfterMetricExtraction"`
	Metrics   []LogMetric `json:"logLineMetrics"`
}

const logActionFields = `__typename
	... on LogLineActionFilter { id name query sourceIds }
	... on LogLineActionMetrics { id name query sourceIds dropLogLineAfterMetricExtraction
		logLineMetrics { name field metricType tags } }
	... on LogLineActionTrigger { id name query sourceIds }`

// ListLogActions returns every log line action of the app in execution order.
func (c *Client) ListLogActions(ctx context.Context, appID string) ([]LogAction, error) {
	var out struct {
		App *struct {
			Logs struct{ LogLineActions []LogAction }
		}
	}
	if err := c.Do(ctx, `query($app: String!) { app(id: $app) { logs { logLineActions { `+logActionFields+` } } } }`,
		map[string]any{"app": appID}, &out); err != nil {
		return nil, err
	}
	if out.App == nil {
		return nil, &Error{Messages: []string{"Object not found."}}
	}
	return out.App.Logs.LogLineActions, nil
}

func (c *Client) GetLogAction(ctx context.Context, appID, id string) (*LogAction, error) {
	all, err := c.ListLogActions(ctx, appID)
	if err != nil {
		return nil, err
	}
	for i := range all {
		if all[i].ID == id {
			return &all[i], nil
		}
	}
	return nil, &Error{Messages: []string{"Object not found."}}
}

// logActionVars sends every field: updateLogLineAction keeps a null name, query, source
// list or metric list, and turns a null drop flag off.
func logActionVars(appID string, a LogAction) map[string]any {
	sources := a.SourceIDs
	if sources == nil {
		sources = []string{}
	}
	vars := map[string]any{"app": appID, "name": a.Name, "query": a.Query, "sourceIds": sources, "metrics": nil, "drop": nil}
	if a.Type == ActionMetrics {
		metrics := make([]LogMetric, 0, len(a.Metrics))
		for _, m := range a.Metrics {
			if m.Tags == nil {
				m.Tags = map[string]any{}
			}
			metrics = append(metrics, m)
		}
		vars["metrics"] = metrics
		vars["drop"] = a.DropLines
	}
	return vars
}

func (c *Client) CreateLogAction(ctx context.Context, appID string, a LogAction) (*LogAction, error) {
	vars := logActionVars(appID, a)
	vars["type"] = "FILTER"
	if a.Type == ActionMetrics {
		vars["type"] = "METRICS"
	}
	var out struct{ CreateLogLineAction *LogAction }
	err := c.Do(ctx, `mutation($app: String!, $name: String!, $query: String!, $type: LogLineActionTypeEnum!,
		$sourceIds: [String!], $metrics: [LogLineMetricInput!], $drop: Boolean) {
		createLogLineAction(appId: $app, name: $name, query: $query, actionType: $type, sourceIds: $sourceIds,
			logLineMetrics: $metrics, dropLogLineAfterMetricExtraction: $drop) { `+logActionFields+` } }`, vars, &out)
	if err != nil {
		return nil, err
	}
	if out.CreateLogLineAction == nil {
		return nil, errors.New("appsignal: createLogLineAction returned no action")
	}
	return out.CreateLogLineAction, nil
}

func (c *Client) UpdateLogAction(ctx context.Context, appID string, a LogAction) (*LogAction, error) {
	vars := logActionVars(appID, a)
	vars["id"] = a.ID
	var out struct{ UpdateLogLineAction *LogAction }
	err := c.Do(ctx, `mutation($app: String!, $id: String!, $name: String, $query: String, $sourceIds: [String!],
		$metrics: [LogLineMetricInput!], $drop: Boolean) {
		updateLogLineAction(appId: $app, id: $id, name: $name, query: $query, sourceIds: $sourceIds,
			logLineMetrics: $metrics, dropLogLineAfterMetricExtraction: $drop) { `+logActionFields+` } }`, vars, &out)
	if err != nil {
		return nil, err
	}
	if out.UpdateLogLineAction == nil {
		return nil, errors.New("appsignal: updateLogLineAction returned no action")
	}
	return out.UpdateLogLineAction, nil
}

func (c *Client) DeleteLogAction(ctx context.Context, appID, id string) error {
	return c.Do(ctx, `mutation($app: String!, $id: String!) { deleteLogLineAction(appId: $app, id: $id) { __typename } }`,
		map[string]any{"app": appID, "id": id}, nil)
}

// DropFilterSuffix names the filter AppSignal adds after a metrics action that drops its lines.
const DropFilterSuffix = " (drop after metric extraction)"

// IsDropFilter reports whether actions[i] is the filter AppSignal manages for the
// metrics action just before it.
func IsDropFilter(actions []LogAction, i int) bool {
	if i == 0 || actions[i].Type != ActionFilter {
		return false
	}
	prev := actions[i-1]
	return prev.Type == ActionMetrics && prev.DropLines && actions[i].Name == prev.Name+DropFilterSuffix
}

// ReorderLogActions puts the given actions first, in that order. Each metrics action keeps
// its drop filter right behind it, and AppSignal appends every action left out.
func (c *Client) ReorderLogActions(ctx context.Context, appID string, ids []string) error {
	all, err := c.ListLogActions(ctx, appID)
	if err != nil {
		return err
	}
	dropFilter := map[string]string{}
	for i := range all {
		if IsDropFilter(all, i) {
			dropFilter[all[i-1].ID] = all[i].ID
		}
	}
	order := make([]string, 0, len(ids)*2)
	for _, id := range ids {
		order = append(order, id)
		if f, ok := dropFilter[id]; ok {
			order = append(order, f)
		}
	}
	return c.Do(ctx, `mutation($app: String!, $ids: [String]!) { reorderLogLineActions(appId: $app, actionIds: $ids) { __typename } }`,
		map[string]any{"app": appID, "ids": order}, nil)
}
