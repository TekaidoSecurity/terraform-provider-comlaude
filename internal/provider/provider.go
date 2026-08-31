// Copyright IBM Corp. 2021, 2025
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"
	"os"

	"github.com/TekaidoSecurity/terraform-provider-comlaude/internal/client"

	"github.com/hashicorp/terraform-plugin-framework/action"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/ephemeral"
	"github.com/hashicorp/terraform-plugin-framework/function"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// Ensure ComlaudeProvider satisfies various provider interfaces.
var _ provider.Provider = &ComlaudeProvider{}
var _ provider.ProviderWithFunctions = &ComlaudeProvider{}
var _ provider.ProviderWithEphemeralResources = &ComlaudeProvider{}
var _ provider.ProviderWithActions = &ComlaudeProvider{}

// ComlaudeProvider defines the provider implementation.
type ComlaudeProvider struct {
	// version is set to the provider version on release, "dev" when the
	// provider is built and ran locally, and "test" when running acceptance
	// testing.
	version string
}

// ComlaudeProviderModel describes the provider data model.
type ComlaudeProviderModel struct {
	Username types.String `tfsdk:"username"`
	Password types.String `tfsdk:"password"`
	APIKey   types.String `tfsdk:"api_key"`
	BaseURL  types.String `tfsdk:"base_url"`
	GroupID  types.String `tfsdk:"group_id"`
}

// ProviderData is what Configure hands to every resource and data source.
type ProviderData struct {
	Client *client.Client
	// GroupID is the resolved default group: explicit attribute, else
	// COMLAUDE_GROUP_ID, else the authenticated profile's group_id.
	GroupID string
}

func (p *ComlaudeProvider) Metadata(ctx context.Context, req provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "comlaude"
	resp.Version = p.version
}

func (p *ComlaudeProvider) Schema(ctx context.Context, req provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Interact with the Com Laude corporate registrar API. " +
			"Authentication uses `POST /api_login`, which requires three credentials " +
			"and a service user with the \"web login\" property enabled.",
		Attributes: map[string]schema.Attribute{
			"username": schema.StringAttribute{
				MarkdownDescription: "Com Laude username (login email). Falls back to `COMLAUDE_USERNAME`.",
				Optional:            true,
				Sensitive:           true,
			},
			"password": schema.StringAttribute{
				MarkdownDescription: "Com Laude password. Falls back to `COMLAUDE_PASSWORD`.",
				Optional:            true,
				Sensitive:           true,
			},
			"api_key": schema.StringAttribute{
				MarkdownDescription: "Com Laude API key (the third `/api_login` credential). Falls back to `COMLAUDE_API_KEY`.",
				Optional:            true,
				Sensitive:           true,
			},
			"base_url": schema.StringAttribute{
				MarkdownDescription: "API base URL. Falls back to `COMLAUDE_BASE_URL`, then `" + client.DefaultBaseURL + "`.",
				Optional:            true,
			},
			"group_id": schema.StringAttribute{
				MarkdownDescription: "Default group (tenant) every resource is scoped under. " +
					"Falls back to `COMLAUDE_GROUP_ID`, then to the authenticated user's own group. " +
					"Individual resources may override it.",
				Optional: true,
			},
		},
	}
}

// resolve returns the attribute value if set, else the environment variable.
func resolve(attr types.String, envVar string) string {
	if !attr.IsNull() && attr.ValueString() != "" {
		return attr.ValueString()
	}
	return os.Getenv(envVar)
}

func (p *ComlaudeProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var data ComlaudeProviderModel

	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	username := resolve(data.Username, "COMLAUDE_USERNAME")
	password := resolve(data.Password, "COMLAUDE_PASSWORD")
	apiKey := resolve(data.APIKey, "COMLAUDE_API_KEY")

	for _, missing := range []struct{ value, attr, env string }{
		{username, "username", "COMLAUDE_USERNAME"},
		{password, "password", "COMLAUDE_PASSWORD"},
		{apiKey, "api_key", "COMLAUDE_API_KEY"},
	} {
		if missing.value == "" {
			resp.Diagnostics.AddError(
				"Missing Com Laude credential",
				fmt.Sprintf("The provider needs %q: set the provider attribute or the %s environment variable.", missing.attr, missing.env),
			)
		}
	}
	if resp.Diagnostics.HasError() {
		return
	}

	baseURL := resolve(data.BaseURL, "COMLAUDE_BASE_URL")
	if baseURL == "" {
		baseURL = client.DefaultBaseURL
	}

	c := client.New(baseURL, username, password, apiKey)

	// Eager login: bad credentials fail here, before any resource work.
	if err := c.Login(ctx); err != nil {
		resp.Diagnostics.AddError(
			"Com Laude login failed",
			"POST /api_login rejected the configured credentials: "+err.Error()+
				"\n\nCheck username/password/api_key, and that the service user has the \"web login\" property enabled.",
		)
		return
	}

	groupID := resolve(data.GroupID, "COMLAUDE_GROUP_ID")
	if groupID == "" {
		profile, err := c.GetProfile(ctx)
		if err != nil {
			resp.Diagnostics.AddError(
				"Could not resolve default group",
				"group_id was not configured and GET /profile failed: "+err.Error(),
			)
			return
		}
		groupID = profile.GroupID
	}

	pd := &ProviderData{Client: c, GroupID: groupID}
	resp.DataSourceData = pd
	resp.ResourceData = pd
	resp.EphemeralResourceData = pd
}

func (p *ComlaudeProvider) Resources(ctx context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewDnsRecordResource,
		NewExampleResource,
	}
}

func (p *ComlaudeProvider) EphemeralResources(ctx context.Context) []func() ephemeral.EphemeralResource {
	return []func() ephemeral.EphemeralResource{
		NewExampleEphemeralResource,
	}
}

func (p *ComlaudeProvider) DataSources(ctx context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		NewDomainDataSource,
		NewExampleDataSource,
	}
}

func (p *ComlaudeProvider) Functions(ctx context.Context) []func() function.Function {
	return []func() function.Function{
		NewExampleFunction,
	}
}

func (p *ComlaudeProvider) Actions(ctx context.Context) []func() action.Action {
	return []func() action.Action{
		NewExampleAction,
	}
}

func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &ComlaudeProvider{
			version: version,
		}
	}
}
