// Copyright (c) Tekaido Security
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ datasource.DataSource = &DomainDataSource{}

// NewDomainDataSource builds the comlaude_domain data source.
func NewDomainDataSource() datasource.DataSource {
	return &DomainDataSource{}
}

// DomainDataSource looks up an existing Domain by exact name. Domains are
// read-only in v1: their lifecycle runs through Domain Orders, out of scope.
type DomainDataSource struct {
	data *ProviderData
}

type DomainDataSourceModel struct {
	Name                  types.String `tfsdk:"name"`
	GroupID               types.String `tfsdk:"group_id"`
	ID                    types.String `tfsdk:"id"`
	AccountID             types.String `tfsdk:"account_id"`
	AccountName           types.String `tfsdk:"account_name"`
	ManagementStatus      types.String `tfsdk:"management_status"`
	RegisteredAt          types.String `tfsdk:"registered_at"`
	ExpiresAt             types.String `tfsdk:"expires_at"`
	TLD                   types.String `tfsdk:"tld"`
	DNSSEC                types.Bool   `tfsdk:"dnssec"`
	Nameservers           types.List   `tfsdk:"nameservers"`
	ActiveZoneID          types.String `tfsdk:"active_zone_id"`
	ActiveZoneTTL         types.Int64  `tfsdk:"active_zone_ttl"`
	ActiveZoneRecordCount types.Int64  `tfsdk:"active_zone_record_count"`
}

func (d *DomainDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_domain"
}

func (d *DomainDataSource) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Looks up an existing domain by exact name. `active_zone_id` is the usual " +
			"way to address the zone that serves live DNS: " +
			"`zone_id = data.comlaude_domain.main.active_zone_id`.",
		Attributes: map[string]schema.Attribute{
			"name": schema.StringAttribute{
				MarkdownDescription: "Domain name to look up (exact match).",
				Required:            true,
			},
			"group_id": schema.StringAttribute{
				MarkdownDescription: "Group to search in. Defaults to the provider's group.",
				Optional:            true,
			},
			"id":                schema.StringAttribute{Computed: true, MarkdownDescription: "Domain id."},
			"account_id":        schema.StringAttribute{Computed: true, MarkdownDescription: "Id of the account holding the domain."},
			"account_name":      schema.StringAttribute{Computed: true, MarkdownDescription: "Name of the account holding the domain."},
			"management_status": schema.StringAttribute{Computed: true, MarkdownDescription: "Management status (e.g. auto_renew_enabled)."},
			"registered_at":     schema.StringAttribute{Computed: true, MarkdownDescription: "Registration timestamp."},
			"expires_at":        schema.StringAttribute{Computed: true, MarkdownDescription: "Expiry timestamp."},
			"tld":               schema.StringAttribute{Computed: true, MarkdownDescription: "Top-level domain extension."},
			"dnssec":            schema.BoolAttribute{Computed: true, MarkdownDescription: "Whether DNSSEC is enabled."},
			"nameservers": schema.ListAttribute{
				Computed:            true,
				ElementType:         types.StringType,
				MarkdownDescription: "Delegated nameserver hostnames.",
			},
			"active_zone_id":           schema.StringAttribute{Computed: true, MarkdownDescription: "Id of the domain's active zone (empty if none)."},
			"active_zone_ttl":          schema.Int64Attribute{Computed: true, MarkdownDescription: "Default record TTL of the active zone."},
			"active_zone_record_count": schema.Int64Attribute{Computed: true, MarkdownDescription: "Number of records in the active zone."},
		},
	}
}

func (d *DomainDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	data, ok := req.ProviderData.(*ProviderData)
	if !ok {
		resp.Diagnostics.AddError("Unexpected Data Source Configure Type",
			fmt.Sprintf("Expected *ProviderData, got: %T.", req.ProviderData))
		return
	}
	d.data = data
}

func (d *DomainDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var model DomainDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &model)...)
	if resp.Diagnostics.HasError() {
		return
	}

	groupID := d.data.GroupID
	if !model.GroupID.IsNull() && model.GroupID.ValueString() != "" {
		groupID = model.GroupID.ValueString()
	}

	domain, err := d.data.Client.FindDomainByName(ctx, groupID, model.Name.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Domain lookup failed", err.Error())
		return
	}

	model.ID = types.StringValue(domain.ID)
	model.AccountID = types.StringValue(domain.Account.ID)
	model.AccountName = types.StringValue(domain.Account.Name)
	model.ManagementStatus = types.StringValue(domain.ManagementStatus)
	model.RegisteredAt = types.StringValue(domain.RegisteredAt)
	model.ExpiresAt = types.StringValue(domain.ExpiresAt)
	model.TLD = types.StringValue(domain.TLD)
	model.DNSSEC = types.BoolValue(domain.DNSSEC)

	ns, diags := types.ListValueFrom(ctx, types.StringType, domain.Nameservers.Names)
	resp.Diagnostics.Append(diags...)
	model.Nameservers = ns

	if domain.ActiveZone != nil {
		model.ActiveZoneID = types.StringValue(domain.ActiveZone.ID)
		model.ActiveZoneTTL = types.Int64Value(domain.ActiveZone.DefaultRecordTTL)
		model.ActiveZoneRecordCount = types.Int64Value(domain.ActiveZone.ResourceRecordCount)
	} else {
		model.ActiveZoneID = types.StringValue("")
		model.ActiveZoneTTL = types.Int64Value(0)
		model.ActiveZoneRecordCount = types.Int64Value(0)
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &model)...)
}
