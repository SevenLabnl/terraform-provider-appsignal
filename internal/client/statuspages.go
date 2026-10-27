package client

import (
	"context"
	"errors"
)

type StatusPage struct {
	ID               string   `json:"id"`
	Title            string   `json:"title"`
	Description      *string  `json:"description"`
	Hostname         string   `json:"hostname"`
	Threshold        int64    `json:"threshold"`
	UptimeMonitorIDs []string `json:"uptimeMonitorIds"`
}

const statusPageFields = `id title description hostname threshold uptimeMonitorIds`

func (p StatusPage) input(withHostname bool) map[string]any {
	monitors := p.UptimeMonitorIDs
	if monitors == nil {
		monitors = []string{}
	}
	in := map[string]any{"title": p.Title, "description": p.Description, "threshold": p.Threshold, "uptimeMonitorIds": monitors}
	if withHostname {
		in["hostname"] = p.Hostname
	}
	return in
}

func (c *Client) GetStatusPage(ctx context.Context, org, id string) (*StatusPage, error) {
	var out struct {
		Organization *struct{ StatusPage *StatusPage }
	}
	if err := c.Do(ctx, `query($org: String!, $id: String!) { organization(slug: $org) { statusPage(id: $id) { `+statusPageFields+` } } }`,
		map[string]any{"org": org, "id": id}, &out); err != nil {
		return nil, err
	}
	if out.Organization == nil || out.Organization.StatusPage == nil {
		return nil, &Error{Messages: []string{"Object not found."}}
	}
	return out.Organization.StatusPage, nil
}

// CreateStatusPage needs the hostname to have a CNAME to cname.appsignal-status.com already.
func (c *Client) CreateStatusPage(ctx context.Context, org string, p StatusPage) (*StatusPage, error) {
	var out struct{ CreateStatusPage *StatusPage }
	err := c.Do(ctx, `mutation($org: String!, $p: CreateStatusPageInput!) {
		createStatusPage(organizationSlug: $org, statusPage: $p) { `+statusPageFields+` } }`,
		map[string]any{"org": org, "p": p.input(true)}, &out)
	if err != nil {
		return nil, err
	}
	if out.CreateStatusPage == nil {
		return nil, errors.New("appsignal: createStatusPage returned no status page")
	}
	return out.CreateStatusPage, nil
}

func (c *Client) UpdateStatusPage(ctx context.Context, org string, p StatusPage) (*StatusPage, error) {
	var out struct{ UpdateStatusPage *StatusPage }
	err := c.Do(ctx, `mutation($org: String!, $id: String!, $p: UpdateStatusPageInput!) {
		updateStatusPage(organizationSlug: $org, id: $id, statusPage: $p) { `+statusPageFields+` } }`,
		map[string]any{"org": org, "id": p.ID, "p": p.input(false)}, &out)
	if err != nil {
		return nil, err
	}
	if out.UpdateStatusPage == nil {
		return nil, errors.New("appsignal: updateStatusPage returned no status page")
	}
	return out.UpdateStatusPage, nil
}

func (c *Client) DeleteStatusPage(ctx context.Context, org, id string) error {
	return c.Do(ctx, `mutation($org: String!, $id: String!) { deleteStatusPage(organizationSlug: $org, id: $id) { id } }`,
		map[string]any{"org": org, "id": id}, nil)
}
