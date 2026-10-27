package provider

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"
	_ "time/tzdata" // timezones are checked without relying on the host's zone files

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/setdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/SevenLabnl/terraform-provider-appsignal/internal/client"
)

var (
	_ resource.Resource                   = &checkInResource{}
	_ resource.ResourceWithConfigure      = &checkInResource{}
	_ resource.ResourceWithImportState    = &checkInResource{}
	_ resource.ResourceWithValidateConfig = &checkInResource{}
)

var cronField = regexp.MustCompile(`^[0-9A-Za-z*,/-]+$`)

func NewCheckInResource() resource.Resource { return &checkInResource{} }

type checkInResource struct{ client *client.Client }

type checkInModel struct {
	ID              types.String `tfsdk:"id"`
	AppID           types.String `tfsdk:"app_id"`
	Identifier      types.String `tfsdk:"identifier"`
	Kind            types.String `tfsdk:"kind"`
	Schedule        types.String `tfsdk:"schedule"`
	Timezone        types.String `tfsdk:"timezone"`
	WaitTimeMinutes types.Int64  `tfsdk:"wait_time_minutes"`
	Description     types.String `tfsdk:"description"`
	NotifierIDs     types.Set    `tfsdk:"notifier_ids"`
}

func (r *checkInResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_check_in"
}

func (r *checkInResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{
		Description: "A check-in monitor in an AppSignal app. A `cron` check-in expects a check-in on a schedule; " +
			"a `heartbeat` check-in expects one at least every `wait_time_minutes`. Jobs check in with the identifier.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"app_id": schema.StringAttribute{Required: true, PlanModifiers: replace},
			"identifier": schema.StringAttribute{
				Description: "Name the job checks in as. It must be unique within the app.",
				Required:    true,
				Validators: []validator.String{
					stringvalidator.RegexMatches(regexp.MustCompile(`^\S+$`), "must be one word without spaces"),
				},
			},
			"kind": schema.StringAttribute{
				Description: "`cron` or `heartbeat`. Changing it replaces the check-in and its history.",
				Required:    true, PlanModifiers: replace,
				Validators: []validator.String{stringvalidator.OneOf("cron", "heartbeat")},
			},
			"schedule": schema.StringAttribute{
				Description: "Five-field cron expression, for `cron` check-ins only.",
				Optional:    true,
			},
			"timezone": schema.StringAttribute{
				Description: "IANA timezone the schedule runs in, for `cron` check-ins. Defaults to `UTC`.",
				Optional:    true, Computed: true, Default: stringdefault.StaticString("UTC"),
			},
			"wait_time_minutes": schema.Int64Attribute{
				Description: "Minutes after the expected time before a check-in counts as missed. At least 1.",
				Required:    true, Validators: []validator.Int64{int64validator.AtLeast(1)},
			},
			"description": schema.StringAttribute{
				Optional: true, Validators: []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"notifier_ids": schema.SetAttribute{
				Description: "Notifiers alerted when a check-in is missed. See the appsignal_notifier data source.",
				ElementType: types.StringType, Optional: true, Computed: true,
				Default: setdefault.StaticValue(types.SetValueMust(types.StringType, nil)),
			},
		},
	}
}

// ValidateConfig checks what AppSignal itself accepts unchecked: a cron check-in without a
// schedule, a malformed schedule, an unknown timezone, or a schedule on a heartbeat.
func (r *checkInResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var m checkInModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() || m.Kind.IsUnknown() || m.Schedule.IsUnknown() || m.Timezone.IsUnknown() {
		return
	}
	if !m.Timezone.IsNull() {
		if _, err := time.LoadLocation(m.Timezone.ValueString()); err != nil || m.Timezone.ValueString() == "" ||
			strings.EqualFold(m.Timezone.ValueString(), "local") {
			resp.Diagnostics.AddAttributeError(path.Root("timezone"), "Unknown timezone",
				fmt.Sprintf("%q is not an IANA timezone such as Europe/Amsterdam or UTC.", m.Timezone.ValueString()))
		}
	}
	switch m.Kind.ValueString() {
	case "cron":
		if m.Schedule.IsNull() {
			resp.Diagnostics.AddAttributeError(path.Root("schedule"), "Missing schedule", "A cron check-in needs a schedule.")
			return
		}
		fields := strings.Fields(m.Schedule.ValueString())
		valid := len(fields) == 5
		for _, f := range fields {
			valid = valid && cronField.MatchString(f)
		}
		if !valid {
			resp.Diagnostics.AddAttributeError(path.Root("schedule"), "Invalid schedule",
				fmt.Sprintf("%q is not a five-field cron expression such as \"0 2 * * *\".", m.Schedule.ValueString()))
		}
	case "heartbeat":
		if !m.Schedule.IsNull() {
			resp.Diagnostics.AddAttributeError(path.Root("schedule"), "Schedule on a heartbeat",
				"Only cron check-ins have a schedule.")
		}
		if !m.Timezone.IsNull() && m.Timezone.ValueString() != "UTC" {
			resp.Diagnostics.AddAttributeError(path.Root("timezone"), "Timezone on a heartbeat",
				"Only cron check-ins have a timezone.")
		}
	}
}

func (r *checkInResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFrom(req.ProviderData, &resp.Diagnostics)
}

func (m *checkInModel) input(ctx context.Context, diags *diag.Diagnostics) client.CheckInInput {
	in := client.CheckInInput{CheckIn: client.CheckIn{
		ID:              m.ID.ValueString(),
		Identifier:      m.Identifier.ValueString(),
		Kind:            strings.ToUpper(m.Kind.ValueString()),
		Syntax:          stringPtr(m.Schedule),
		Description:     stringPtr(m.Description),
		Timezone:        m.Timezone.ValueString(),
		WaitTimeMinutes: m.WaitTimeMinutes.ValueInt64(),
	}}
	in.NotifierIDs = stringSet(ctx, m.NotifierIDs, diags)
	return in
}

func (m *checkInModel) fill(ci *client.CheckIn, diags *diag.Diagnostics) {
	m.ID = types.StringValue(ci.ID)
	m.Identifier = types.StringValue(ci.Identifier)
	m.Kind = types.StringValue(strings.ToLower(ci.Kind))
	m.Schedule = optionalString(ci.Syntax)
	m.Timezone = types.StringValue(ci.Timezone)
	m.WaitTimeMinutes = types.Int64Value(ci.WaitTimeMinutes)
	m.Description = optionalString(ci.Description)
	m.NotifierIDs = notifierIDSet(ci.Notifiers, diags)
}

// identifierTaken reports another check-in with the identifier: AppSignal allows duplicates,
// which would make check-ins ambiguous.
func (r *checkInResource) identifierTaken(ctx context.Context, appID, identifier, self string, diags *diag.Diagnostics) bool {
	all, err := r.client.ListCheckIns(ctx, appID)
	if err != nil {
		diags.AddError("Listing AppSignal check-ins", err.Error())
		return true
	}
	for _, ci := range all {
		if ci.Identifier == identifier && ci.ID != self {
			diags.AddAttributeError(path.Root("identifier"), "Check-in identifier in use",
				fmt.Sprintf("Check-in %s already uses %q. Import it (<app_id>/%s) or pick another identifier.", ci.ID, identifier, ci.ID))
			return true
		}
	}
	return false
}

func (r *checkInResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan checkInModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if r.identifierTaken(ctx, plan.AppID.ValueString(), plan.Identifier.ValueString(), "", &resp.Diagnostics) {
		return
	}
	in := plan.input(ctx, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	ci, err := r.client.CreateCheckIn(ctx, plan.AppID.ValueString(), in)
	if err != nil {
		resp.Diagnostics.AddError("Creating AppSignal check-in", err.Error())
		return
	}
	plan.fill(ci, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *checkInResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state checkInModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	ci, err := r.client.GetCheckIn(ctx, state.AppID.ValueString(), state.ID.ValueString())
	if client.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Reading AppSignal check-in", err.Error())
		return
	}
	state.fill(ci, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *checkInResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state checkInModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	plan.ID = state.ID
	if r.identifierTaken(ctx, plan.AppID.ValueString(), plan.Identifier.ValueString(), state.ID.ValueString(), &resp.Diagnostics) {
		return
	}
	in := plan.input(ctx, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	ci, err := r.client.UpdateCheckIn(ctx, plan.AppID.ValueString(), in)
	if err != nil {
		resp.Diagnostics.AddError("Updating AppSignal check-in", err.Error())
		return
	}
	plan.fill(ci, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *checkInResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state checkInModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.client.DeleteCheckIn(ctx, state.AppID.ValueString(), state.ID.ValueString())
	if err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Deleting AppSignal check-in", err.Error())
	}
}

func (r *checkInResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	appID, id, ok := splitImportID(req.ID)
	if !ok {
		resp.Diagnostics.AddError("Invalid import ID", "Expected <app_id>/<check_in_id>, got "+req.ID)
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("app_id"), appID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), id)...)
}
