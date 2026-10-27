package provider

import (
	"context"
	"regexp"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/setdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/SevenLabnl/terraform-provider-appsignal/internal/client"
)

var (
	_ resource.Resource                = &statusPageResource{}
	_ resource.ResourceWithConfigure   = &statusPageResource{}
	_ resource.ResourceWithImportState = &statusPageResource{}
)

var hostnamePattern = regexp.MustCompile(`^[A-Za-z0-9-]+(\.[A-Za-z0-9-]+)+$`)

func NewStatusPageResource() resource.Resource { return &statusPageResource{} }

type statusPageResource struct{ client *client.Client }

type statusPageModel struct {
	ID               types.String `tfsdk:"id"`
	OrganizationSlug types.String `tfsdk:"organization_slug"`
	Hostname         types.String `tfsdk:"hostname"`
	Title            types.String `tfsdk:"title"`
	Description      types.String `tfsdk:"description"`
	Threshold        types.Int64  `tfsdk:"threshold"`
	UptimeMonitorIDs types.Set    `tfsdk:"uptime_monitor_ids"`
}

func (r *statusPageResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_status_page"
}

func (r *statusPageResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{
		Description: "A public status page of an AppSignal organization, showing uptime monitors. AppSignal " +
			"only accepts the hostname once it has a CNAME record pointing to `cname.appsignal-status.com`, " +
			"so create that record first (and make this resource depend on it).",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"organization_slug": schema.StringAttribute{Required: true, PlanModifiers: replace},
			"hostname": schema.StringAttribute{
				Description: "Hostname the page is served on, such as `status.example.com`. Changing it replaces the page.",
				Required:    true, PlanModifiers: replace,
				Validators: []validator.String{stringvalidator.RegexMatches(hostnamePattern,
					"must be a hostname such as status.example.com, without scheme or path")},
			},
			"title": schema.StringAttribute{Required: true, Validators: []validator.String{stringvalidator.LengthAtLeast(1)}},
			"description": schema.StringAttribute{
				Optional: true, Validators: []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"threshold": schema.Int64Attribute{
				Description: "Minimum threshold for considering a service down, as AppSignal describes it. At least 1.",
				Required:    true, Validators: []validator.Int64{int64validator.AtLeast(1)},
			},
			"uptime_monitor_ids": schema.SetAttribute{
				Description: "Uptime monitors shown on the page. They may belong to any app of the organization.",
				ElementType: types.StringType, Optional: true, Computed: true,
				Default: setdefault.StaticValue(types.SetValueMust(types.StringType, nil)),
			},
		},
	}
}

func (r *statusPageResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFrom(req.ProviderData, &resp.Diagnostics)
}

func (m *statusPageModel) page() client.StatusPage {
	return client.StatusPage{
		ID: m.ID.ValueString(), Title: m.Title.ValueString(), Description: stringPtr(m.Description),
		Hostname: m.Hostname.ValueString(), Threshold: m.Threshold.ValueInt64(),
	}
}

func (m *statusPageModel) fill(p *client.StatusPage) {
	m.ID = types.StringValue(p.ID)
	m.Title = types.StringValue(p.Title)
	m.Description = optionalString(p.Description)
	m.Hostname = types.StringValue(p.Hostname)
	m.Threshold = types.Int64Value(p.Threshold)
}

func (r *statusPageResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan statusPageModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	p := plan.page()
	p.UptimeMonitorIDs = stringSet(ctx, plan.UptimeMonitorIDs, &resp.Diagnostics)
	got, err := r.client.CreateStatusPage(ctx, plan.OrganizationSlug.ValueString(), p)
	if err != nil {
		resp.Diagnostics.AddError("Creating AppSignal status page", err.Error())
		return
	}
	plan.fill(got)
	plan.UptimeMonitorIDs = stringValueSet(got.UptimeMonitorIDs, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *statusPageResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state statusPageModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	got, err := r.client.GetStatusPage(ctx, state.OrganizationSlug.ValueString(), state.ID.ValueString())
	if client.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Reading AppSignal status page", err.Error())
		return
	}
	state.fill(got)
	state.UptimeMonitorIDs = stringValueSet(got.UptimeMonitorIDs, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *statusPageResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state statusPageModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	plan.ID = state.ID
	p := plan.page()
	p.UptimeMonitorIDs = stringSet(ctx, plan.UptimeMonitorIDs, &resp.Diagnostics)
	got, err := r.client.UpdateStatusPage(ctx, plan.OrganizationSlug.ValueString(), p)
	if err != nil {
		resp.Diagnostics.AddError("Updating AppSignal status page", err.Error())
		return
	}
	plan.fill(got)
	plan.UptimeMonitorIDs = stringValueSet(got.UptimeMonitorIDs, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *statusPageResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state statusPageModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.client.DeleteStatusPage(ctx, state.OrganizationSlug.ValueString(), state.ID.ValueString())
	if err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Deleting AppSignal status page", err.Error())
	}
}

func (r *statusPageResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	org, id, ok := splitImportID(req.ID)
	if !ok {
		resp.Diagnostics.AddError("Invalid import ID", "Expected <organization_slug>/<status_page_id>, got "+req.ID)
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("organization_slug"), org)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), id)...)
}
