package provider

import (
	"context"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/SevenLabnl/terraform-provider-appsignal/internal/client"
)

var (
	_ resource.Resource                = &logSourceResource{}
	_ resource.ResourceWithConfigure   = &logSourceResource{}
	_ resource.ResourceWithImportState = &logSourceResource{}
)

var logFormats = []string{"plaintext", "logfmt", "json", "autodetect"}

func NewLogSourceResource() resource.Resource { return &logSourceResource{} }

type logSourceResource struct{ client *client.Client }

type logSourceModel struct {
	ID            types.String `tfsdk:"id"`
	AppID         types.String `tfsdk:"app_id"`
	Name          types.String `tfsdk:"name"`
	Type          types.String `tfsdk:"type"`
	Format        types.String `tfsdk:"format"`
	ParseSeverity types.Bool   `tfsdk:"parse_severity"`
	Key           types.String `tfsdk:"key"`
}

func (r *logSourceResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_log_source"
}

func (r *logSourceResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	keep := []planmodifier.String{stringplanmodifier.UseStateForUnknown()}
	resp.Schema = schema.Schema{
		Description: "A log source in an AppSignal app: an endpoint that accepts log lines with its own key.",
		Attributes: map[string]schema.Attribute{
			"id":     schema.StringAttribute{Computed: true, PlanModifiers: keep},
			"app_id": schema.StringAttribute{Required: true, PlanModifiers: replace},
			"name":   schema.StringAttribute{Required: true},
			"type": schema.StringAttribute{
				Description: "Kind of source, for example `http_json`. Cannot be changed after creation.",
				Required:    true, PlanModifiers: replace,
			},
			"format": schema.StringAttribute{
				Description: "How lines are parsed: " + strings.Join(logFormats, ", ") + ". Defaults to `plaintext`.",
				Optional:    true, Computed: true,
				Default:    stringdefault.StaticString("plaintext"),
				Validators: []validator.String{stringvalidator.OneOf(logFormats...)},
			},
			"parse_severity": schema.BoolAttribute{
				Description: "Read the severity from the line. Defaults to false.",
				Optional:    true, Computed: true, Default: booldefault.StaticBool(false),
			},
			"key": schema.StringAttribute{
				Description: "Ingestion key of this source.",
				Computed:    true, Sensitive: true, PlanModifiers: keep,
			},
		},
	}
}

func (r *logSourceResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFrom(req.ProviderData, &resp.Diagnostics)
}

func (m *logSourceModel) fill(s *client.LogSource) {
	m.ID = types.StringValue(s.ID)
	m.Name = types.StringValue(s.Name)
	m.Type = types.StringValue(s.Type)
	m.Format = types.StringValue(strings.ToLower(s.Format))
	m.ParseSeverity = types.BoolValue(s.ParseSeverity)
	m.Key = types.StringValue(s.Key)
}

func (m *logSourceModel) source() client.LogSource {
	return client.LogSource{
		ID:            m.ID.ValueString(),
		Name:          m.Name.ValueString(),
		Type:          m.Type.ValueString(),
		Format:        strings.ToUpper(m.Format.ValueString()),
		ParseSeverity: m.ParseSeverity.ValueBool(),
	}
}

func (r *logSourceResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan logSourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	s, err := r.client.CreateLogSource(ctx, plan.AppID.ValueString(), plan.source())
	if err != nil {
		resp.Diagnostics.AddError("Creating AppSignal log source", err.Error())
		return
	}
	plan.fill(s)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *logSourceResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state logSourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	s, err := r.client.GetLogSource(ctx, state.AppID.ValueString(), state.ID.ValueString())
	if client.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Reading AppSignal log source", err.Error())
		return
	}
	state.fill(s)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *logSourceResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state logSourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	plan.ID = state.ID
	s, err := r.client.UpdateLogSource(ctx, plan.AppID.ValueString(), plan.source())
	if err != nil {
		resp.Diagnostics.AddError("Updating AppSignal log source", err.Error())
		return
	}
	plan.fill(s)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *logSourceResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state logSourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.client.DeleteLogSource(ctx, state.AppID.ValueString(), state.ID.ValueString())
	if err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Deleting AppSignal log source", err.Error())
	}
}

func (r *logSourceResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	appID, id, ok := splitImportID(req.ID)
	if !ok {
		resp.Diagnostics.AddError("Invalid import ID", "Expected <app_id>/<log_source_id>, got "+req.ID)
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("app_id"), appID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), id)...)
}
