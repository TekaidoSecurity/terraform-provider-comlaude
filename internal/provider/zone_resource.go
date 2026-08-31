// Copyright (c) Tekaido Security
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/TekaidoSecurity/terraform-provider-comlaude/internal/client"
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ resource.Resource = &ZoneResource{}
var _ resource.ResourceWithImportState = &ZoneResource{}

// NewZoneResource builds the comlaude_zone resource.
func NewZoneResource() resource.Resource {
	return &ZoneResource{}
}

// ZoneResource manages a DNS Zone on a Domain.
type ZoneResource struct {
	data *ProviderData
}

type ZoneResourceModel struct {
	GroupID          types.String `tfsdk:"group_id"`
	DomainID         types.String `tfsdk:"domain_id"`
	Supplier         types.String `tfsdk:"supplier"`
	SupplierID       types.String `tfsdk:"supplier_id"`
	DefaultRecordTTL types.Int64  `tfsdk:"default_record_ttl"`
	Active           types.Bool   `tfsdk:"active"`
	ID               types.String `tfsdk:"id"`
	Signed           types.Bool   `tfsdk:"signed"`
	Networks         types.List   `tfsdk:"networks"`
}

func (r *ZoneResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_zone"
}

func (r *ZoneResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a DNS zone on a domain. A domain holds at most one **active** zone " +
			"(the one serving live DNS): **activating a zone deactivates the domain's other zones**, which " +
			"will drift until their next refresh. Zones are created inactive; activation is an explicit " +
			"`active = true`. An active zone cannot be destroyed — set `active = false` and apply first, " +
			"then destroy (deliberate two-step teardown, so an accidental destroy can never stop live DNS). " +
			"Zone create and delete require the `zone-manager` role.",
		Attributes: map[string]schema.Attribute{
			"group_id": schema.StringAttribute{
				MarkdownDescription: "Group the domain belongs to. Defaults to the provider's group.",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplaceIfConfigured(),
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"domain_id": schema.StringAttribute{
				MarkdownDescription: "Domain the zone belongs to.",
				Required:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"supplier": schema.StringAttribute{
				MarkdownDescription: "DNS supplier for the zone, by name (`\"Com Laude DNS\"`), key " +
					"(`dns_supplier_comlaude`), or id. Usually omitted: when exactly one DNS supplier " +
					"is available for the domain (a domain holds at most one zone per supplier), it is " +
					"chosen automatically; only genuine ambiguity asks for this attribute, and the " +
					"error lists the choices.",
				Optional:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplaceIfConfigured()},
			},
			"supplier_id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Resolved id of the zone's DNS supplier.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"default_record_ttl": schema.Int64Attribute{
				MarkdownDescription: "Default TTL for new records in this zone (1–604800). Changing it does not touch existing records.",
				Optional:            true,
				Computed:            true,
				Validators:          []validator.Int64{int64validator.Between(1, 604800)},
			},
			"active": schema.BoolAttribute{
				MarkdownDescription: "Whether this zone serves live DNS. Defaults to `false`. " +
					"Setting `true` deactivates the domain's other zones.",
				Optional: true,
				Computed: true,
				Default:  booldefault.StaticBool(false),
			},
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Zone id.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"signed": schema.BoolAttribute{
				Computed:            true,
				MarkdownDescription: "Whether the zone is DNSSEC-signed.",
			},
			"networks": schema.ListAttribute{
				Computed:            true,
				ElementType:         types.Int64Type,
				MarkdownDescription: "Networks the zone is published to.",
			},
		},
	}
}

func (r *ZoneResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	data, ok := req.ProviderData.(*ProviderData)
	if !ok {
		resp.Diagnostics.AddError("Unexpected Resource Configure Type",
			fmt.Sprintf("Expected *ProviderData, got: %T.", req.ProviderData))
		return
	}
	r.data = data
}

func (r *ZoneResource) groupID(model *ZoneResourceModel) string {
	if !model.GroupID.IsNull() && !model.GroupID.IsUnknown() && model.GroupID.ValueString() != "" {
		return model.GroupID.ValueString()
	}
	return r.data.GroupID
}

func (r *ZoneResource) input(model *ZoneResourceModel) client.ZoneInput {
	in := client.ZoneInput{}
	if !model.DefaultRecordTTL.IsNull() && !model.DefaultRecordTTL.IsUnknown() {
		v := model.DefaultRecordTTL.ValueInt64()
		in.DefaultRecordTTL = &v
	}
	if !model.Active.IsNull() && !model.Active.IsUnknown() {
		v := model.Active.ValueBool()
		in.Active = &v
	}
	return in
}

func (r *ZoneResource) hydrate(ctx context.Context, model *ZoneResourceModel, z client.Zone) []error {
	model.ID = types.StringValue(z.ID)
	model.Active = types.BoolValue(z.Active)
	if z.Supplier != nil {
		model.SupplierID = types.StringValue(z.Supplier.ID)
	} else if model.SupplierID.IsUnknown() {
		model.SupplierID = types.StringNull()
	}
	model.Signed = types.BoolValue(z.Signed)
	model.DefaultRecordTTL = types.Int64Value(z.DefaultRecordTTL)
	networks := make([]int64, len(z.Networks))
	for i, n := range z.Networks {
		networks[i] = int64(n)
	}
	list, diags := types.ListValueFrom(ctx, types.Int64Type, networks)
	if diags.HasError() {
		return []error{fmt.Errorf("networks conversion failed")}
	}
	model.Networks = list
	return nil
}

// zoneErrorHint appends role/entitlement guidance to zone API failures.
func zoneErrorHint(err error) string {
	msg := err.Error()
	if errors.Is(err, client.ErrPaymentRequired) {
		msg += "\n\nThe zone service entitlement is not enabled for this account; contact Com Laude to enable it."
	}
	if errors.Is(err, client.ErrAuth) {
		msg += "\n\nZone create and delete require the zone-manager role on the service user."
	}
	return msg
}

func (r *ZoneResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var model ZoneResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &model)...)
	if resp.Diagnostics.HasError() {
		return
	}
	groupID := r.groupID(&model)

	in := r.input(&model)
	// The API requires a supplier at creation and allows one zone per
	// supplier per domain (both live-verified). Resolution is automatic when
	// unambiguous; a real choice is surfaced, never guessed.
	supplier, err := r.data.Client.ResolveDNSSupplier(ctx, groupID, model.DomainID.ValueString(), model.Supplier.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Could not determine the DNS supplier for the new zone", err.Error())
		return
	}
	in.SupplierID = supplier.ID

	z, err := r.data.Client.CreateZone(ctx, groupID, model.DomainID.ValueString(), in)
	if err != nil {
		resp.Diagnostics.AddError("Zone creation failed", zoneErrorHint(err))
		return
	}
	model.GroupID = types.StringValue(groupID)
	if errs := r.hydrate(ctx, &model, z); len(errs) > 0 {
		resp.Diagnostics.AddError("Zone created but response could not be decoded", errs[0].Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &model)...)
}

func (r *ZoneResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var model ZoneResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &model)...)
	if resp.Diagnostics.HasError() {
		return
	}
	z, err := r.data.Client.GetZone(ctx, r.groupID(&model), model.DomainID.ValueString(), model.ID.ValueString())
	if errors.Is(err, client.ErrNotFound) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Zone read failed", err.Error())
		return
	}
	if errs := r.hydrate(ctx, &model, z); len(errs) > 0 {
		resp.Diagnostics.AddError("Zone response could not be decoded", errs[0].Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &model)...)
}

func (r *ZoneResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var model ZoneResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &model)...)
	if resp.Diagnostics.HasError() {
		return
	}
	groupID := r.groupID(&model)
	domainID := model.DomainID.ValueString()
	zoneID := model.ID.ValueString()

	if err := r.data.Client.UpdateZone(ctx, groupID, domainID, zoneID, r.input(&model)); err != nil {
		resp.Diagnostics.AddError("Zone update failed", zoneErrorHint(err))
		return
	}
	z, err := r.data.Client.GetZone(ctx, groupID, domainID, zoneID)
	if err != nil {
		resp.Diagnostics.AddError("Zone updated but could not be read back", err.Error())
		return
	}
	model.GroupID = types.StringValue(groupID)
	if errs := r.hydrate(ctx, &model, z); len(errs) > 0 {
		resp.Diagnostics.AddError("Zone response could not be decoded", errs[0].Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &model)...)
}

func (r *ZoneResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var model ZoneResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &model)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if model.Active.ValueBool() {
		resp.Diagnostics.AddError(
			"Cannot destroy an active zone",
			"This zone is active: it is serving live DNS for its domain, and destroying it would stop that. "+
				"Set `active = false`, run `terraform apply`, then destroy. "+
				"This two-step teardown is deliberate: an accidental destroy must never take down live DNS.",
		)
		return
	}
	err := r.data.Client.DeleteZone(ctx, r.groupID(&model), model.DomainID.ValueString(), model.ID.ValueString())
	if err != nil && !errors.Is(err, client.ErrNotFound) {
		resp.Diagnostics.AddError("Zone deletion failed", zoneErrorHint(err))
	}
}

// ImportState accepts <group_id>/<domain_id>/<zone_id>.
func (r *ZoneResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts := strings.Split(req.ID, "/")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		resp.Diagnostics.AddError("Invalid import id",
			fmt.Sprintf("Expected <group_id>/<domain_id>/<zone_id>, got %q.", req.ID))
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("group_id"), parts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("domain_id"), parts[1])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), parts[2])...)
}
