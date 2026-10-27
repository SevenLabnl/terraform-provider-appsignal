package provider

import (
	"context"
	"sort"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/mapvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/setvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/setdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/SevenLabnl/terraform-provider-appsignal/internal/client"
)

var (
	_ resource.Resource                = &uptimeMonitorResource{}
	_ resource.ResourceWithConfigure   = &uptimeMonitorResource{}
	_ resource.ResourceWithImportState = &uptimeMonitorResource{}
)

var uptimeRegions = []string{"europe", "north_america", "asia_pacific", "south_america"}

func NewUptimeMonitorResource() resource.Resource { return &uptimeMonitorResource{} }

type uptimeMonitorResource struct{ client *client.Client }

type uptimeMonitorModel struct {
	ID                   types.String `tfsdk:"id"`
	AppID                types.String `tfsdk:"app_id"`
	Name                 types.String `tfsdk:"name"`
	URL                  types.String `tfsdk:"url"`
	Description          types.String `tfsdk:"description"`
	Regions              types.Set    `tfsdk:"regions"`
	NotifierIDs          types.Set    `tfsdk:"notifier_ids"`
	WarmupDuration       types.Int64  `tfsdk:"warmup_duration"`
	Headers              types.Map    `tfsdk:"headers"`
	CheckBodyContent     types.String `tfsdk:"check_body_content"`
	MonitorSSLExpiration types.Bool   `tfsdk:"monitor_ssl_expiration"`
}

func (r *uptimeMonitorResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_uptime_monitor"
}

func (r *uptimeMonitorResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "An uptime monitor in an AppSignal app. AppSignal checks the URL every minute " +
			"and expects a 2xx response within 30 seconds; neither can be configured.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"app_id": schema.StringAttribute{
				Required: true, PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"name": schema.StringAttribute{Required: true},
			"url":  schema.StringAttribute{Required: true},
			"description": schema.StringAttribute{
				Optional: true, Validators: []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"regions": schema.SetAttribute{
				Description: "Regions to check from: " + strings.Join(uptimeRegions, ", ") + ".",
				ElementType: types.StringType, Required: true,
				Validators: []validator.Set{
					setvalidator.SizeAtLeast(1),
					setvalidator.ValueStringsAre(stringvalidator.OneOf(uptimeRegions...)),
				},
			},
			"notifier_ids": schema.SetAttribute{
				Description: "Notifiers alerted when the monitor fails. See the appsignal_notifier data source.",
				ElementType: types.StringType, Optional: true, Computed: true,
				Default: setdefault.StaticValue(types.SetValueMust(types.StringType, nil)),
			},
			"warmup_duration": schema.Int64Attribute{
				Description: "Minutes the check must keep failing before an alert opens. Defaults to 0.",
				Optional:    true, Computed: true, Default: int64default.StaticInt64(0),
			},
			"headers": schema.MapAttribute{
				Description: "Request headers sent with every check.",
				ElementType: types.StringType, Optional: true, Sensitive: true,
				Validators: []validator.Map{mapvalidator.SizeAtLeast(1)},
			},
			"check_body_content": schema.StringAttribute{
				Description: "Text the response body must contain.",
				Optional:    true, Validators: []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"monitor_ssl_expiration": schema.BoolAttribute{
				Description: "Alert before the certificate expires. Defaults to false.",
				Optional:    true, Computed: true, Default: booldefault.StaticBool(false),
			},
		},
	}
}

func (r *uptimeMonitorResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFrom(req.ProviderData, &resp.Diagnostics)
}

func (m *uptimeMonitorModel) monitor(ctx context.Context, diags *diag.Diagnostics) client.UptimeMonitor {
	out := client.UptimeMonitor{
		ID:               m.ID.ValueString(),
		Name:             m.Name.ValueString(),
		URL:              m.URL.ValueString(),
		Description:      stringPtr(m.Description),
		WarmupDuration:   m.WarmupDuration.ValueInt64(),
		CheckBodyContent: stringPtr(m.CheckBodyContent),
	}
	ssl := m.MonitorSSLExpiration.ValueBool()
	out.MonitorSSLExpiration = &ssl

	var regions []string
	diags.Append(m.Regions.ElementsAs(ctx, &regions, false)...)
	for _, r := range regions {
		out.Regions = append(out.Regions, strings.ToUpper(r))
	}
	sort.Strings(out.Regions)
	if !m.NotifierIDs.IsNull() && !m.NotifierIDs.IsUnknown() {
		diags.Append(m.NotifierIDs.ElementsAs(ctx, &out.NotifierIDs, false)...)
		sort.Strings(out.NotifierIDs)
	}
	if !m.Headers.IsNull() && !m.Headers.IsUnknown() {
		headers := map[string]string{}
		diags.Append(m.Headers.ElementsAs(ctx, &headers, false)...)
		keys := make([]string, 0, len(headers))
		for k := range headers {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			v := headers[k]
			out.Headers = append(out.Headers, client.KeyValue{Key: k, Value: &v})
		}
	}
	return out
}

func (m *uptimeMonitorModel) fill(u *client.UptimeMonitor, diags *diag.Diagnostics) {
	m.ID = types.StringValue(u.ID)
	m.Name = types.StringValue(u.Name)
	m.URL = types.StringValue(u.URL)
	m.Description = optionalString(u.Description)
	m.CheckBodyContent = optionalString(u.CheckBodyContent)
	m.WarmupDuration = types.Int64Value(u.WarmupDuration)
	m.MonitorSSLExpiration = types.BoolValue(u.MonitorSSLExpiration != nil && *u.MonitorSSLExpiration)

	regions := make([]attr.Value, 0, len(u.Regions))
	for _, r := range u.Regions {
		regions = append(regions, types.StringValue(strings.ToLower(r)))
	}
	var d diag.Diagnostics
	m.Regions, d = types.SetValue(types.StringType, regions)
	diags.Append(d...)

	notifiers := make([]attr.Value, 0, len(u.NotifierIDs))
	for _, id := range u.NotifierIDs {
		notifiers = append(notifiers, types.StringValue(id))
	}
	m.NotifierIDs, d = types.SetValue(types.StringType, notifiers)
	diags.Append(d...)

	if len(u.Headers) == 0 {
		m.Headers = types.MapNull(types.StringType)
	} else {
		headers := map[string]attr.Value{}
		for _, h := range u.Headers {
			v := ""
			if h.Value != nil {
				v = *h.Value
			}
			headers[h.Key] = types.StringValue(v)
		}
		m.Headers, d = types.MapValue(types.StringType, headers)
		diags.Append(d...)
	}
}

func (r *uptimeMonitorResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan uptimeMonitorModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	in := plan.monitor(ctx, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	u, err := r.client.CreateUptimeMonitor(ctx, plan.AppID.ValueString(), in)
	if err != nil {
		resp.Diagnostics.AddError("Creating AppSignal uptime monitor", err.Error())
		return
	}
	plan.fill(u, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *uptimeMonitorResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state uptimeMonitorModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	u, err := r.client.GetUptimeMonitor(ctx, state.AppID.ValueString(), state.ID.ValueString())
	if client.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Reading AppSignal uptime monitor", err.Error())
		return
	}
	state.fill(u, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *uptimeMonitorResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state uptimeMonitorModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	plan.ID = state.ID
	in := plan.monitor(ctx, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	u, err := r.client.UpdateUptimeMonitor(ctx, plan.AppID.ValueString(), in)
	if err != nil {
		resp.Diagnostics.AddError("Updating AppSignal uptime monitor", err.Error())
		return
	}
	plan.fill(u, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *uptimeMonitorResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state uptimeMonitorModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.client.DeleteUptimeMonitor(ctx, state.AppID.ValueString(), state.ID.ValueString())
	if err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Deleting AppSignal uptime monitor", err.Error())
	}
}

func (r *uptimeMonitorResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	appID, id, ok := splitImportID(req.ID)
	if !ok {
		resp.Diagnostics.AddError("Invalid import ID", "Expected <app_id>/<uptime_monitor_id>, got "+req.ID)
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("app_id"), appID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), id)...)
}
