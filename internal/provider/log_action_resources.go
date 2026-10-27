package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/setdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/SevenLabnl/terraform-provider-appsignal/internal/client"
)

var (
	logMetricTypes = []string{"counter", "gauge", "distribution"}
	logMetricName  = regexp.MustCompile(`^[a-z0-9._]+$`)
)

type logActionBase struct {
	ID        types.String `tfsdk:"id"`
	AppID     types.String `tfsdk:"app_id"`
	Name      types.String `tfsdk:"name"`
	Query     types.String `tfsdk:"query"`
	SourceIDs types.Set    `tfsdk:"source_ids"`
}

type logFilterModel struct {
	logActionBase
}

type logMetricItemModel struct {
	Name  types.String `tfsdk:"name"`
	Type  types.String `tfsdk:"type"`
	Field types.String `tfsdk:"field"`
	Tags  types.Map    `tfsdk:"tags"`
}

type logMetricModel struct {
	logActionBase
	DropLines types.Bool           `tfsdk:"drop_lines"`
	Metrics   []logMetricItemModel `tfsdk:"metrics"`
}

type logActionModel interface {
	base() *logActionBase
	action(ctx context.Context, diags *diag.Diagnostics) client.LogAction
	fill(a *client.LogAction, diags *diag.Diagnostics)
}

func (m *logFilterModel) base() *logActionBase { return &m.logActionBase }
func (m *logMetricModel) base() *logActionBase { return &m.logActionBase }

func (b *logActionBase) action(ctx context.Context, typ string, diags *diag.Diagnostics) client.LogAction {
	return client.LogAction{
		Type: typ, ID: b.ID.ValueString(), Name: b.Name.ValueString(), Query: b.Query.ValueString(),
		SourceIDs: stringSet(ctx, b.SourceIDs, diags),
	}
}

func (b *logActionBase) fillBase(a *client.LogAction, diags *diag.Diagnostics) {
	b.ID = types.StringValue(a.ID)
	b.Name = types.StringValue(a.Name)
	b.Query = types.StringValue(a.Query)
	b.SourceIDs = stringValueSet(a.SourceIDs, diags)
}

func (m *logFilterModel) action(ctx context.Context, diags *diag.Diagnostics) client.LogAction {
	return m.logActionBase.action(ctx, client.ActionFilter, diags)
}

func (m *logFilterModel) fill(a *client.LogAction, diags *diag.Diagnostics) { m.fillBase(a, diags) }

func (m *logMetricModel) action(ctx context.Context, diags *diag.Diagnostics) client.LogAction {
	a := m.logActionBase.action(ctx, client.ActionMetrics, diags)
	a.DropLines = m.DropLines.ValueBool()
	for _, item := range m.Metrics {
		tags := map[string]string{}
		if !item.Tags.IsNull() && !item.Tags.IsUnknown() {
			diags.Append(item.Tags.ElementsAs(ctx, &tags, false)...)
		}
		metric := client.LogMetric{
			Name: item.Name.ValueString(), MetricType: strings.ToUpper(item.Type.ValueString()),
			Field: stringPtr(item.Field), Tags: map[string]any{},
		}
		for k, v := range tags {
			metric.Tags[k] = v
		}
		a.Metrics = append(a.Metrics, metric)
	}
	return a
}

func (m *logMetricModel) fill(a *client.LogAction, diags *diag.Diagnostics) {
	m.fillBase(a, diags)
	m.DropLines = types.BoolValue(a.DropLines)
	m.Metrics = make([]logMetricItemModel, 0, len(a.Metrics))
	for _, metric := range a.Metrics {
		item := logMetricItemModel{
			Name:  types.StringValue(metric.Name),
			Type:  types.StringValue(strings.ToLower(metric.MetricType)),
			Field: optionalString(metric.Field),
			Tags:  types.MapNull(types.StringType),
		}
		if len(metric.Tags) > 0 {
			tags := map[string]attr.Value{}
			for k, v := range metric.Tags {
				s, ok := v.(string)
				if !ok {
					// A tag set in the UI to something other than text is shown as its JSON.
					raw, _ := json.Marshal(v)
					s = string(raw)
				}
				tags[k] = types.StringValue(s)
			}
			var d diag.Diagnostics
			item.Tags, d = types.MapValue(types.StringType, tags)
			diags.Append(d...)
		}
		m.Metrics = append(m.Metrics, item)
	}
}

func logActionBaseAttributes() map[string]schema.Attribute {
	nonEmpty := []validator.String{stringvalidator.LengthAtLeast(1)}
	return map[string]schema.Attribute{
		"id": schema.StringAttribute{
			Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
		},
		"app_id": schema.StringAttribute{
			Required: true, PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
		},
		"name": schema.StringAttribute{Required: true, Validators: nonEmpty},
		"query": schema.StringAttribute{
			Description: "Log query selecting the lines. AppSignal does not validate it, so check it in the log viewer first.",
			Required:    true, Validators: nonEmpty,
		},
		"source_ids": schema.SetAttribute{
			Description: "Log sources the action applies to. Empty means every source of the app.",
			ElementType: types.StringType, Optional: true, Computed: true,
			Default: setdefault.StaticValue(types.SetValueMust(types.StringType, nil)),
		},
	}
}

func NewLogFilterResource() resource.Resource {
	return &logActionResource{kind: "filter", typ: client.ActionFilter, newModel: func() logActionModel { return &logFilterModel{} }}
}

func NewLogMetricResource() resource.Resource {
	return &logActionResource{kind: "metric", typ: client.ActionMetrics, newModel: func() logActionModel { return &logMetricModel{} }}
}

var (
	_ resource.Resource                   = &logActionResource{}
	_ resource.ResourceWithConfigure      = &logActionResource{}
	_ resource.ResourceWithImportState    = &logActionResource{}
	_ resource.ResourceWithValidateConfig = &logActionResource{}
)

type logActionResource struct {
	kind     string
	typ      string
	newModel func() logActionModel
	client   *client.Client
}

func (r *logActionResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_log_" + r.kind
}

func (r *logActionResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	attrs := logActionBaseAttributes()
	if r.kind == "filter" {
		resp.Schema = schema.Schema{
			Description: "A log filter in an AppSignal app: matching lines are dropped at ingestion and never stored. " +
				"Log actions run in order and new ones are added last; see appsignal_log_action_order.",
			Attributes: attrs,
		}
		return
	}
	attrs["drop_lines"] = schema.BoolAttribute{
		Description: "Drop the matching lines once the metrics are taken from them. AppSignal does this with a " +
			"filter of its own right after this action. Defaults to false.",
		Optional: true, Computed: true, Default: booldefault.StaticBool(false),
	}
	attrs["metrics"] = schema.ListNestedAttribute{
		Description: "Metrics taken from each matching line. Metric names must be unique within the app.",
		Required:    true, Validators: []validator.List{listvalidator.SizeAtLeast(1)},
		NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
			"name": schema.StringAttribute{
				Description: "Lowercase letters, digits, dots and underscores.",
				Required:    true,
				Validators:  []validator.String{stringvalidator.RegexMatches(logMetricName, "must use only a-z, 0-9, dots and underscores")},
			},
			"type": schema.StringAttribute{
				Description: "One of " + strings.Join(logMetricTypes, ", ") + ".",
				Required:    true, Validators: []validator.String{stringvalidator.OneOf(logMetricTypes...)},
			},
			"field": schema.StringAttribute{
				Description: "Line attribute holding the value. Required for gauge and distribution metrics.",
				Optional:    true, Validators: []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"tags": schema.MapAttribute{
				Description: "Tags added to the metric.", ElementType: types.StringType, Optional: true,
			},
		}},
	}
	resp.Schema = schema.Schema{
		Description: "A log metrics action in an AppSignal app: it turns matching lines into counter, gauge or " +
			"distribution metrics. Log actions run in order and new ones are added last; see appsignal_log_action_order.",
		Attributes: attrs,
	}
}

func (r *logActionResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	if r.kind != "metric" {
		return
	}
	var metrics []logMetricItemModel
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("metrics"), &metrics)...)
	seen := map[string]bool{}
	for i, m := range metrics {
		p := path.Root("metrics").AtListIndex(i)
		if !m.Name.IsUnknown() && !m.Name.IsNull() {
			if seen[m.Name.ValueString()] {
				resp.Diagnostics.AddAttributeError(p.AtName("name"), "Duplicate metric name",
					fmt.Sprintf("%q is used twice.", m.Name.ValueString()))
			}
			seen[m.Name.ValueString()] = true
		}
		if !m.Type.IsUnknown() && m.Type.ValueString() != "counter" && m.Field.IsNull() {
			resp.Diagnostics.AddAttributeError(p.AtName("field"), "Missing field",
				"A "+m.Type.ValueString()+" metric needs the field that holds its value.")
		}
	}
}

func (r *logActionResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFrom(req.ProviderData, &resp.Diagnostics)
}

func (r *logActionResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	plan := r.newModel()
	resp.Diagnostics.Append(req.Plan.Get(ctx, plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	a := plan.action(ctx, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	got, err := r.client.CreateLogAction(ctx, plan.base().AppID.ValueString(), a)
	if err != nil {
		resp.Diagnostics.AddError("Creating AppSignal log "+r.kind, err.Error())
		return
	}
	plan.fill(got, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *logActionResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	state := r.newModel()
	resp.Diagnostics.Append(req.State.Get(ctx, state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	b := state.base()
	got, err := r.client.GetLogAction(ctx, b.AppID.ValueString(), b.ID.ValueString())
	if client.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Reading AppSignal log "+r.kind, err.Error())
		return
	}
	if got.Type != r.typ {
		resp.Diagnostics.AddError("Wrong kind of log action",
			"Log action "+b.ID.ValueString()+" is a "+got.Type+", not a "+r.typ+".")
		return
	}
	state.fill(got, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *logActionResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	plan, state := r.newModel(), r.newModel()
	resp.Diagnostics.Append(req.Plan.Get(ctx, plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	plan.base().ID = state.base().ID
	a := plan.action(ctx, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	got, err := r.client.UpdateLogAction(ctx, plan.base().AppID.ValueString(), a)
	if err != nil {
		resp.Diagnostics.AddError("Updating AppSignal log "+r.kind, err.Error())
		return
	}
	plan.fill(got, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *logActionResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	state := r.newModel()
	resp.Diagnostics.Append(req.State.Get(ctx, state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	b := state.base()
	err := r.client.DeleteLogAction(ctx, b.AppID.ValueString(), b.ID.ValueString())
	if err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Deleting AppSignal log "+r.kind, err.Error())
	}
}

func (r *logActionResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	appID, id, ok := splitImportID(req.ID)
	if !ok {
		resp.Diagnostics.AddError("Invalid import ID", "Expected <app_id>/<log_action_id>, got "+req.ID)
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("app_id"), appID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), id)...)
}

// ---------------------------------------------------------------- order

var (
	_ resource.Resource                = &logActionOrderResource{}
	_ resource.ResourceWithConfigure   = &logActionOrderResource{}
	_ resource.ResourceWithImportState = &logActionOrderResource{}
)

func NewLogActionOrderResource() resource.Resource { return &logActionOrderResource{} }

type logActionOrderResource struct{ client *client.Client }

type logActionOrderModel struct {
	ID        types.String `tfsdk:"id"`
	AppID     types.String `tfsdk:"app_id"`
	ActionIDs types.List   `tfsdk:"action_ids"`
}

func (r *logActionOrderResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_log_action_order"
}

func (r *logActionOrderResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "The order in which an app's log actions (filters, metrics and log triggers) run. The listed " +
			"actions run first, in this order; the rest follow in their current order. The filter AppSignal adds " +
			"for a metrics action with `drop_lines` stays right behind that action. Use one per app; destroying " +
			"it leaves the order as it is.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description: "The app id.", Computed: true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"app_id": schema.StringAttribute{
				Required: true, PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"action_ids": schema.ListAttribute{
				Description: "Ids of appsignal_log_filter, appsignal_log_metric and appsignal_log_trigger resources.",
				ElementType: types.StringType, Required: true,
				Validators: []validator.List{listvalidator.SizeAtLeast(1), listvalidator.UniqueValues()},
			},
		},
	}
}

func (r *logActionOrderResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFrom(req.ProviderData, &resp.Diagnostics)
}

func (r *logActionOrderResource) apply(ctx context.Context, m *logActionOrderModel, diags *diag.Diagnostics) {
	var ids []string
	diags.Append(m.ActionIDs.ElementsAs(ctx, &ids, false)...)
	if diags.HasError() {
		return
	}
	if err := r.client.ReorderLogActions(ctx, m.AppID.ValueString(), ids); err != nil {
		diags.AddError("Reordering AppSignal log actions", err.Error())
		return
	}
	m.ID = m.AppID
	r.read(ctx, m, ids, diags)
}

// read records the order AppSignal runs the listed actions in. When an unlisted action
// runs among them, it records the actual first actions instead, so the plan shows a change.
func (r *logActionOrderResource) read(ctx context.Context, m *logActionOrderModel, listed []string, diags *diag.Diagnostics) {
	all, err := r.client.ListLogActions(ctx, m.AppID.ValueString())
	if err != nil {
		diags.AddError("Reading AppSignal log actions", err.Error())
		return
	}
	want := map[string]bool{}
	for _, id := range listed {
		want[id] = true
	}
	var running, current []string
	for i, a := range all {
		if client.IsDropFilter(all, i) {
			continue
		}
		running = append(running, a.ID)
		if want[a.ID] {
			current = append(current, a.ID)
		}
	}
	for i := range current {
		if running[i] != current[i] {
			current = running[:len(current)]
			break
		}
	}
	m.ActionIDs = stringValueList(current, diags)
}

func (r *logActionOrderResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan logActionOrderModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	r.apply(ctx, &plan, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *logActionOrderResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state logActionOrderModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var listed []string
	resp.Diagnostics.Append(state.ActionIDs.ElementsAs(ctx, &listed, false)...)
	if state.ActionIDs.IsNull() {
		// After an import the state holds no list yet; take the app's whole order.
		all, err := r.client.ListLogActions(ctx, state.AppID.ValueString())
		if err != nil {
			resp.Diagnostics.AddError("Reading AppSignal log actions", err.Error())
			return
		}
		for i, a := range all {
			if !client.IsDropFilter(all, i) {
				listed = append(listed, a.ID)
			}
		}
	}
	r.read(ctx, &state, listed, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *logActionOrderResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan logActionOrderModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	r.apply(ctx, &plan, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *logActionOrderResource) Delete(context.Context, resource.DeleteRequest, *resource.DeleteResponse) {
}

func (r *logActionOrderResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("app_id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
}

func stringValueList(values []string, diags *diag.Diagnostics) types.List {
	elems := make([]attr.Value, 0, len(values))
	for _, v := range values {
		elems = append(elems, types.StringValue(v))
	}
	l, d := types.ListValue(types.StringType, elems)
	diags.Append(d...)
	return l
}
