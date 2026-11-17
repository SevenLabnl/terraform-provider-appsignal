package client

import (
	"context"
	"errors"
)

// LogView is a saved view in the log viewer: a filter on sources and a query.
type LogView struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	SourceIDs  []string `json:"sourceIds"`
	Query      string   `json:"query"`
	Columns    []string `json:"columns"`
	LineHeight string   `json:"lineHeight"`
}

const logViewFields = `id name sourceIds query columns lineHeight`

func (c *Client) GetLogView(ctx context.Context, appID, id string) (*LogView, error) {
	var out struct {
		App *struct{ LogView *LogView }
	}
	if err := c.Do(ctx, `query($app: String!, $id: String!) { app(id: $app) { logView(id: $id) { `+logViewFields+` } } }`,
		map[string]any{"app": appID, "id": id}, &out); err != nil {
		return nil, err
	}
	if out.App == nil || out.App.LogView == nil {
		return nil, &Error{Messages: []string{"Object not found."}}
	}
	return out.App.LogView, nil
}

// vars sends every field: an update keeps any argument it leaves out.
func (v LogView) vars(appID string) map[string]any {
	sources, columns := v.SourceIDs, v.Columns
	if sources == nil {
		sources = []string{}
	}
	if columns == nil {
		columns = []string{}
	}
	return map[string]any{"app": appID, "id": v.ID, "name": v.Name, "sourceIds": sources,
		"query": v.Query, "columns": columns, "lineHeight": v.LineHeight}
}

func (c *Client) CreateLogView(ctx context.Context, appID string, v LogView) (*LogView, error) {
	vars := v.vars(appID)
	delete(vars, "id")
	var out struct{ CreateLogView *LogView }
	err := c.Do(ctx, `mutation($app: String!, $name: String!, $sourceIds: [String!], $query: String, $columns: [String!], $lineHeight: String) {
		createLogView(appId: $app, name: $name, sourceIds: $sourceIds, query: $query, columns: $columns, lineHeight: $lineHeight) { `+logViewFields+` } }`,
		vars, &out)
	if err != nil {
		return nil, err
	}
	if out.CreateLogView == nil {
		return nil, errors.New("appsignal: createLogView returned no view")
	}
	return out.CreateLogView, nil
}

func (c *Client) UpdateLogView(ctx context.Context, appID string, v LogView) (*LogView, error) {
	var out struct{ UpdateLogView *LogView }
	err := c.Do(ctx, `mutation($app: String!, $id: String!, $name: String!, $sourceIds: [String!], $query: String, $columns: [String!], $lineHeight: String) {
		updateLogView(appId: $app, id: $id, name: $name, sourceIds: $sourceIds, query: $query, columns: $columns, lineHeight: $lineHeight) { `+logViewFields+` } }`,
		v.vars(appID), &out)
	if err != nil {
		return nil, err
	}
	if out.UpdateLogView == nil {
		return nil, errors.New("appsignal: updateLogView returned no view")
	}
	return out.UpdateLogView, nil
}

func (c *Client) DeleteLogView(ctx context.Context, appID, id string) error {
	return c.Do(ctx, `mutation($app: String!, $id: String!) { deleteLogView(appId: $app, id: $id) { id } }`,
		map[string]any{"app": appID, "id": id}, nil)
}
