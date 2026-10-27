// Package provider implements the AppSignal provider for OpenTofu and Terraform.
package provider

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/SevenLabnl/terraform-provider-appsignal/internal/client"
)

var _ provider.Provider = &appsignalProvider{}

type appsignalProvider struct {
	version string
}

func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &appsignalProvider{version: version}
	}
}

type providerModel struct {
	Token    types.String `tfsdk:"token"`
	Endpoint types.String `tfsdk:"endpoint"`
}

func (p *appsignalProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "appsignal"
	resp.Version = p.version
}

func (p *appsignalProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages AppSignal apps, log sources and uptime monitors through the AppSignal GraphQL API.",
		Attributes: map[string]schema.Attribute{
			"token": schema.StringAttribute{
				Description: "AppSignal personal API token. Defaults to the APPSIGNAL_TOKEN environment variable.",
				Optional:    true,
				Sensitive:   true,
			},
			"endpoint": schema.StringAttribute{
				Description: "GraphQL endpoint. Defaults to APPSIGNAL_ENDPOINT, then " + client.DefaultEndpoint + ".",
				Optional:    true,
			},
		},
	}
}

func (p *appsignalProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var cfg providerModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if cfg.Token.IsUnknown() {
		resp.Diagnostics.AddAttributeError(path.Root("token"), "Unknown AppSignal token",
			"The token must be known when the provider is configured.")
		return
	}

	token := os.Getenv("APPSIGNAL_TOKEN")
	if !cfg.Token.IsNull() {
		token = cfg.Token.ValueString()
	}
	if strings.TrimSpace(token) == "" {
		resp.Diagnostics.AddAttributeError(path.Root("token"), "Missing AppSignal token",
			"Set the provider's token argument or the APPSIGNAL_TOKEN environment variable.")
		return
	}
	endpoint := os.Getenv("APPSIGNAL_ENDPOINT")
	if !cfg.Endpoint.IsNull() && !cfg.Endpoint.IsUnknown() {
		endpoint = cfg.Endpoint.ValueString()
	}

	c := client.New(endpoint, token, "terraform-provider-appsignal/"+p.version)
	resp.ResourceData = c
	resp.DataSourceData = c
}

func (p *appsignalProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewAppResource,
		NewLogSourceResource,
		NewUptimeMonitorResource,
	}
}

func (p *appsignalProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		NewOrganizationDataSource,
		NewAppDataSource,
		NewNotifierDataSource,
	}
}

func clientFrom(data any, diags *diag.Diagnostics) *client.Client {
	if data == nil {
		return nil
	}
	c, ok := data.(*client.Client)
	if !ok {
		diags.AddError("Unexpected provider data", fmt.Sprintf("expected *client.Client, got %T", data))
		return nil
	}
	return c
}

// splitImportID parses the "<app_id>/<id>" form used to import objects inside an app.
func splitImportID(id string) (string, string, bool) {
	appID, objID, ok := strings.Cut(id, "/")
	if !ok || appID == "" || objID == "" || strings.Contains(objID, "/") {
		return "", "", false
	}
	return appID, objID, true
}

// optionalString maps an empty API string to null, so an unset attribute stays unset.
func optionalString(s *string) types.String {
	if s == nil || *s == "" {
		return types.StringNull()
	}
	return types.StringValue(*s)
}

func stringPtr(v types.String) *string {
	if v.IsNull() || v.IsUnknown() {
		return nil
	}
	s := v.ValueString()
	return &s
}
