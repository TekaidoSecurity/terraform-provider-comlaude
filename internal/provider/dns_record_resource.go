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
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ resource.Resource = &DnsRecordResource{}
var _ resource.ResourceWithImportState = &DnsRecordResource{}
var _ resource.ResourceWithValidateConfig = &DnsRecordResource{}

// recordTypes is the v1 enum: the 10 standard types plus MXDUMMY.
// REDIRECT is deferred (see the map's Out of scope).
var recordTypes = []string{"A", "AAAA", "CAA", "CNAME", "DS", "NS", "PTR", "TXT", "MX", "MXDUMMY", "SRV"}

// typeSpecific names which optional attributes each record type may carry;
// anything not listed for the type is rejected at plan time.
var typeSpecific = map[string][]string{
	"MX":      {"priority"},
	"MXDUMMY": {"priority"},
	"SRV":     {"priority", "weight", "port"},
	"CAA":     {"flags", "tag"},
	"DS":      {"digest_type", "key_tag", "algorithm"},
}

// NewDnsRecordResource builds the comlaude_dns_record resource.
func NewDnsRecordResource() resource.Resource {
	return &DnsRecordResource{}
}

// DnsRecordResource manages one Resource Record in a Zone.
type DnsRecordResource struct {
	data *ProviderData
}

type DnsRecordResourceModel struct {
	GroupID    types.String `tfsdk:"group_id"`
	ZoneID     types.String `tfsdk:"zone_id"`
	Name       types.String `tfsdk:"name"`
	Type       types.String `tfsdk:"type"`
	TTL        types.Int64  `tfsdk:"ttl"`
	Value      types.String `tfsdk:"value"`
	Priority   types.Int64  `tfsdk:"priority"`
	Weight     types.Int64  `tfsdk:"weight"`
	Port       types.Int64  `tfsdk:"port"`
	Flags      types.Int64  `tfsdk:"flags"`
	Tag        types.String `tfsdk:"tag"`
	DigestType types.Int64  `tfsdk:"digest_type"`
	KeyTag     types.Int64  `tfsdk:"key_tag"`
	Algorithm  types.Int64  `tfsdk:"algorithm"`
	ID         types.String `tfsdk:"id"`
	FQDN       types.String `tfsdk:"fqdn"`
	Locked     types.Bool   `tfsdk:"locked"`
}

func (r *DnsRecordResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_dns_record"
}

func (r *DnsRecordResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a single DNS resource record in a zone. " +
			"**If the zone is active, every change here is delegated to live DNS.** " +
			"Names are written relative to the zone (`\"www\"`), with `\"@\"` for the zone apex; " +
			"the `fqdn` attribute carries the fully qualified form the API uses.",
		Attributes: map[string]schema.Attribute{
			"group_id": schema.StringAttribute{
				MarkdownDescription: "Group the zone belongs to. Defaults to the provider's group.",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplaceIfConfigured(),
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"zone_id": schema.StringAttribute{
				MarkdownDescription: "Zone holding the record. Typically `data.comlaude_domain.x.active_zone_id`.",
				Required:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Record name relative to the zone (`\"www\"`), or `\"@\"` for the apex.",
				Required:            true,
				Validators:          []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"type": schema.StringAttribute{
				MarkdownDescription: "Record type: " + strings.Join(recordTypes, ", ") + ".",
				Required:            true,
				Validators:          []validator.String{stringvalidator.OneOf(recordTypes...)},
			},
			"ttl": schema.Int64Attribute{
				MarkdownDescription: "Time to live in seconds (1–604800).",
				Required:            true,
				Validators:          []validator.Int64{int64validator.Between(1, 604800)},
			},
			"value": schema.StringAttribute{
				MarkdownDescription: "Record value; format depends on the type.",
				Required:            true,
			},
			"priority": schema.Int64Attribute{
				MarkdownDescription: "Only for MX, MXDUMMY, SRV.",
				Optional:            true,
			},
			"weight": schema.Int64Attribute{
				MarkdownDescription: "Only for SRV.",
				Optional:            true,
			},
			"port": schema.Int64Attribute{
				MarkdownDescription: "Only for SRV.",
				Optional:            true,
			},
			"flags": schema.Int64Attribute{
				MarkdownDescription: "Only for CAA; 0 or 128.",
				Optional:            true,
				Validators:          []validator.Int64{int64validator.OneOf(0, 128)},
			},
			"tag": schema.StringAttribute{
				MarkdownDescription: "Only for CAA.",
				Optional:            true,
				Validators:          []validator.String{stringvalidator.OneOf("issue", "issuewild", "iodef", "contactemail")},
			},
			"digest_type": schema.Int64Attribute{
				MarkdownDescription: "Only for DS; 1, 2 or 4.",
				Optional:            true,
				Validators:          []validator.Int64{int64validator.OneOf(1, 2, 4)},
			},
			"key_tag": schema.Int64Attribute{
				MarkdownDescription: "Only for DS; 0–99999.",
				Optional:            true,
				Validators:          []validator.Int64{int64validator.Between(0, 99999)},
			},
			"algorithm": schema.Int64Attribute{
				MarkdownDescription: "Only for DS; one of 5, 7, 8, 10, 12, 13, 14, 15, 16.",
				Optional:            true,
				Validators:          []validator.Int64{int64validator.OneOf(5, 7, 8, 10, 12, 13, 14, 15, 16)},
			},
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Record id.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"fqdn": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Fully qualified record name as the API stores it.",
			},
			"locked": schema.BoolAttribute{
				Computed:            true,
				MarkdownDescription: "Whether Com Laude has locked the record.",
			},
		},
	}
}

// ValidateConfig rejects type-foreign attributes at plan time.
func (r *DnsRecordResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var model DnsRecordResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &model)...)
	if resp.Diagnostics.HasError() || model.Type.IsUnknown() || model.Type.IsNull() {
		return
	}
	recType := model.Type.ValueString()
	allowed := map[string]bool{}
	for _, a := range typeSpecific[recType] {
		allowed[a] = true
	}

	set := map[string]bool{
		"priority":    !model.Priority.IsNull(),
		"weight":      !model.Weight.IsNull(),
		"port":        !model.Port.IsNull(),
		"flags":       !model.Flags.IsNull(),
		"tag":         !model.Tag.IsNull(),
		"digest_type": !model.DigestType.IsNull(),
		"key_tag":     !model.KeyTag.IsNull(),
		"algorithm":   !model.Algorithm.IsNull(),
	}
	for attr, isSet := range set {
		if isSet && !allowed[attr] {
			resp.Diagnostics.AddAttributeError(path.Root(attr),
				"Attribute not valid for this record type",
				fmt.Sprintf("%q cannot be set on a %s record.", attr, recType))
		}
	}
}

func (r *DnsRecordResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

// fqdnFor builds the wire name from a relative name; relativeFor inverts it.
func fqdnFor(name, domain string) string {
	if name == "@" {
		return domain
	}
	return name + "." + domain
}

func relativeFor(fqdn, domain string) string {
	if fqdn == domain {
		return "@"
	}
	return strings.TrimSuffix(fqdn, "."+domain)
}

func (r *DnsRecordResource) groupID(model *DnsRecordResourceModel) string {
	if !model.GroupID.IsNull() && !model.GroupID.IsUnknown() && model.GroupID.ValueString() != "" {
		return model.GroupID.ValueString()
	}
	return r.data.GroupID
}

func optInt(v types.Int64) *int64 {
	if v.IsNull() || v.IsUnknown() {
		return nil
	}
	val := v.ValueInt64()
	return &val
}

func (r *DnsRecordResource) input(model *DnsRecordResourceModel, domain string) client.RecordInput {
	return client.RecordInput{
		Name:     fqdnFor(model.Name.ValueString(), domain),
		Type:     model.Type.ValueString(),
		TTL:      model.TTL.ValueInt64(),
		Value:    model.Value.ValueString(),
		Priority: optInt(model.Priority),
		Weight:   optInt(model.Weight),
		Port:     optInt(model.Port),
		Flags:    optInt(model.Flags),
		Tag:      model.Tag.ValueString(),
		Digest:   optInt(model.DigestType),
		KeyTag:   optInt(model.KeyTag),
		Algo:     optInt(model.Algorithm),
	}
}

// hydrate copies API state into the model (computed fields + drift-prone ones).
func (r *DnsRecordResource) hydrate(model *DnsRecordResourceModel, rec client.Record) {
	domain := rec.Zone.Domain.Name
	model.ID = types.StringValue(rec.ID)
	model.FQDN = types.StringValue(rec.Name)
	model.Locked = types.BoolValue(rec.Locked != 0)
	model.Name = types.StringValue(relativeFor(rec.Name, domain))
	model.Type = types.StringValue(rec.Type)
	model.TTL = types.Int64Value(rec.TTL)
	model.Value = types.StringValue(rec.Value)
	setOpt := func(dst *types.Int64, v *int64) {
		if v != nil {
			*dst = types.Int64Value(*v)
		} else {
			*dst = types.Int64Null()
		}
	}
	setOpt(&model.Priority, rec.Priority)
	setOpt(&model.Weight, rec.Weight)
	setOpt(&model.Port, rec.Port)
	setOpt(&model.Flags, rec.Flags)
	setOpt(&model.DigestType, rec.Digest)
	setOpt(&model.KeyTag, rec.KeyTag)
	setOpt(&model.Algorithm, rec.Algo)
	if rec.Tag != "" {
		model.Tag = types.StringValue(rec.Tag)
	} else {
		model.Tag = types.StringNull()
	}
}

func (r *DnsRecordResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var model DnsRecordResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &model)...)
	if resp.Diagnostics.HasError() {
		return
	}
	groupID := r.groupID(&model)
	zoneID := model.ZoneID.ValueString()

	domain, err := r.data.Client.ResolveZoneDomain(ctx, groupID, zoneID)
	if err != nil {
		resp.Diagnostics.AddError("Could not resolve the zone's domain", err.Error())
		return
	}

	id, err := r.data.Client.CreateRecord(ctx, groupID, zoneID, r.input(&model, domain))
	if err != nil {
		resp.Diagnostics.AddError("Record creation failed", err.Error())
		return
	}

	rec, err := r.data.Client.GetRecord(ctx, groupID, zoneID, id)
	if err != nil {
		resp.Diagnostics.AddError("Record created but could not be read back", err.Error())
		return
	}
	model.GroupID = types.StringValue(groupID)
	r.hydrate(&model, rec)
	resp.Diagnostics.Append(resp.State.Set(ctx, &model)...)
}

func (r *DnsRecordResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var model DnsRecordResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &model)...)
	if resp.Diagnostics.HasError() {
		return
	}
	rec, err := r.data.Client.GetRecord(ctx, r.groupID(&model), model.ZoneID.ValueString(), model.ID.ValueString())
	if errors.Is(err, client.ErrNotFound) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Record read failed", err.Error())
		return
	}
	r.hydrate(&model, rec)
	resp.Diagnostics.Append(resp.State.Set(ctx, &model)...)
}

func (r *DnsRecordResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var model DnsRecordResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &model)...)
	if resp.Diagnostics.HasError() {
		return
	}
	groupID := r.groupID(&model)
	zoneID := model.ZoneID.ValueString()

	domain, err := r.data.Client.ResolveZoneDomain(ctx, groupID, zoneID)
	if err != nil {
		resp.Diagnostics.AddError("Could not resolve the zone's domain", err.Error())
		return
	}
	if err := r.data.Client.UpdateRecord(ctx, groupID, zoneID, model.ID.ValueString(), r.input(&model, domain)); err != nil {
		resp.Diagnostics.AddError("Record update failed", err.Error())
		return
	}
	rec, err := r.data.Client.GetRecord(ctx, groupID, zoneID, model.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Record updated but could not be read back", err.Error())
		return
	}
	model.GroupID = types.StringValue(groupID)
	r.hydrate(&model, rec)
	resp.Diagnostics.Append(resp.State.Set(ctx, &model)...)
}

func (r *DnsRecordResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var model DnsRecordResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &model)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.data.Client.DeleteRecord(ctx, r.groupID(&model), model.ZoneID.ValueString(), model.ID.ValueString())
	if err != nil && !errors.Is(err, client.ErrNotFound) {
		resp.Diagnostics.AddError("Record deletion failed", err.Error())
	}
}

// ImportState accepts <group_id>/<zone_id>/<record_id>.
func (r *DnsRecordResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts := strings.Split(req.ID, "/")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		resp.Diagnostics.AddError("Invalid import id",
			fmt.Sprintf("Expected <group_id>/<zone_id>/<record_id>, got %q.", req.ID))
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("group_id"), parts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("zone_id"), parts[1])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), parts[2])...)
}
