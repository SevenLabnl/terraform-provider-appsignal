package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/datasourcevalidator"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/SevenLabnl/terraform-provider-appsignal/internal/client"
)

func NewUptimeMonitorDataSource() datasource.DataSource { return &uptimeMonitorDataSource{} }

type uptimeMonitorDataSource struct{ client *client.Client }

func (d *uptimeMonitorDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_uptime_monitor"
}

func (d *uptimeMonitorDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "An uptime monitor of an app, looked up by id, name or url. Useful for monitors made " +
			"outside this configuration, such as in the AppSignal UI or by an operator. A name or url " +
			"must match exactly one monitor of the app.",
		Attributes: map[string]schema.Attribute{
			"app_id":       schema.StringAttribute{Required: true},
			"id":           schema.StringAttribute{Optional: true, Computed: true},
			"name":         schema.StringAttribute{Optional: true, Computed: true},
			"url":          schema.StringAttribute{Optional: true, Computed: true},
			"description":  schema.StringAttribute{Computed: true},
			"regions":      schema.SetAttribute{ElementType: types.StringType, Computed: true},
			"notifier_ids": schema.SetAttribute{ElementType: types.StringType, Computed: true},
			"warmup_duration": schema.Int64Attribute{
				Description: "Minutes a check must keep failing before the monitor alerts.", Computed: true,
			},
			"headers": schema.MapAttribute{
				Description: "Request headers sent with every check.", ElementType: types.StringType,
				Computed: true, Sensitive: true,
			},
			"check_body_content":     schema.StringAttribute{Computed: true},
			"monitor_ssl_expiration": schema.BoolAttribute{Computed: true},
		},
	}
}

func (d *uptimeMonitorDataSource) ConfigValidators(_ context.Context) []datasource.ConfigValidator {
	return []datasource.ConfigValidator{
		datasourcevalidator.ExactlyOneOf(path.MatchRoot("id"), path.MatchRoot("name"), path.MatchRoot("url")),
	}
}

func (d *uptimeMonitorDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = clientFrom(req.ProviderData, &resp.Diagnostics)
}

func (d *uptimeMonitorDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var m uptimeMonitorModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	app := m.AppID.ValueString()
	var found *client.UptimeMonitor
	if !m.ID.IsNull() {
		u, err := d.client.GetUptimeMonitor(ctx, app, m.ID.ValueString())
		if err != nil {
			resp.Diagnostics.AddError("Reading AppSignal uptime monitor", err.Error())
			return
		}
		found = u
	} else {
		all, err := d.client.ListUptimeMonitors(ctx, app)
		if err != nil {
			resp.Diagnostics.AddError("Listing AppSignal uptime monitors", err.Error())
			return
		}
		field, want := "name", m.Name.ValueString()
		if !m.URL.IsNull() {
			field, want = "url", m.URL.ValueString()
		}
		u, err := pickMonitor(all, field, want)
		if err != nil {
			resp.Diagnostics.AddError("AppSignal uptime monitor not found", fmt.Sprintf("App %s: %s", app, err))
			return
		}
		found = u
	}
	m.fill(found, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

// pickMonitor returns the one monitor whose name or url equals want.
func pickMonitor(all []client.UptimeMonitor, field, want string) (*client.UptimeMonitor, error) {
	var hits []*client.UptimeMonitor
	var seen []string
	for i := range all {
		got := all[i].Name
		if field == "url" {
			got = all[i].URL
		}
		if got == want {
			hits = append(hits, &all[i])
		}
		seen = append(seen, fmt.Sprintf("%q", got))
	}
	switch len(hits) {
	case 1:
		return hits[0], nil
	case 0:
		if len(seen) == 0 {
			return nil, fmt.Errorf("no uptime monitor with %s %q; the app has no uptime monitors", field, want)
		}
		return nil, fmt.Errorf("no uptime monitor with %s %q. It has: %s", field, want, strings.Join(seen, ", "))
	default:
		ids := make([]string, len(hits))
		for i, h := range hits {
			ids[i] = h.ID
		}
		return nil, fmt.Errorf("%d uptime monitors have %s %q (%s); look one up by id",
			len(hits), field, want, strings.Join(ids, ", "))
	}
}
