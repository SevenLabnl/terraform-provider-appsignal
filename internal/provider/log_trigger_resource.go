package provider

import (
	"context"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/setdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/SevenLabnl/terraform-provider-appsignal/internal/client"
)

var (
	_ resource.Resource                = &logTriggerResource{}
	_ resource.ResourceWithConfigure   = &logTriggerResource{}
	_ resource.ResourceWithImportState = &logTriggerResource{}
)

var notificationFrequencies = []string{"always", "never", "first_in_deploy", "first_after_close", "nth_in_hour", "nth_in_day"}

func NewLogTriggerResource() resource.Resource { return &logTriggerResource{} }

type logTriggerResource struct{ client *client.Client }

type logTriggerModel struct {
	ID                    types.String `tfsdk:"id"`
	AppID                 types.String `tfsdk:"app_id"`
	Name                  types.String `tfsdk:"name"`
	Query                 types.String `tfsdk:"query"`
	Description           types.String `tfsdk:"description"`
	SourceIDs             types.Set    `tfsdk:"source_ids"`
	NotifierIDs           types.Set    `tfsdk:"notifier_ids"`
	NotificationFrequency types.String `tfsdk:"notification_frequency"`
	NotificationThreshold types.Int64  `tfsdk:"notification_threshold"`
}

func (r *logTriggerResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_log_trigger"
}

func (r *logTriggerResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	emptySet := setdefault.StaticValue(types.SetValueMust(types.StringType, nil))
	nonEmpty := []validator.String{stringvalidator.LengthAtLeast(1)}
	resp.Schema = schema.Schema{
		Description: "A log trigger in an AppSignal app: it opens a log incident when incoming lines match a query.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"app_id": schema.StringAttribute{
				Required: true, PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"name": schema.StringAttribute{Required: true, Validators: nonEmpty},
			"query": schema.StringAttribute{
				Description: "Log query in AppSignal's query syntax. AppSignal does not validate it, so check it in " +
					"the log viewer first.",
				Required: true, Validators: nonEmpty,
			},
			"description": schema.StringAttribute{Optional: true, Validators: nonEmpty},
			"source_ids": schema.SetAttribute{
				Description: "Log sources the trigger watches. Empty means every source of the app.",
				ElementType: types.StringType, Optional: true, Computed: true, Default: emptySet,
			},
			"notifier_ids": schema.SetAttribute{
				Description: "Notifiers alerted for new incidents. See the appsignal_notifier data source.",
				ElementType: types.StringType, Optional: true, Computed: true, Default: emptySet,
			},
			"notification_frequency": schema.StringAttribute{
				Description: "When to notify: " + strings.Join(notificationFrequencies, ", ") + ". Defaults to `always`.",
				Optional:    true, Computed: true, Default: stringdefault.StaticString("always"),
				Validators: []validator.String{stringvalidator.OneOf(notificationFrequencies...)},
			},
			"notification_threshold": schema.Int64Attribute{
				Description: "N for the `nth_in_hour` and `nth_in_day` frequencies. Defaults to 1.",
				Optional:    true, Computed: true, Default: int64default.StaticInt64(1),
				Validators: []validator.Int64{int64validator.AtLeast(1)},
			},
		},
	}
}

func (r *logTriggerResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFrom(req.ProviderData, &resp.Diagnostics)
}

func (m *logTriggerModel) input(ctx context.Context, diags *diag.Diagnostics) client.LogTriggerInput {
	in := client.LogTriggerInput{LogTrigger: client.LogTrigger{
		ID:                       m.ID.ValueString(),
		Name:                     m.Name.ValueString(),
		Query:                    m.Query.ValueString(),
		Description:              stringPtr(m.Description),
		NotificationOptions:      strings.ToUpper(m.NotificationFrequency.ValueString()),
		NotificationTriggerValue: m.NotificationThreshold.ValueInt64(),
	}}
	in.SourceIDs = stringSet(ctx, m.SourceIDs, diags)
	in.NotifierIDs = stringSet(ctx, m.NotifierIDs, diags)
	return in
}

func (m *logTriggerModel) fill(t *client.LogTrigger, diags *diag.Diagnostics) {
	m.ID = types.StringValue(t.ID)
	m.Name = types.StringValue(t.Name)
	m.Query = types.StringValue(t.Query)
	m.Description = optionalString(t.Description)
	m.SourceIDs = stringValueSet(t.SourceIDs, diags)
	m.NotifierIDs = notifierIDSet(t.Notifiers, diags)
	m.NotificationFrequency = types.StringValue(strings.ToLower(t.NotificationOptions))
	m.NotificationThreshold = types.Int64Value(t.NotificationTriggerValue)
}

func (r *logTriggerResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan logTriggerModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	in := plan.input(ctx, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	t, err := r.client.CreateLogTrigger(ctx, plan.AppID.ValueString(), in)
	if err != nil {
		resp.Diagnostics.AddError("Creating AppSignal log trigger", err.Error())
		return
	}
	plan.fill(t, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *logTriggerResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state logTriggerModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	t, err := r.client.GetLogTrigger(ctx, state.AppID.ValueString(), state.ID.ValueString())
	if client.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Reading AppSignal log trigger", err.Error())
		return
	}
	state.fill(t, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *logTriggerResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state logTriggerModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	plan.ID = state.ID
	in := plan.input(ctx, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	t, err := r.client.UpdateLogTrigger(ctx, plan.AppID.ValueString(), in)
	if err != nil {
		resp.Diagnostics.AddError("Updating AppSignal log trigger", err.Error())
		return
	}
	plan.fill(t, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *logTriggerResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state logTriggerModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.client.DeleteLogTrigger(ctx, state.AppID.ValueString(), state.ID.ValueString())
	if err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Deleting AppSignal log trigger", err.Error())
	}
}

func (r *logTriggerResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	appID, id, ok := splitImportID(req.ID)
	if !ok {
		resp.Diagnostics.AddError("Invalid import ID", "Expected <app_id>/<log_trigger_id>, got "+req.ID)
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("app_id"), appID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), id)...)
}
