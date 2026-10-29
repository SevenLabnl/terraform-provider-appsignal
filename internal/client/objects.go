package client

import (
	"context"
	"errors"
)

type Notifier struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Icon string `json:"icon"`
}

type App struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Environment    string `json:"environment"`
	PushAPIKey     string `json:"pushApiKey"`
	FrontendAPIKey string `json:"frontendApiKey"`
	Organization   struct {
		Slug string `json:"slug"`
	} `json:"organization"`
	Notifiers []Notifier `json:"notifiers"`
}

const appFields = `id name environment pushApiKey frontendApiKey organization { slug } notifiers { id name icon }`

func (c *Client) GetApp(ctx context.Context, id string) (*App, error) {
	var out struct{ App *App }
	if err := c.Do(ctx, `query($id: String!) { app(id: $id) { `+appFields+` } }`,
		map[string]any{"id": id}, &out); err != nil {
		return nil, err
	}
	if out.App == nil {
		return nil, &Error{Messages: []string{"Object not found."}}
	}
	return out.App, nil
}

func (c *Client) CreateApp(ctx context.Context, organizationSlug, name, environment string) (*App, error) {
	var out struct{ CreateApp *App }
	err := c.Do(ctx, `mutation($org: String!, $name: String!, $env: String!) {
		createApp(organizationSlug: $org, name: $name, environment: $env) { `+appFields+` } }`,
		map[string]any{"org": organizationSlug, "name": name, "env": environment}, &out)
	if err != nil {
		return nil, err
	}
	if out.CreateApp == nil {
		return nil, errors.New("appsignal: createApp returned no app")
	}
	return out.CreateApp, nil
}

func (c *Client) DeleteApp(ctx context.Context, id string) error {
	return c.Do(ctx, `mutation($id: String!) { deleteApp(appId: $id) { id } }`,
		map[string]any{"id": id}, nil)
}

type Organization struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Slug       string `json:"slug"`
	PushAPIKey string `json:"pushApiKey"`
	Apps       []App  `json:"apps"`
}

func (c *Client) GetOrganization(ctx context.Context, slug string) (*Organization, error) {
	var out struct{ Organization *Organization }
	if err := c.Do(ctx, `query($slug: String!) { organization(slug: $slug) {
		id name slug pushApiKey apps { `+appFields+` } } }`,
		map[string]any{"slug": slug}, &out); err != nil {
		return nil, err
	}
	if out.Organization == nil {
		return nil, &Error{Messages: []string{"Object not found."}}
	}
	return out.Organization, nil
}

type LogSource struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Type          string `json:"type"`
	Format        string `json:"fmt"`
	ParseSeverity bool   `json:"parseSeverity"`
	Key           string `json:"key"`
}

const logSourceFields = `id name type fmt parseSeverity key`

func (c *Client) GetLogSource(ctx context.Context, appID, id string) (*LogSource, error) {
	var out struct {
		App *struct {
			Logs struct{ Source *LogSource }
		}
	}
	if err := c.Do(ctx, `query($app: String!, $id: String!) { app(id: $app) { logs { source(id: $id) { `+logSourceFields+` } } } }`,
		map[string]any{"app": appID, "id": id}, &out); err != nil {
		return nil, err
	}
	if out.App == nil || out.App.Logs.Source == nil {
		return nil, &Error{Messages: []string{"Object not found."}}
	}
	return out.App.Logs.Source, nil
}

func (c *Client) CreateLogSource(ctx context.Context, appID string, s LogSource) (*LogSource, error) {
	var out struct{ CreateLogSource *LogSource }
	err := c.Do(ctx, `mutation($app: String!, $name: String!, $type: String!, $fmt: SourceFormatEnum!, $sev: Boolean) {
		createLogSource(appId: $app, name: $name, type: $type, fmt: $fmt, parseSeverity: $sev) { `+logSourceFields+` } }`,
		map[string]any{"app": appID, "name": s.Name, "type": s.Type, "fmt": s.Format, "sev": s.ParseSeverity}, &out)
	if err != nil {
		return nil, err
	}
	if out.CreateLogSource == nil {
		return nil, errors.New("appsignal: createLogSource returned no source")
	}
	return out.CreateLogSource, nil
}

func (c *Client) UpdateLogSource(ctx context.Context, appID string, s LogSource) (*LogSource, error) {
	var out struct{ UpdateLogSource *LogSource }
	err := c.Do(ctx, `mutation($app: String!, $id: String!, $name: String!, $fmt: SourceFormatEnum!, $sev: Boolean) {
		updateLogSource(appId: $app, id: $id, name: $name, fmt: $fmt, parseSeverity: $sev) { `+logSourceFields+` } }`,
		map[string]any{"app": appID, "id": s.ID, "name": s.Name, "fmt": s.Format, "sev": s.ParseSeverity}, &out)
	if err != nil {
		return nil, err
	}
	if out.UpdateLogSource == nil {
		return nil, errors.New("appsignal: updateLogSource returned no source")
	}
	return out.UpdateLogSource, nil
}

func (c *Client) DeleteLogSource(ctx context.Context, appID, id string) error {
	return c.Do(ctx, `mutation($app: String!, $id: String!) { deleteLogSource(appId: $app, id: $id) { id } }`,
		map[string]any{"app": appID, "id": id}, nil)
}

type KeyValue struct {
	Key   string  `json:"key"`
	Value *string `json:"value"`
}

type UptimeMonitor struct {
	ID                   string     `json:"id,omitempty"`
	Name                 string     `json:"name"`
	URL                  string     `json:"url"`
	Description          *string    `json:"description"`
	NotifierIDs          []string   `json:"notifierIds"`
	WarmupDuration       int64      `json:"warmupDuration"`
	Headers              []KeyValue `json:"headers"`
	CheckBodyContent     *string    `json:"checkBodyContent"`
	MonitorSSLExpiration *bool      `json:"monitorSslExpiration"`
	Regions              []string   `json:"regions"`
}

const uptimeMonitorFields = `id name url description notifierIds warmupDuration headers { key value } checkBodyContent monitorSslExpiration regions`

// input is the monitor as UptimeMonitorInput, which has no id.
func (m UptimeMonitor) input() UptimeMonitor {
	m.ID = ""
	if m.NotifierIDs == nil {
		m.NotifierIDs = []string{}
	}
	if m.Headers == nil {
		m.Headers = []KeyValue{}
	}
	return m
}

func (c *Client) GetUptimeMonitor(ctx context.Context, appID, id string) (*UptimeMonitor, error) {
	var out struct {
		App *struct{ UptimeMonitor *UptimeMonitor }
	}
	if err := c.Do(ctx, `query($app: String!, $id: String!) { app(id: $app) { uptimeMonitor(id: $id) { `+uptimeMonitorFields+` } } }`,
		map[string]any{"app": appID, "id": id}, &out); err != nil {
		return nil, err
	}
	if out.App == nil || out.App.UptimeMonitor == nil {
		return nil, &Error{Messages: []string{"Object not found."}}
	}
	return out.App.UptimeMonitor, nil
}

func (c *Client) CreateUptimeMonitor(ctx context.Context, appID string, m UptimeMonitor) (*UptimeMonitor, error) {
	var out struct{ CreateUptimeMonitor *UptimeMonitor }
	err := c.Do(ctx, `mutation($app: String!, $m: UptimeMonitorInput!) {
		createUptimeMonitor(appId: $app, uptimeMonitor: $m) { `+uptimeMonitorFields+` } }`,
		map[string]any{"app": appID, "m": m.input()}, &out)
	if err != nil {
		return nil, err
	}
	if out.CreateUptimeMonitor == nil {
		return nil, errors.New("appsignal: createUptimeMonitor returned no monitor")
	}
	return out.CreateUptimeMonitor, nil
}

func (c *Client) UpdateUptimeMonitor(ctx context.Context, appID string, m UptimeMonitor) (*UptimeMonitor, error) {
	var out struct{ UpdateUptimeMonitor *UptimeMonitor }
	err := c.Do(ctx, `mutation($app: String!, $id: String!, $m: UptimeMonitorInput!) {
		updateUptimeMonitor(appId: $app, id: $id, uptimeMonitor: $m) { `+uptimeMonitorFields+` } }`,
		map[string]any{"app": appID, "id": m.ID, "m": m.input()}, &out)
	if err != nil {
		return nil, err
	}
	if out.UpdateUptimeMonitor == nil {
		return nil, errors.New("appsignal: updateUptimeMonitor returned no monitor")
	}
	return out.UpdateUptimeMonitor, nil
}

func (c *Client) DeleteUptimeMonitor(ctx context.Context, appID, id string) error {
	return c.Do(ctx, `mutation($app: String!, $id: String!) { deleteUptimeMonitor(appId: $app, id: $id) { id } }`,
		map[string]any{"app": appID, "id": id}, nil)
}

type HostedCollector struct {
	ID           string `json:"id"`
	URL          string `json:"url"`
	Subdomain    string `json:"subdomain"`
	Region       string `json:"region"`
	Status       string `json:"status"`
	IsDefault    bool   `json:"isDefault"`
	Experimental bool   `json:"experimental"`
}

func (c *Client) ListHostedCollectors(ctx context.Context, organizationSlug string) ([]HostedCollector, error) {
	var out struct {
		Organization *struct{ HostedCollectors []HostedCollector }
	}
	if err := c.Do(ctx, `query($slug: String!) { organization(slug: $slug) {
		hostedCollectors { id url subdomain region status isDefault experimental } } }`,
		map[string]any{"slug": organizationSlug}, &out); err != nil {
		return nil, err
	}
	if out.Organization == nil {
		return nil, &Error{Messages: []string{"Object not found."}}
	}
	return out.Organization.HostedCollectors, nil
}
