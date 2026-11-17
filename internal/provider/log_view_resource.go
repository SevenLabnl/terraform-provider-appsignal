package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/setdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/SevenLabnl/terraform-provider-appsignal/internal/client"
)

var (
	_ resource.Resource                = &logViewResource{}
	_ resource.ResourceWithConfigure   = &logViewResource{}
	_ resource.ResourceWithImportState = &logViewResource{}
)

func NewLogViewResource() resource.Resource { return &logViewResource{} }

type logViewResource struct{ client *client.Client }

type logViewModel struct {
	ID         types.String `tfsdk:"id"`
	AppID      types.String `tfsdk:"app_id"`
	Name       types.String `tfsdk:"name"`
	SourceIDs  types.Set    `tfsdk:"source_ids"`
	Query      types.String `tfsdk:"query"`
	Columns    types.List   `tfsdk:"columns"`
	LineHeight types.String `tfsdk:"line_height"`
}

func (r *logViewResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_log_view"
}

func (r *logViewResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A saved view in the log viewer of an AppSignal app. AppSignal makes one when a log source is " +
			"created in its UI, but not when a source is created through the API.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"app_id": schema.StringAttribute{
				Required: true, PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"name": schema.StringAttribute{
				Required: true, Validators: []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"source_ids": schema.SetAttribute{
				Description: "Log sources the view shows.",
				ElementType: types.StringType, Optional: true, Computed: true,
				Default: setdefault.StaticValue(types.SetValueMust(types.StringType, nil)),
			},
			"query": schema.StringAttribute{
				Description: "Log query in AppSignal's query syntax. AppSignal does not validate it, so check it in " +
					"the log viewer first.",
				Optional: true, Computed: true, Default: stringdefault.StaticString(""),
			},
			"columns": schema.ListAttribute{
				Description: "Attributes shown as extra columns, in order. AppSignal does not check the names.",
				ElementType: types.StringType, Optional: true, Computed: true,
				Default: listdefault.StaticValue(types.ListValueMust(types.StringType, nil)),
			},
			"line_height": schema.StringAttribute{
				Description: "Line height as the log viewer stores it. Defaults to `0`, the value of views made in the UI.",
				Optional:    true, Computed: true, Default: stringdefault.StaticString("0"),
			},
		},
	}
}

func (r *logViewResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFrom(req.ProviderData, &resp.Diagnostics)
}

func (m *logViewModel) view(ctx context.Context, diags *diag.Diagnostics) client.LogView {
	v := client.LogView{
		ID:         m.ID.ValueString(),
		Name:       m.Name.ValueString(),
		Query:      m.Query.ValueString(),
		LineHeight: m.LineHeight.ValueString(),
		SourceIDs:  stringSet(ctx, m.SourceIDs, diags),
	}
	if !m.Columns.IsNull() && !m.Columns.IsUnknown() {
		diags.Append(m.Columns.ElementsAs(ctx, &v.Columns, false)...)
	}
	return v
}

func (m *logViewModel) fill(v *client.LogView, diags *diag.Diagnostics) {
	m.ID = types.StringValue(v.ID)
	m.Name = types.StringValue(v.Name)
	m.Query = types.StringValue(v.Query)
	m.LineHeight = types.StringValue(v.LineHeight)
	m.SourceIDs = stringValueSet(v.SourceIDs, diags)
	columns := make([]attr.Value, 0, len(v.Columns))
	for _, c := range v.Columns {
		columns = append(columns, types.StringValue(c))
	}
	var d diag.Diagnostics
	m.Columns, d = types.ListValue(types.StringType, columns)
	diags.Append(d...)
}

func (r *logViewResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan logViewModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	in := plan.view(ctx, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	v, err := r.client.CreateLogView(ctx, plan.AppID.ValueString(), in)
	if err != nil {
		resp.Diagnostics.AddError("Creating AppSignal log view", err.Error())
		return
	}
	plan.fill(v, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *logViewResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state logViewModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	v, err := r.client.GetLogView(ctx, state.AppID.ValueString(), state.ID.ValueString())
	if client.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Reading AppSignal log view", err.Error())
		return
	}
	state.fill(v, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *logViewResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state logViewModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	plan.ID = state.ID
	in := plan.view(ctx, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	v, err := r.client.UpdateLogView(ctx, plan.AppID.ValueString(), in)
	if err != nil {
		resp.Diagnostics.AddError("Updating AppSignal log view", err.Error())
		return
	}
	plan.fill(v, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *logViewResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state logViewModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.client.DeleteLogView(ctx, state.AppID.ValueString(), state.ID.ValueString())
	if err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Deleting AppSignal log view", err.Error())
	}
}

func (r *logViewResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	appID, id, ok := splitImportID(req.ID)
	if !ok {
		resp.Diagnostics.AddError("Invalid import ID", "Expected <app_id>/<log_view_id>, got "+req.ID)
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("app_id"), appID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), id)...)
}
