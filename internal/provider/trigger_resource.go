package provider

import (
	"context"
	"sort"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/mapvalidator"
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
	_ resource.Resource                   = &triggerResource{}
	_ resource.ResourceWithConfigure      = &triggerResource{}
	_ resource.ResourceWithImportState    = &triggerResource{}
	_ resource.ResourceWithValidateConfig = &triggerResource{}
)

// Uptime monitor kinds are left out: AppSignal makes those triggers for each monitor.
var triggerKinds = []string{
	"Advanced", "HostCPUUsage", "HostDiskIO", "HostDiskUsage", "HostLoadAverage", "HostMemoryUsage",
	"HostNetworkTraffic", "HostSwapUsage", "QueueTime", "Performance", "Throughput", "ExceptionRate",
	"ExceptionCount", "TracePerformance", "TracePerformanceAction", "TraceThroughput", "TraceErrorRate",
	"TraceErrorCount",
}

var (
	metricFields  = []string{"mean", "p90", "p95", "count", "gauge", "counter"}
	valueFormats  = []string{"number", "percent", "duration", "throughput", "size"}
	sizeUnits     = []string{"bit", "byte", "kilobit", "kilobyte", "megabyte"}
	comparisonOps = map[string]string{
		">": "GREATER_THAN", "<": "LESS_THAN", ">=": "GREATER_THAN_OR_EQUAL",
		"<=": "LESS_THAN_OR_EQUAL", "==": "EQUAL", "!=": "NOT_EQUAL",
	}
)

func NewTriggerResource() resource.Resource { return &triggerResource{} }

type triggerResource struct{ client *client.Client }

type triggerModel struct {
	ID                 types.String  `tfsdk:"id"`
	AppID              types.String  `tfsdk:"app_id"`
	Kind               types.String  `tfsdk:"kind"`
	Name               types.String  `tfsdk:"name"`
	Description        types.String  `tfsdk:"description"`
	MetricName         types.String  `tfsdk:"metric_name"`
	Field              types.String  `tfsdk:"field"`
	Tags               types.Map     `tfsdk:"tags"`
	ComparisonOperator types.String  `tfsdk:"comparison_operator"`
	Threshold          types.Float64 `tfsdk:"threshold"`
	WarmupDuration     types.Int64   `tfsdk:"warmup_duration"`
	CooldownDuration   types.Int64   `tfsdk:"cooldown_duration"`
	NoMatchIsZero      types.Bool    `tfsdk:"no_match_is_zero"`
	Format             types.String  `tfsdk:"format"`
	FormatInput        types.String  `tfsdk:"format_input"`
	DashboardID        types.String  `tfsdk:"dashboard_id"`
	NotifierIDs        types.Set     `tfsdk:"notifier_ids"`
}

func (r *triggerResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_trigger"
}

func (r *triggerResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	ops := make([]string, 0, len(comparisonOps))
	for op := range comparisonOps {
		ops = append(ops, op)
	}
	sort.Strings(ops)
	nonEmpty := []validator.String{stringvalidator.LengthAtLeast(1)}
	notNegative := []validator.Int64{int64validator.AtLeast(0)}

	resp.Schema = schema.Schema{
		Description: "A metric trigger (anomaly detection) in an AppSignal app. AppSignal never changes a trigger " +
			"in place: every change archives the trigger and creates a new version with a new id, which also " +
			"closes the alerts that were open on the old version.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description: "Id of the current version. It changes on every update.", Computed: true,
			},
			"app_id": schema.StringAttribute{
				Required: true, PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"kind": schema.StringAttribute{
				Description: "Trigger kind. Use `Advanced` for custom metrics. The provider passes the metric, field " +
					"and tags through as given; to copy the exact shape of a built-in kind, create the trigger in " +
					"the AppSignal UI and import it.",
				Required: true, Validators: []validator.String{stringvalidator.OneOf(triggerKinds...)},
			},
			"name":        schema.StringAttribute{Required: true, Validators: nonEmpty},
			"description": schema.StringAttribute{Optional: true, Validators: nonEmpty},
			"metric_name": schema.StringAttribute{Required: true, Validators: nonEmpty},
			"field": schema.StringAttribute{
				Description: "Metric field to compare: " + strings.Join(metricFields, ", ") + ".",
				Required:    true, Validators: []validator.String{stringvalidator.OneOf(metricFields...)},
			},
			"tags": schema.MapAttribute{
				Description: "Tag filters on the metric. Values may contain `*` wildcards.",
				ElementType: types.StringType, Optional: true,
				Validators: []validator.Map{mapvalidator.SizeAtLeast(1)},
			},
			"comparison_operator": schema.StringAttribute{
				Description: "One of " + strings.Join(ops, ", ") + ".",
				Required:    true, Validators: []validator.String{stringvalidator.OneOf(ops...)},
			},
			"threshold": schema.Float64Attribute{Description: "Value the metric is compared with.", Required: true},
			"warmup_duration": schema.Int64Attribute{
				Description: "Minutes the condition must hold before an alert opens. Defaults to 0.",
				Optional:    true, Computed: true, Default: int64default.StaticInt64(0), Validators: notNegative,
			},
			"cooldown_duration": schema.Int64Attribute{
				Description: "Minutes the condition must be gone before an alert closes. Defaults to 0.",
				Optional:    true, Computed: true, Default: int64default.StaticInt64(0), Validators: notNegative,
			},
			"no_match_is_zero": schema.BoolAttribute{
				Description: "Treat missing data as 0. Defaults to false.",
				Optional:    true, Computed: true, Default: booldefault.StaticBool(false),
			},
			"format": schema.StringAttribute{
				Description: "How values are shown in alerts: " + strings.Join(valueFormats, ", ") + ".",
				Optional:    true, Validators: []validator.String{stringvalidator.OneOf(valueFormats...)},
			},
			"format_input": schema.StringAttribute{
				Description: "Unit of the reported value when format is `size`: " + strings.Join(sizeUnits, ", ") + ".",
				Optional:    true, Validators: []validator.String{stringvalidator.OneOf(sizeUnits...)},
			},
			"dashboard_id": schema.StringAttribute{
				Description: "Dashboard linked from notifications.", Optional: true, Validators: nonEmpty,
			},
			"notifier_ids": schema.SetAttribute{
				Description: "Notifiers alerted when the trigger fires. See the appsignal_notifier data source.",
				ElementType: types.StringType, Optional: true, Computed: true,
				Default: setdefault.StaticValue(types.SetValueMust(types.StringType, nil)),
			},
		},
	}
}

func (r *triggerResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	validateFormatInput(ctx, req.Config, &resp.Diagnostics)
}

func (r *triggerResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFrom(req.ProviderData, &resp.Diagnostics)
}

func (m *triggerModel) input(ctx context.Context, diags *diag.Diagnostics) client.TriggerInput {
	in := client.TriggerInput{Trigger: client.Trigger{
		Kind:        m.Kind.ValueString(),
		Name:        m.Name.ValueString(),
		Description: stringPtr(m.Description),
		MetricName:  m.MetricName.ValueString(),
		Field:       strings.ToUpper(m.Field.ValueString()),
		ThresholdCondition: client.ThresholdCondition{
			ComparisonOperator: comparisonOps[m.ComparisonOperator.ValueString()],
			Value:              m.Threshold.ValueFloat64(),
		},
		WarmupDuration:   m.WarmupDuration.ValueInt64(),
		CooldownDuration: m.CooldownDuration.ValueInt64(),
		NoMatchIsZero:    m.NoMatchIsZero.ValueBool(),
		Format:           stringPtr(m.Format),
		FormatInput:      stringPtr(m.FormatInput),
		DashboardID:      stringPtr(m.DashboardID),
	}}
	in.Tags = keyValues(ctx, m.Tags, diags)
	in.NotifierIDs = stringSet(ctx, m.NotifierIDs, diags)
	return in
}

func (m *triggerModel) fill(t *client.Trigger, diags *diag.Diagnostics) {
	m.ID = types.StringValue(t.ID)
	m.Kind = types.StringValue(t.Kind)
	m.Name = types.StringValue(t.Name)
	m.Description = optionalString(t.Description)
	m.MetricName = types.StringValue(t.MetricName)
	m.Field = types.StringValue(strings.ToLower(t.Field))
	m.ComparisonOperator = types.StringValue(t.ThresholdCondition.ComparisonOperator)
	for op, enum := range comparisonOps {
		if enum == t.ThresholdCondition.ComparisonOperator {
			m.ComparisonOperator = types.StringValue(op)
		}
	}
	m.Threshold = types.Float64Value(t.ThresholdCondition.Value)
	m.WarmupDuration = types.Int64Value(t.WarmupDuration)
	m.CooldownDuration = types.Int64Value(t.CooldownDuration)
	m.NoMatchIsZero = types.BoolValue(t.NoMatchIsZero)
	m.Format = optionalString(t.Format)
	m.FormatInput = optionalString(t.FormatInput)
	m.DashboardID = optionalString(t.DashboardID)
	m.Tags = keyValueMap(t.Tags, diags)
	m.NotifierIDs = notifierIDSet(t.Notifiers, diags)
}

func (r *triggerResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan triggerModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	in := plan.input(ctx, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	t, err := r.client.CreateTrigger(ctx, plan.AppID.ValueString(), "", in)
	if err != nil {
		resp.Diagnostics.AddError("Creating AppSignal trigger", err.Error())
		return
	}
	plan.fill(t, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *triggerResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state triggerModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	t, err := r.client.GetTrigger(ctx, state.AppID.ValueString(), state.ID.ValueString())
	if client.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Reading AppSignal trigger", err.Error())
		return
	}
	state.fill(t, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *triggerResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state triggerModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	in := plan.input(ctx, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	t, err := r.client.CreateTrigger(ctx, plan.AppID.ValueString(), state.ID.ValueString(), in)
	if err != nil {
		resp.Diagnostics.AddError("Updating AppSignal trigger", err.Error())
		return
	}
	plan.fill(t, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *triggerResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state triggerModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.client.ArchiveTrigger(ctx, state.AppID.ValueString(), state.ID.ValueString())
	if err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Archiving AppSignal trigger", err.Error())
	}
}

func (r *triggerResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	appID, id, ok := splitImportID(req.ID)
	if !ok {
		resp.Diagnostics.AddError("Invalid import ID", "Expected <app_id>/<trigger_id>, got "+req.ID)
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("app_id"), appID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), id)...)
}

// keyValues turns a map into the API's key/value list, sorted so requests are stable.
func keyValues(ctx context.Context, m types.Map, diags *diag.Diagnostics) []client.KeyValue {
	if m.IsNull() || m.IsUnknown() {
		return nil
	}
	values := map[string]string{}
	diags.Append(m.ElementsAs(ctx, &values, false)...)
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]client.KeyValue, 0, len(keys))
	for _, k := range keys {
		v := values[k]
		out = append(out, client.KeyValue{Key: k, Value: &v})
	}
	return out
}

func keyValueMap(kvs []client.KeyValue, diags *diag.Diagnostics) types.Map {
	if len(kvs) == 0 {
		return types.MapNull(types.StringType)
	}
	values := map[string]attr.Value{}
	for _, kv := range kvs {
		v := ""
		if kv.Value != nil {
			v = *kv.Value
		}
		values[kv.Key] = types.StringValue(v)
	}
	m, d := types.MapValue(types.StringType, values)
	diags.Append(d...)
	return m
}

func stringSet(ctx context.Context, s types.Set, diags *diag.Diagnostics) []string {
	if s.IsNull() || s.IsUnknown() {
		return nil
	}
	var out []string
	diags.Append(s.ElementsAs(ctx, &out, false)...)
	sort.Strings(out)
	return out
}

func notifierIDSet(notifiers []client.Notifier, diags *diag.Diagnostics) types.Set {
	ids := make([]string, 0, len(notifiers))
	for _, n := range notifiers {
		ids = append(ids, n.ID)
	}
	return stringValueSet(ids, diags)
}

func stringValueSet(values []string, diags *diag.Diagnostics) types.Set {
	elems := make([]attr.Value, 0, len(values))
	for _, v := range values {
		elems = append(elems, types.StringValue(v))
	}
	s, d := types.SetValue(types.StringType, elems)
	diags.Append(d...)
	return s
}
