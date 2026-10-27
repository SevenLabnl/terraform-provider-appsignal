package client

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

type Dashboard struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	Description *string  `json:"description"`
	Visuals     []Visual `json:"visuals"`
}

type Layout struct {
	X int64 `json:"x"`
	Y int64 `json:"y"`
	W int64 `json:"w"`
	H int64 `json:"h"`
}

type MetricField struct {
	Field string `json:"field"`
}

type TimeseriesMetric struct {
	Name   string        `json:"name"`
	Fields []MetricField `json:"fields"`
	Tags   []KeyValue    `json:"tags"`
}

type NumberMetric struct {
	Name      string     `json:"name"`
	Field     string     `json:"field"`
	Aggregate string     `json:"aggregate"`
	Tags      []KeyValue `json:"tags"`
}

// Visual holds either kind of dashboard visual; Type is the GraphQL type name.
type Visual struct {
	Type           string             `json:"__typename"`
	ID             string             `json:"id"`
	Title          string             `json:"title"`
	Description    *string            `json:"description"`
	Format         *string            `json:"format"`
	FormatInput    *string            `json:"formatInput"`
	Layout         *Layout            `json:"layout"`
	Display        *string            `json:"display"`
	DrawNullAsZero *bool              `json:"drawNullAsZero"`
	LineLabel      *string            `json:"lineLabel"`
	MinYAxis       *float64           `json:"minYAxis"`
	Metrics        []TimeseriesMetric `json:"metrics"`
	Metric         *NumberMetric      `json:"metric"`
}

const (
	TypeTimeseries = "VisualTimeseries"
	TypeNumber     = "VisualNumber"
)

const visualFields = `__typename
	... on VisualTimeseries { id title description format formatInput layout { x y w h } display drawNullAsZero
		lineLabel minYAxis metrics { name fields { field } tags { key value } } }
	... on VisualNumber { id title description format formatInput layout { x y w h }
		metric { name field aggregate tags { key value } } }`

const dashboardFields = `id title description`

func (c *Client) GetDashboard(ctx context.Context, appID, id string) (*Dashboard, error) {
	var out struct {
		App *struct{ Dashboard *Dashboard }
	}
	if err := c.Do(ctx, `query($app: String!, $id: String!) { app(id: $app) { dashboard(id: $id) { `+dashboardFields+` } } }`,
		map[string]any{"app": appID, "id": id}, &out); err != nil {
		return nil, err
	}
	if out.App == nil || out.App.Dashboard == nil {
		return nil, &Error{Messages: []string{"Object not found."}}
	}
	return out.App.Dashboard, nil
}

func (c *Client) CreateDashboard(ctx context.Context, appID string, d Dashboard) (*Dashboard, error) {
	var out struct{ CreateDashboard *Dashboard }
	err := c.Do(ctx, `mutation($app: String!, $title: String!, $description: String) {
		createDashboard(appId: $app, title: $title, description: $description) { `+dashboardFields+` } }`,
		map[string]any{"app": appID, "title": d.Title, "description": d.Description}, &out)
	if err != nil {
		return nil, err
	}
	if out.CreateDashboard == nil {
		return nil, errors.New("appsignal: createDashboard returned no dashboard")
	}
	return out.CreateDashboard, nil
}

// UpdateDashboard always sends the description: the mutation replaces it, and null clears it.
func (c *Client) UpdateDashboard(ctx context.Context, appID string, d Dashboard) (*Dashboard, error) {
	var out struct{ UpdateDashboard *Dashboard }
	err := c.Do(ctx, `mutation($app: String!, $id: String!, $title: String!, $description: String) {
		updateDashboard(appId: $app, id: $id, title: $title, description: $description) { `+dashboardFields+` } }`,
		map[string]any{"app": appID, "id": d.ID, "title": d.Title, "description": d.Description}, &out)
	if err != nil {
		return nil, err
	}
	if out.UpdateDashboard == nil {
		return nil, errors.New("appsignal: updateDashboard returned no dashboard")
	}
	return out.UpdateDashboard, nil
}

func (c *Client) DeleteDashboard(ctx context.Context, appID, id string) error {
	return c.Do(ctx, `mutation($app: String!, $id: String!) { deleteDashboard(appId: $app, id: $id) { id } }`,
		map[string]any{"app": appID, "id": id}, nil)
}

func (c *Client) GetVisual(ctx context.Context, appID, dashboardID, id string) (*Visual, error) {
	var out struct {
		App *struct {
			Dashboard *struct{ Visual *Visual }
		}
	}
	if err := c.Do(ctx, `query($app: String!, $dashboard: String!, $id: String!) {
		app(id: $app) { dashboard(id: $dashboard) { visual(id: $id) { `+visualFields+` } } } }`,
		map[string]any{"app": appID, "dashboard": dashboardID, "id": id}, &out); err != nil {
		return nil, err
	}
	if out.App == nil || out.App.Dashboard == nil || out.App.Dashboard.Visual == nil {
		return nil, &Error{Messages: []string{"Object not found."}}
	}
	return out.App.Dashboard.Visual, nil
}

// visualInput is the VisualTimeseriesInput or VisualNumberInput for v. Every field is
// sent because the update mutations keep what is left out and clear what is null; only
// an unset layout is left out, so a layout arranged in the UI survives.
func visualInput(v Visual) map[string]any {
	in := map[string]any{
		"title": v.Title, "description": v.Description, "format": v.Format, "formatInput": v.FormatInput,
	}
	if v.Layout != nil {
		in["layout"] = v.Layout
	}
	if v.Type == TypeNumber {
		if v.Metric != nil {
			m := *v.Metric
			if m.Tags == nil {
				m.Tags = []KeyValue{}
			}
			in["metric"] = m
		}
		return in
	}
	metrics := make([]TimeseriesMetric, 0, len(v.Metrics))
	for _, m := range v.Metrics {
		if m.Tags == nil {
			m.Tags = []KeyValue{}
		}
		if m.Fields == nil {
			m.Fields = []MetricField{}
		}
		metrics = append(metrics, m)
	}
	in["metrics"] = metrics
	in["display"] = v.Display
	in["drawNullAsZero"] = v.DrawNullAsZero
	in["lineLabel"] = v.LineLabel
	in["minYAxis"] = v.MinYAxis
	return in
}

func visualMutation(v Visual) (create, update string) {
	kind := "Timeseries"
	if v.Type == TypeNumber {
		kind = "Number"
	}
	return "createVisual" + kind, "updateVisual" + kind
}

// The create mutations return the whole dashboard, not the new visual. Creates on one
// dashboard are serialised so the new visual is the one id that was not there before.
var dashboardLocks sync.Map

func lockDashboard(id string) func() {
	m, _ := dashboardLocks.LoadOrStore(id, &sync.Mutex{})
	m.(*sync.Mutex).Lock()
	return m.(*sync.Mutex).Unlock
}

func (c *Client) visualIDs(ctx context.Context, appID, dashboardID string) (map[string]bool, error) {
	var out struct {
		App *struct {
			Dashboard *struct{ Visuals []struct{ ID string } }
		}
	}
	if err := c.Do(ctx, `query($app: String!, $id: String!) { app(id: $app) { dashboard(id: $id) {
		visuals { ... on VisualTimeseries { id } ... on VisualNumber { id } } } } }`,
		map[string]any{"app": appID, "id": dashboardID}, &out); err != nil {
		return nil, err
	}
	if out.App == nil || out.App.Dashboard == nil {
		return nil, &Error{Messages: []string{"Object not found."}}
	}
	ids := map[string]bool{}
	for _, v := range out.App.Dashboard.Visuals {
		ids[v.ID] = true
	}
	return ids, nil
}

// GetDashboardVisualIDs lists the ids of every visual on a dashboard.
func (c *Client) GetDashboardVisualIDs(ctx context.Context, appID, dashboardID string) ([]string, error) {
	ids, err := c.visualIDs(ctx, appID, dashboardID)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(ids))
	for id := range ids {
		out = append(out, id)
	}
	return out, nil
}

func (c *Client) CreateVisual(ctx context.Context, appID, dashboardID string, v Visual) (*Visual, error) {
	unlock := lockDashboard(dashboardID)
	defer unlock()
	before, err := c.visualIDs(ctx, appID, dashboardID)
	if err != nil {
		return nil, err
	}
	create, _ := visualMutation(v)
	inputType := "VisualTimeseriesInput"
	if v.Type == TypeNumber {
		inputType = "VisualNumberInput"
	}
	var out map[string]*struct{ Visuals []Visual }
	err = c.Do(ctx, `mutation($app: String!, $dashboard: String!, $v: `+inputType+`!) {
		`+create+`(appId: $app, dashboardId: $dashboard, visual: $v) { visuals { `+visualFields+` } } }`,
		map[string]any{"app": appID, "dashboard": dashboardID, "v": visualInput(v)}, &out)
	if err != nil {
		return nil, err
	}
	if out[create] == nil {
		return nil, fmt.Errorf("appsignal: %s returned no dashboard", create)
	}
	var added []Visual
	for _, got := range out[create].Visuals {
		if !before[got.ID] {
			added = append(added, got)
		}
	}
	// Someone else may add a visual at the same moment; prefer the one with our title.
	for i := len(added) - 1; i >= 0 && len(added) > 1; i-- {
		if added[i].Title == v.Title && added[i].Type == v.Type {
			return &added[i], nil
		}
	}
	if len(added) == 0 {
		return nil, fmt.Errorf("appsignal: %s did not add a visual", create)
	}
	return &added[len(added)-1], nil
}

func (c *Client) UpdateVisual(ctx context.Context, appID, dashboardID string, v Visual) (*Visual, error) {
	_, update := visualMutation(v)
	inputType := "VisualTimeseriesInput"
	if v.Type == TypeNumber {
		inputType = "VisualNumberInput"
	}
	var out map[string]*struct{ Visual *Visual }
	err := c.Do(ctx, `mutation($app: String!, $id: String!, $v: `+inputType+`!) {
		`+update+`(appId: $app, visualId: $id, visual: $v) { visual(id: $id) { `+visualFields+` } } }`,
		map[string]any{"app": appID, "id": v.ID, "v": visualInput(v)}, &out)
	if err != nil {
		return nil, err
	}
	if out[update] == nil || out[update].Visual == nil {
		return nil, fmt.Errorf("appsignal: %s returned no visual", update)
	}
	return out[update].Visual, nil
}

func (c *Client) DeleteVisual(ctx context.Context, appID, id string) error {
	return c.Do(ctx, `mutation($app: String!, $id: String!) { deleteVisual(appId: $app, id: $id) { id } }`,
		map[string]any{"app": appID, "id": id}, nil)
}
