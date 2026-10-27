package provider

import (
	"context"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/SevenLabnl/terraform-provider-appsignal/internal/client"
)

var (
	_ resource.Resource                = &appResource{}
	_ resource.ResourceWithConfigure   = &appResource{}
	_ resource.ResourceWithImportState = &appResource{}
)

var (
	appDeleteTimeout = 3 * time.Minute
	appDeletePoll    = 2 * time.Second
)

func NewAppResource() resource.Resource { return &appResource{} }

type appResource struct{ client *client.Client }

type appModel struct {
	ID               types.String `tfsdk:"id"`
	OrganizationSlug types.String `tfsdk:"organization_slug"`
	Name             types.String `tfsdk:"name"`
	Environment      types.String `tfsdk:"environment"`
	PushAPIKey       types.String `tfsdk:"push_api_key"`
	FrontendAPIKey   types.String `tfsdk:"frontend_api_key"`
}

func (r *appResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_app"
}

func (r *appResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	keep := []planmodifier.String{stringplanmodifier.UseStateForUnknown()}
	resp.Schema = schema.Schema{
		Description: "An AppSignal app: one name in one environment. The API cannot rename an app, " +
			"so changing any argument replaces it. Destroying an app deletes all of its data.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{Computed: true, PlanModifiers: keep},
			"organization_slug": schema.StringAttribute{
				Description: "Slug of the organization the app belongs to.", Required: true, PlanModifiers: replace,
			},
			"name":        schema.StringAttribute{Required: true, PlanModifiers: replace},
			"environment": schema.StringAttribute{Required: true, PlanModifiers: replace},
			"push_api_key": schema.StringAttribute{
				Description: "Push API key that integrations and collectors report with.",
				Computed:    true, Sensitive: true, PlanModifiers: keep,
			},
			"frontend_api_key": schema.StringAttribute{
				Description: "Front-end API key for browser error reporting.",
				Computed:    true, Sensitive: true, PlanModifiers: keep,
			},
		},
	}
}

func (r *appResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFrom(req.ProviderData, &resp.Diagnostics)
}

func (m *appModel) fill(a *client.App) {
	m.ID = types.StringValue(a.ID)
	m.Name = types.StringValue(a.Name)
	m.Environment = types.StringValue(a.Environment)
	m.PushAPIKey = types.StringValue(a.PushAPIKey)
	m.FrontendAPIKey = types.StringValue(a.FrontendAPIKey)
	if a.Organization.Slug != "" {
		m.OrganizationSlug = types.StringValue(a.Organization.Slug)
	}
}

func (r *appResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan appModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	app, err := r.client.CreateApp(ctx, plan.OrganizationSlug.ValueString(), plan.Name.ValueString(), plan.Environment.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Creating AppSignal app", err.Error())
		return
	}
	plan.fill(app)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *appResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state appModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	app, err := r.client.GetApp(ctx, state.ID.ValueString())
	if client.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Reading AppSignal app", err.Error())
		return
	}
	state.fill(app)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update is never called with a change, since every argument forces replacement.
func (r *appResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan appModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *appResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state appModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.DeleteApp(ctx, state.ID.ValueString()); err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Deleting AppSignal app", err.Error())
		return
	}
	// AppSignal deletes apps in the background; wait, so a replacement does not meet the old app.
	deadline := time.Now().Add(appDeleteTimeout)
	for time.Now().Before(deadline) {
		_, err := r.client.GetApp(ctx, state.ID.ValueString())
		if client.IsNotFound(err) {
			return
		}
		select {
		case <-ctx.Done():
			resp.Diagnostics.AddError("Deleting AppSignal app", ctx.Err().Error())
			return
		case <-time.After(appDeletePoll):
		}
	}
	resp.Diagnostics.AddWarning("AppSignal app still visible",
		"AppSignal accepted the deletion of app "+state.ID.ValueString()+" but still listed it after "+appDeleteTimeout.String()+".")
}

func (r *appResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
