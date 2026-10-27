package provider

import (
	"context"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/mapvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/objectplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"

	"github.com/SevenLabnl/terraform-provider-appsignal/internal/client"
)

var (
	displays   = []string{"line", "area", "area_relative"}
	aggregates = []string{"max", "min", "avg", "sum", "first", "last"}
	layoutType = map[string]attr.Type{"x": types.Int64Type, "y": types.Int64Type, "w": types.Int64Type, "h": types.Int64Type}
)

// visualModel is what the two visual resources share; each has its own schema.
type visualModel interface {
	ids() (appID, dashboardID, id types.String)
	setID(types.String)
	visual(ctx context.Context, diags *diag.Diagnostics) client.Visual
	fill(v *client.Visual, diags *diag.Diagnostics)
}

type visualBase struct {
	ID          types.String `tfsdk:"id"`
	AppID       types.String `tfsdk:"app_id"`
	DashboardID types.String `tfsdk:"dashboard_id"`
	Title       types.String `tfsdk:"title"`
	Description types.String `tfsdk:"description"`
	Format      types.String `tfsdk:"format"`
	FormatInput types.String `tfsdk:"format_input"`
	Layout      types.Object `tfsdk:"layout"`
}

func (b *visualBase) ids() (types.String, types.String, types.String) {
	return b.AppID, b.DashboardID, b.ID
}
func (b *visualBase) setID(id types.String) { b.ID = id }

func (b *visualBase) base(ctx context.Context, typ string, diags *diag.Diagnostics) client.Visual {
	v := client.Visual{
		Type: typ, ID: b.ID.ValueString(), Title: b.Title.ValueString(),
		Description: stringPtr(b.Description), Format: stringPtr(b.Format), FormatInput: stringPtr(b.FormatInput),
	}
	if !b.Layout.IsNull() && !b.Layout.IsUnknown() {
		var l struct {
			X types.Int64 `tfsdk:"x"`
			Y types.Int64 `tfsdk:"y"`
			W types.Int64 `tfsdk:"w"`
			H types.Int64 `tfsdk:"h"`
		}
		diags.Append(b.Layout.As(ctx, &l, basetypes.ObjectAsOptions{})...)
		v.Layout = &client.Layout{X: l.X.ValueInt64(), Y: l.Y.ValueInt64(), W: l.W.ValueInt64(), H: l.H.ValueInt64()}
	}
	return v
}

func (b *visualBase) fillBase(v *client.Visual, diags *diag.Diagnostics) {
	b.ID = types.StringValue(v.ID)
	b.Title = types.StringValue(v.Title)
	b.Description = optionalString(v.Description)
	b.Format = optionalString(v.Format)
	b.FormatInput = optionalString(v.FormatInput)
	if v.Layout == nil {
		b.Layout = types.ObjectNull(layoutType)
		return
	}
	var d diag.Diagnostics
	b.Layout, d = types.ObjectValue(layoutType, map[string]attr.Value{
		"x": types.Int64Value(v.Layout.X), "y": types.Int64Value(v.Layout.Y),
		"w": types.Int64Value(v.Layout.W), "h": types.Int64Value(v.Layout.H),
	})
	diags.Append(d...)
}

func visualBaseAttributes(kind string) map[string]schema.Attribute {
	nonEmpty := []validator.String{stringvalidator.LengthAtLeast(1)}
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	position := []validator.Int64{int64validator.AtLeast(0)}
	size := []validator.Int64{int64validator.AtLeast(1)}
	return map[string]schema.Attribute{
		"id": schema.StringAttribute{
			Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
		},
		"app_id":       schema.StringAttribute{Required: true, PlanModifiers: replace},
		"dashboard_id": schema.StringAttribute{Required: true, PlanModifiers: replace},
		"title":        schema.StringAttribute{Required: true, Validators: nonEmpty},
		"description":  schema.StringAttribute{Optional: true, Validators: nonEmpty},
		"format": schema.StringAttribute{
			Description: "How values are shown: " + strings.Join(valueFormats, ", ") + ". Defaults to `number`.",
			Optional:    true, Computed: true, Default: stringdefault.StaticString("number"),
			Validators: []validator.String{stringvalidator.OneOf(valueFormats...)},
		},
		"format_input": schema.StringAttribute{
			Description: "Unit of the reported value when format is `size`: " + strings.Join(sizeUnits, ", ") + ".",
			Optional:    true, Validators: []validator.String{stringvalidator.OneOf(sizeUnits...)},
		},
		"layout": schema.SingleNestedAttribute{
			Description: "Position and size on the dashboard grid. Left out, the " + kind + " keeps the place " +
				"AppSignal or the UI gives it.",
			Optional: true, Computed: true,
			PlanModifiers: []planmodifier.Object{objectplanmodifier.UseStateForUnknown()},
			Attributes: map[string]schema.Attribute{
				"x": schema.Int64Attribute{Required: true, Validators: position},
				"y": schema.Int64Attribute{Required: true, Validators: position},
				"w": schema.Int64Attribute{Required: true, Validators: size},
				"h": schema.Int64Attribute{Required: true, Validators: size},
			},
		},
	}
}

func metricTagsAttribute() schema.MapAttribute {
	return schema.MapAttribute{
		Description: "Tag filters. Values may be `*` to show every value as its own line.",
		ElementType: types.StringType, Optional: true,
		Validators: []validator.Map{mapvalidator.SizeAtLeast(1)},
	}
}

func validateFormatInput(ctx context.Context, config tfsdk.Config, diags *diag.Diagnostics) {
	var format, formatInput types.String
	diags.Append(config.GetAttribute(ctx, path.Root("format"), &format)...)
	diags.Append(config.GetAttribute(ctx, path.Root("format_input"), &formatInput)...)
	if formatInput.IsNull() || formatInput.IsUnknown() || format.IsUnknown() {
		return
	}
	if format.ValueString() != "size" {
		diags.AddAttributeError(path.Root("format_input"), "format_input needs format = \"size\"",
			"AppSignal only uses format_input for sizes.")
	}
}

// ---------------------------------------------------------------- timeseries

type timeseriesMetricModel struct {
	Name   types.String `tfsdk:"name"`
	Fields types.List   `tfsdk:"fields"`
	Tags   types.Map    `tfsdk:"tags"`
}

type timeseriesModel struct {
	visualBase
	Display        types.String            `tfsdk:"display"`
	DrawNullAsZero types.Bool              `tfsdk:"draw_null_as_zero"`
	LineLabel      types.String            `tfsdk:"line_label"`
	MinYAxis       types.Float64           `tfsdk:"min_y_axis"`
	Metrics        []timeseriesMetricModel `tfsdk:"metrics"`
}

func (m *timeseriesModel) visual(ctx context.Context, diags *diag.Diagnostics) client.Visual {
	v := m.base(ctx, client.TypeTimeseries, diags)
	display := strings.ToUpper(m.Display.ValueString())
	v.Display = &display
	draw := m.DrawNullAsZero.ValueBool()
	v.DrawNullAsZero = &draw
	v.LineLabel = stringPtr(m.LineLabel)
	if !m.MinYAxis.IsNull() && !m.MinYAxis.IsUnknown() {
		floor := m.MinYAxis.ValueFloat64()
		v.MinYAxis = &floor
	}
	for _, mm := range m.Metrics {
		var fields []string
		diags.Append(mm.Fields.ElementsAs(ctx, &fields, false)...)
		metric := client.TimeseriesMetric{Name: mm.Name.ValueString(), Tags: keyValues(ctx, mm.Tags, diags)}
		for _, f := range fields {
			metric.Fields = append(metric.Fields, client.MetricField{Field: strings.ToUpper(f)})
		}
		v.Metrics = append(v.Metrics, metric)
	}
	return v
}

func (m *timeseriesModel) fill(v *client.Visual, diags *diag.Diagnostics) {
	m.fillBase(v, diags)
	m.Display = types.StringValue("line")
	if v.Display != nil {
		m.Display = types.StringValue(strings.ToLower(*v.Display))
	}
	m.DrawNullAsZero = types.BoolValue(v.DrawNullAsZero != nil && *v.DrawNullAsZero)
	m.LineLabel = optionalString(v.LineLabel)
	m.MinYAxis = types.Float64PointerValue(v.MinYAxis)
	m.Metrics = make([]timeseriesMetricModel, 0, len(v.Metrics))
	for _, mm := range v.Metrics {
		fields := make([]attr.Value, 0, len(mm.Fields))
		for _, f := range mm.Fields {
			fields = append(fields, types.StringValue(strings.ToLower(f.Field)))
		}
		list, d := types.ListValue(types.StringType, fields)
		diags.Append(d...)
		m.Metrics = append(m.Metrics, timeseriesMetricModel{
			Name: types.StringValue(mm.Name), Fields: list, Tags: keyValueMap(mm.Tags, diags),
		})
	}
}

func NewTimeseriesResource() resource.Resource {
	return &visualResource{kind: "timeseries", newModel: func() visualModel { return &timeseriesModel{} }}
}

func timeseriesSchema() schema.Schema {
	attrs := visualBaseAttributes("graph")
	attrs["display"] = schema.StringAttribute{
		Description: "Line style: " + strings.Join(displays, ", ") + ". Defaults to `line`.",
		Optional:    true, Computed: true, Default: stringdefault.StaticString("line"),
		Validators: []validator.String{stringvalidator.OneOf(displays...)},
	}
	attrs["draw_null_as_zero"] = schema.BoolAttribute{
		Description: "Draw missing data as 0 instead of a gap. Defaults to true, as in AppSignal.",
		Optional:    true, Computed: true, Default: booldefault.StaticBool(true),
	}
	attrs["line_label"] = schema.StringAttribute{
		Description: "Label of each line, with `%name%`, `%field%` and `%<tag>%` replaced. AppSignal uses `%name%` when unset.",
		Optional:    true, Validators: []validator.String{stringvalidator.LengthAtLeast(1)},
	}
	attrs["min_y_axis"] = schema.Float64Attribute{Description: "Lowest value on the y axis.", Optional: true}
	attrs["metrics"] = schema.ListNestedAttribute{
		Description: "Metrics drawn in the graph, in order.",
		Required:    true, Validators: []validator.List{listvalidator.SizeAtLeast(1)},
		NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
			"name": schema.StringAttribute{Required: true, Validators: []validator.String{stringvalidator.LengthAtLeast(1)}},
			"fields": schema.ListAttribute{
				Description: "Fields to draw: " + strings.Join(metricFields, ", ") + ".",
				ElementType: types.StringType, Required: true,
				Validators: []validator.List{
					listvalidator.SizeAtLeast(1), listvalidator.UniqueValues(),
					listvalidator.ValueStringsAre(stringvalidator.OneOf(metricFields...)),
				},
			},
			"tags": metricTagsAttribute(),
		}},
	}
	return schema.Schema{
		Description: "A graph of one or more metrics over time on an AppSignal dashboard.",
		Attributes:  attrs,
	}
}

// ---------------------------------------------------------------- number

type numberMetricModel struct {
	Name      types.String `tfsdk:"name"`
	Field     types.String `tfsdk:"field"`
	Aggregate types.String `tfsdk:"aggregate"`
	Tags      types.Map    `tfsdk:"tags"`
}

type numberModel struct {
	visualBase
	Metric *numberMetricModel `tfsdk:"metric"`
}

func (m *numberModel) visual(ctx context.Context, diags *diag.Diagnostics) client.Visual {
	v := m.base(ctx, client.TypeNumber, diags)
	if m.Metric != nil {
		v.Metric = &client.NumberMetric{
			Name:      m.Metric.Name.ValueString(),
			Field:     strings.ToUpper(m.Metric.Field.ValueString()),
			Aggregate: strings.ToUpper(m.Metric.Aggregate.ValueString()),
			Tags:      keyValues(ctx, m.Metric.Tags, diags),
		}
	}
	return v
}

func (m *numberModel) fill(v *client.Visual, diags *diag.Diagnostics) {
	m.fillBase(v, diags)
	m.Metric = nil
	if v.Metric != nil {
		m.Metric = &numberMetricModel{
			Name:      types.StringValue(v.Metric.Name),
			Field:     types.StringValue(strings.ToLower(v.Metric.Field)),
			Aggregate: types.StringValue(strings.ToLower(v.Metric.Aggregate)),
			Tags:      keyValueMap(v.Metric.Tags, diags),
		}
	}
}

func NewNumberResource() resource.Resource {
	return &visualResource{kind: "number", newModel: func() visualModel { return &numberModel{} }}
}

func numberSchema() schema.Schema {
	attrs := visualBaseAttributes("tile")
	attrs["metric"] = schema.SingleNestedAttribute{
		Description: "The metric shown, reduced to one value over the selected time range.",
		Required:    true,
		Attributes: map[string]schema.Attribute{
			"name": schema.StringAttribute{Required: true, Validators: []validator.String{stringvalidator.LengthAtLeast(1)}},
			"field": schema.StringAttribute{
				Description: "One of " + strings.Join(metricFields, ", ") + ".",
				Required:    true, Validators: []validator.String{stringvalidator.OneOf(metricFields...)},
			},
			"aggregate": schema.StringAttribute{
				Description: "How the range is reduced: " + strings.Join(aggregates, ", ") + ".",
				Required:    true, Validators: []validator.String{stringvalidator.OneOf(aggregates...)},
			},
			"tags": metricTagsAttribute(),
		},
	}
	return schema.Schema{
		Description: "A single-number tile (Big Number) on an AppSignal dashboard.",
		Attributes:  attrs,
	}
}

// ---------------------------------------------------------------- shared resource

var (
	_ resource.Resource                   = &visualResource{}
	_ resource.ResourceWithConfigure      = &visualResource{}
	_ resource.ResourceWithImportState    = &visualResource{}
	_ resource.ResourceWithValidateConfig = &visualResource{}
)

type visualResource struct {
	kind     string
	newModel func() visualModel
	client   *client.Client
}

func (r *visualResource) typeName() string {
	if r.kind == "number" {
		return client.TypeNumber
	}
	return client.TypeTimeseries
}

func (r *visualResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_dashboard_" + r.kind
}

func (r *visualResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	if r.kind == "number" {
		resp.Schema = numberSchema()
		return
	}
	resp.Schema = timeseriesSchema()
}

func (r *visualResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	validateFormatInput(ctx, req.Config, &resp.Diagnostics)
}

func (r *visualResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFrom(req.ProviderData, &resp.Diagnostics)
}

func (r *visualResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	plan := r.newModel()
	resp.Diagnostics.Append(req.Plan.Get(ctx, plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	appID, dashboardID, _ := plan.ids()
	v := plan.visual(ctx, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	got, err := r.client.CreateVisual(ctx, appID.ValueString(), dashboardID.ValueString(), v)
	if err != nil {
		resp.Diagnostics.AddError("Creating AppSignal dashboard "+r.kind, err.Error())
		return
	}
	plan.fill(got, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *visualResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	state := r.newModel()
	resp.Diagnostics.Append(req.State.Get(ctx, state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	appID, dashboardID, id := state.ids()
	got, err := r.client.GetVisual(ctx, appID.ValueString(), dashboardID.ValueString(), id.ValueString())
	if client.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Reading AppSignal dashboard "+r.kind, err.Error())
		return
	}
	if got.Type != r.typeName() {
		resp.Diagnostics.AddError("Wrong kind of visual",
			"Visual "+id.ValueString()+" is a "+got.Type+", not a "+r.typeName()+". Import it as the matching resource.")
		return
	}
	state.fill(got, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *visualResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	plan, state := r.newModel(), r.newModel()
	resp.Diagnostics.Append(req.Plan.Get(ctx, plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	_, _, id := state.ids()
	plan.setID(id)
	appID, dashboardID, _ := plan.ids()
	v := plan.visual(ctx, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	got, err := r.client.UpdateVisual(ctx, appID.ValueString(), dashboardID.ValueString(), v)
	if err != nil {
		resp.Diagnostics.AddError("Updating AppSignal dashboard "+r.kind, err.Error())
		return
	}
	plan.fill(got, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *visualResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	state := r.newModel()
	resp.Diagnostics.Append(req.State.Get(ctx, state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	appID, _, id := state.ids()
	err := r.client.DeleteVisual(ctx, appID.ValueString(), id.ValueString())
	if err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Deleting AppSignal dashboard "+r.kind, err.Error())
	}
}

func (r *visualResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts := strings.Split(req.ID, "/")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		resp.Diagnostics.AddError("Invalid import ID", "Expected <app_id>/<dashboard_id>/<visual_id>, got "+req.ID)
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("app_id"), parts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("dashboard_id"), parts[1])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), parts[2])...)
}
