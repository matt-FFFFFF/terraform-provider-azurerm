package azurerm

import (
	"fmt"
	"net/http"
	"time"

	"github.com/Azure/azure-sdk-for-go/services/dns/mgmt/2018-05-01/dns"
	"github.com/hashicorp/terraform-plugin-sdk/helper/schema"
	"github.com/terraform-providers/terraform-provider-azurerm/azurerm/helpers/azure"
	"github.com/terraform-providers/terraform-provider-azurerm/azurerm/helpers/tf"
	"github.com/terraform-providers/terraform-provider-azurerm/azurerm/internal/features"
	"github.com/terraform-providers/terraform-provider-azurerm/azurerm/internal/tags"
	"github.com/terraform-providers/terraform-provider-azurerm/azurerm/internal/timeouts"
	"github.com/terraform-providers/terraform-provider-azurerm/azurerm/utils"
)

func resourceArmDnsAAAARecord() *schema.Resource {
	return &schema.Resource{
		Create: resourceArmDnsAaaaRecordCreateUpdate,
		Read:   resourceArmDnsAaaaRecordRead,
		Update: resourceArmDnsAaaaRecordCreateUpdate,
		Delete: resourceArmDnsAaaaRecordDelete,
		Importer: &schema.ResourceImporter{
			State: schema.ImportStatePassthrough,
		},

		Timeouts: &schema.ResourceTimeout{
			Create: schema.DefaultTimeout(30 * time.Minute),
			Read:   schema.DefaultTimeout(5 * time.Minute),
			Update: schema.DefaultTimeout(30 * time.Minute),
			Delete: schema.DefaultTimeout(30 * time.Minute),
		},

		Schema: map[string]*schema.Schema{
			"name": {
				Type:     schema.TypeString,
				Required: true,
				ForceNew: true,
			},

			"resource_group_name": azure.SchemaResourceGroupName(),

			"zone_name": {
				Type:     schema.TypeString,
				Required: true,
			},

			"records": {
				Type:          schema.TypeSet,
				Optional:      true,
				Elem:          &schema.Schema{Type: schema.TypeString},
				Set:           schema.HashString,
				ConflictsWith: []string{"target_resource_id"},
			},

			"ttl": {
				Type:     schema.TypeInt,
				Required: true,
			},

			"fqdn": {
				Type:     schema.TypeString,
				Computed: true,
			},

			"tags": tags.Schema(),

			"target_resource_id": {
				Type:          schema.TypeString,
				Optional:      true,
				ValidateFunc:  azure.ValidateResourceID,
				ConflictsWith: []string{"records"},
			},
		},
	}
}

func resourceArmDnsAaaaRecordCreateUpdate(d *schema.ResourceData, meta interface{}) error {
	client := meta.(*ArmClient).Dns.RecordSetsClient
	ctx, cancel := timeouts.ForCreateUpdate(meta.(*ArmClient).StopContext, d)
	defer cancel()

	name := d.Get("name").(string)
	resGroup := d.Get("resource_group_name").(string)
	zoneName := d.Get("zone_name").(string)

	if features.ShouldResourcesBeImported() && d.IsNewResource() {
		existing, err := client.Get(ctx, resGroup, zoneName, name, dns.AAAA)
		if err != nil {
			if !utils.ResponseWasNotFound(existing.Response) {
				return fmt.Errorf("Error checking for presence of existing DNS AAAA Record %q (Zone %q / Resource Group %q): %s", name, zoneName, resGroup, err)
			}
		}

		if existing.ID != nil && *existing.ID != "" {
			return tf.ImportAsExistsError("azurerm_dns_aaaa_record", *existing.ID)
		}
	}

	ttl := int64(d.Get("ttl").(int))
	t := d.Get("tags").(map[string]interface{})
	targetResourceId := d.Get("target_resource_id").(string)

	if bool(targetResourceId == "") && bool(len(d.Get("records").(*schema.Set).List()) == 0) {
		return fmt.Errorf("Neither 'records' nor 'target_resource_id' is defined")
	}

	var targetResource dns.SubResource
	if targetResourceId != "" {
		targetResource.ID = utils.String(targetResourceId)
	}

	parameters := dns.RecordSet{
		Name: &name,
		RecordSetProperties: &dns.RecordSetProperties{
			Metadata:       tags.Expand(t),
			TTL:            &ttl,
			AaaaRecords:    expandAzureRmDnsAaaaRecords(d),
			TargetResource: &targetResource,
		},
	}

	eTag := ""
	ifNoneMatch := "" // set to empty to allow updates to records after creation
	if _, err := client.CreateOrUpdate(ctx, resGroup, zoneName, name, dns.AAAA, parameters, eTag, ifNoneMatch); err != nil {
		return fmt.Errorf("Error creating/updating DNS AAAA Record %q (Zone %q / Resource Group %q): %s", name, zoneName, resGroup, err)
	}

	resp, err := client.Get(ctx, resGroup, zoneName, name, dns.AAAA)
	if err != nil {
		return fmt.Errorf("Error retrieving DNS AAAA Record %q (Zone %q / Resource Group %q): %s", name, zoneName, resGroup, err)
	}

	if resp.ID == nil {
		return fmt.Errorf("Cannot read DNS AAAA Record %s (resource group %s) ID", name, resGroup)
	}

	d.SetId(*resp.ID)

	return resourceArmDnsAaaaRecordRead(d, meta)
}

func resourceArmDnsAaaaRecordRead(d *schema.ResourceData, meta interface{}) error {
	dnsClient := meta.(*ArmClient).Dns.RecordSetsClient
	ctx, cancel := timeouts.ForRead(meta.(*ArmClient).StopContext, d)
	defer cancel()

	id, err := azure.ParseAzureResourceID(d.Id())
	if err != nil {
		return err
	}

	resGroup := id.ResourceGroup
	name := id.Path["AAAA"]
	zoneName := id.Path["dnszones"]

	resp, err := dnsClient.Get(ctx, resGroup, zoneName, name, dns.AAAA)
	if err != nil {
		if utils.ResponseWasNotFound(resp.Response) {
			d.SetId("")
			return nil
		}
		return fmt.Errorf("Error reading DNS AAAA record %s: %v", name, err)
	}

	targetResource := resp.TargetResource

	d.Set("name", name)
	d.Set("resource_group_name", resGroup)
	d.Set("zone_name", zoneName)
	d.Set("ttl", resp.TTL)
	d.Set("fqdn", resp.Fqdn)
	d.Set("target_resource_id", targetResource.ID)

	// Only flatten DNS records if they are present in the resource, e.g. not for alias records
	if resp.AaaaRecords != nil {
		if err := d.Set("records", flattenAzureRmDnsAaaaRecords(resp.AaaaRecords)); err != nil {
			return err
		}
	}
	return tags.FlattenAndSet(d, resp.Metadata)
}

func resourceArmDnsAaaaRecordDelete(d *schema.ResourceData, meta interface{}) error {
	dnsClient := meta.(*ArmClient).Dns.RecordSetsClient
	ctx, cancel := timeouts.ForDelete(meta.(*ArmClient).StopContext, d)
	defer cancel()

	id, err := azure.ParseAzureResourceID(d.Id())
	if err != nil {
		return err
	}

	resGroup := id.ResourceGroup
	name := id.Path["AAAA"]
	zoneName := id.Path["dnszones"]

	resp, err := dnsClient.Delete(ctx, resGroup, zoneName, name, dns.AAAA, "")
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Error deleting DNS AAAA Record %s: %+v", name, err)
	}

	return nil
}

func flattenAzureRmDnsAaaaRecords(records *[]dns.AaaaRecord) []string {
	results := make([]string, 0, len(*records))

	if records != nil {
		for _, record := range *records {
			results = append(results, *record.Ipv6Address)
		}
	}

	return results
}

func expandAzureRmDnsAaaaRecords(d *schema.ResourceData) *[]dns.AaaaRecord {
	recordStrings := d.Get("records").(*schema.Set).List()
	records := make([]dns.AaaaRecord, len(recordStrings))

	for i, v := range recordStrings {
		ipv6 := v.(string)
		records[i] = dns.AaaaRecord{
			Ipv6Address: &ipv6,
		}
	}

	return &records
}
