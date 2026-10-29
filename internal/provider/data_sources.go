package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/datasourcevalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/SevenLabnl/terraform-provider-appsignal/internal/client"
)

// ---------------------------------------------------------------- organization

func NewOrganizationDataSource() datasource.DataSource { return &organizationDataSource{} }

type organizationDataSource struct{ client *client.Client }

type organizationModel struct {
	Slug       types.String `tfsdk:"slug"`
	ID         types.String `tfsdk:"id"`
	Name       types.String `tfsdk:"name"`
	PushAPIKey types.String `tfsdk:"push_api_key"`
}

func (d *organizationDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_organization"
}

func (d *organizationDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "An AppSignal organization, looked up by slug.",
		Attributes: map[string]schema.Attribute{
			"slug": schema.StringAttribute{Required: true},
			"id":   schema.StringAttribute{Computed: true},
			"name": schema.StringAttribute{Computed: true},
			"push_api_key": schema.StringAttribute{
				Description: "Organization-wide push API key.", Computed: true, Sensitive: true,
			},
		},
	}
}

func (d *organizationDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = clientFrom(req.ProviderData, &resp.Diagnostics)
}

func (d *organizationDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var m organizationModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	org, err := d.client.GetOrganization(ctx, m.Slug.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Reading AppSignal organization", err.Error())
		return
	}
	m.ID = types.StringValue(org.ID)
	m.Name = types.StringValue(org.Name)
	m.PushAPIKey = types.StringValue(org.PushAPIKey)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

// ---------------------------------------------------------------- app

var notifierObject = types.ObjectType{AttrTypes: map[string]attr.Type{
	"id": types.StringType, "name": types.StringType, "icon": types.StringType,
}}

func NewAppDataSource() datasource.DataSource { return &appDataSource{} }

type appDataSource struct{ client *client.Client }

type appDataModel struct {
	ID               types.String `tfsdk:"id"`
	OrganizationSlug types.String `tfsdk:"organization_slug"`
	Name             types.String `tfsdk:"name"`
	Environment      types.String `tfsdk:"environment"`
	PushAPIKey       types.String `tfsdk:"push_api_key"`
	FrontendAPIKey   types.String `tfsdk:"frontend_api_key"`
	Notifiers        types.List   `tfsdk:"notifiers"`
}

func (d *appDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_app"
}

func (d *appDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "An AppSignal app, looked up by id or by organization_slug, name and environment.",
		Attributes: map[string]schema.Attribute{
			"id":                schema.StringAttribute{Optional: true, Computed: true},
			"organization_slug": schema.StringAttribute{Optional: true, Computed: true},
			"name":              schema.StringAttribute{Optional: true, Computed: true},
			"environment":       schema.StringAttribute{Optional: true, Computed: true},
			"push_api_key":      schema.StringAttribute{Computed: true, Sensitive: true},
			"frontend_api_key":  schema.StringAttribute{Computed: true, Sensitive: true},
			"notifiers": schema.ListAttribute{
				Description: "Notifiers configured for the app.", Computed: true, ElementType: notifierObject,
			},
		},
	}
}

func (d *appDataSource) ConfigValidators(_ context.Context) []datasource.ConfigValidator {
	return []datasource.ConfigValidator{
		datasourcevalidator.ExactlyOneOf(path.MatchRoot("id"), path.MatchRoot("organization_slug")),
		datasourcevalidator.RequiredTogether(path.MatchRoot("organization_slug"), path.MatchRoot("name"), path.MatchRoot("environment")),
	}
}

func (d *appDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = clientFrom(req.ProviderData, &resp.Diagnostics)
}

func (d *appDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var m appDataModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var app *client.App
	if !m.ID.IsNull() {
		a, err := d.client.GetApp(ctx, m.ID.ValueString())
		if err != nil {
			resp.Diagnostics.AddError("Reading AppSignal app", err.Error())
			return
		}
		app = a
	} else {
		org, err := d.client.GetOrganization(ctx, m.OrganizationSlug.ValueString())
		if err != nil {
			resp.Diagnostics.AddError("Reading AppSignal organization", err.Error())
			return
		}
		for i := range org.Apps {
			if org.Apps[i].Name == m.Name.ValueString() && org.Apps[i].Environment == m.Environment.ValueString() {
				app = &org.Apps[i]
				break
			}
		}
		if app == nil {
			resp.Diagnostics.AddError("AppSignal app not found", fmt.Sprintf("No app %q in environment %q in organization %q.",
				m.Name.ValueString(), m.Environment.ValueString(), m.OrganizationSlug.ValueString()))
			return
		}
		app.Organization.Slug = org.Slug
	}

	m.ID = types.StringValue(app.ID)
	m.OrganizationSlug = types.StringValue(app.Organization.Slug)
	m.Name = types.StringValue(app.Name)
	m.Environment = types.StringValue(app.Environment)
	m.PushAPIKey = types.StringValue(app.PushAPIKey)
	m.FrontendAPIKey = types.StringValue(app.FrontendAPIKey)
	notifiers := make([]attr.Value, 0, len(app.Notifiers))
	for _, n := range app.Notifiers {
		obj, diags := types.ObjectValue(notifierObject.AttrTypes, map[string]attr.Value{
			"id": types.StringValue(n.ID), "name": types.StringValue(n.Name), "icon": types.StringValue(n.Icon),
		})
		resp.Diagnostics.Append(diags...)
		notifiers = append(notifiers, obj)
	}
	list, diags := types.ListValue(notifierObject, notifiers)
	resp.Diagnostics.Append(diags...)
	m.Notifiers = list
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

// ---------------------------------------------------------------- notifier

func NewNotifierDataSource() datasource.DataSource { return &notifierDataSource{} }

type notifierDataSource struct{ client *client.Client }

type notifierModel struct {
	AppID types.String `tfsdk:"app_id"`
	Name  types.String `tfsdk:"name"`
	ID    types.String `tfsdk:"id"`
	Icon  types.String `tfsdk:"icon"`
}

func (d *notifierDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_notifier"
}

func (d *notifierDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A notifier of an app, looked up by name. The AppSignal API cannot create notifiers; " +
			"make them in the AppSignal UI and refer to them here.",
		Attributes: map[string]schema.Attribute{
			"app_id": schema.StringAttribute{Required: true},
			"name":   schema.StringAttribute{Required: true},
			"id":     schema.StringAttribute{Computed: true},
			"icon":   schema.StringAttribute{Computed: true},
		},
	}
}

func (d *notifierDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = clientFrom(req.ProviderData, &resp.Diagnostics)
}

func (d *notifierDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var m notifierModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	app, err := d.client.GetApp(ctx, m.AppID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Reading AppSignal app", err.Error())
		return
	}
	var names []string
	for _, n := range app.Notifiers {
		if n.Name == m.Name.ValueString() {
			m.ID = types.StringValue(n.ID)
			m.Icon = types.StringValue(n.Icon)
			resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
			return
		}
		names = append(names, n.Name)
	}
	resp.Diagnostics.AddError("AppSignal notifier not found", fmt.Sprintf("App %s has no notifier named %q. It has: %s.",
		m.AppID.ValueString(), m.Name.ValueString(), strings.Join(names, ", ")))
}

// ---------------------------------------------------------------- hosted collector

func NewHostedCollectorDataSource() datasource.DataSource { return &hostedCollectorDataSource{} }

type hostedCollectorDataSource struct{ client *client.Client }

type hostedCollectorModel struct {
	OrganizationSlug types.String `tfsdk:"organization_slug"`
	ID               types.String `tfsdk:"id"`
	URL              types.String `tfsdk:"url"`
	Subdomain        types.String `tfsdk:"subdomain"`
	Region           types.String `tfsdk:"region"`
	Status           types.String `tfsdk:"status"`
	IsDefault        types.Bool   `tfsdk:"is_default"`
	Experimental     types.Bool   `tfsdk:"experimental"`
}

func (d *hostedCollectorDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_hosted_collector"
}

func (d *hostedCollectorDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "An OpenTelemetry collector that AppSignal hosts for the organization, looked up by id, " +
			"or the organization's default one when id is left out.",
		Attributes: map[string]schema.Attribute{
			"organization_slug": schema.StringAttribute{Required: true},
			"id": schema.StringAttribute{
				Description: "Leave out for the organization's default hosted collector.", Optional: true, Computed: true,
			},
			"url": schema.StringAttribute{
				Description: "Base URL that OTLP/HTTP exporters send to; signals go to `/v1/traces`, `/v1/metrics` and `/v1/logs`.",
				Computed:    true,
			},
			"subdomain":    schema.StringAttribute{Computed: true},
			"region":       schema.StringAttribute{Computed: true},
			"status":       schema.StringAttribute{Description: "Such as `provisioned`.", Computed: true},
			"is_default":   schema.BoolAttribute{Computed: true},
			"experimental": schema.BoolAttribute{Description: "Whether it runs AppSignal's experimental build.", Computed: true},
		},
	}
}

func (d *hostedCollectorDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = clientFrom(req.ProviderData, &resp.Diagnostics)
}

func (d *hostedCollectorDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var m hostedCollectorModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	all, err := d.client.ListHostedCollectors(ctx, m.OrganizationSlug.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Reading AppSignal hosted collectors", err.Error())
		return
	}
	for _, c := range all {
		if (m.ID.IsNull() && c.IsDefault) || c.ID == m.ID.ValueString() {
			m.ID = types.StringValue(c.ID)
			m.URL = types.StringValue(c.URL)
			m.Subdomain = types.StringValue(c.Subdomain)
			m.Region = types.StringValue(c.Region)
			m.Status = types.StringValue(c.Status)
			m.IsDefault = types.BoolValue(c.IsDefault)
			m.Experimental = types.BoolValue(c.Experimental)
			resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
			return
		}
	}
	what := "no default hosted collector"
	if !m.ID.IsNull() {
		what = fmt.Sprintf("no hosted collector %q", m.ID.ValueString())
	}
	resp.Diagnostics.AddError("AppSignal hosted collector not found",
		fmt.Sprintf("Organization %q has %s.", m.OrganizationSlug.ValueString(), what))
}
