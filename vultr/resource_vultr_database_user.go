package vultr

import (
	"context"
	"log"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/validation"
	"github.com/vultr/govultr/v3"
)

func resourceVultrDatabaseUser() *schema.Resource {
	return &schema.Resource{
		CreateContext: resourceVultrDatabaseUserCreate,
		ReadContext:   resourceVultrDatabaseUserRead,
		UpdateContext: resourceVultrDatabaseUserUpdate,
		DeleteContext: resourceVultrDatabaseUserDelete,
		Importer: &schema.ResourceImporter{
			StateContext: schema.ImportStatePassthroughContext,
		},
		Schema: map[string]*schema.Schema{
			// Required
			"database_id": {
				Type:         schema.TypeString,
				Required:     true,
				ValidateFunc: validation.NoZeroValues,
				ForceNew:     true,
			},
			"username": {
				Type:     schema.TypeString,
				Required: true,
			},
			// Optional
			"password": {
				Type:     schema.TypeString,
				Optional: true,
				Computed: true,
			},
			"encryption": {
				Type:     schema.TypeString,
				Optional: true,
				Computed: true,
			},
			"permission": {
				Type:     schema.TypeString,
				Optional: true,
				Computed: true,
			},
			"access_control": {
				Type:     schema.TypeSet,
				Optional: true,
				Computed: true,
				MaxItems: 1,
				Elem: &schema.Resource{
					Schema: userACLSchema(),
				},
			},
			// Computed
			"access_key": {
				Type:     schema.TypeString,
				Computed: true,
			},
			"access_cert": {
				Type:     schema.TypeString,
				Computed: true,
			},
		},
	}
}

func resourceVultrDatabaseUserCreate(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	client := meta.(*Client).govultrClient()

	databaseID := d.Get("database_id").(string)

	req := &govultr.DatabaseUserCreateReq{
		Username:   d.Get("username").(string),
		Password:   d.Get("password").(string),
		Encryption: d.Get("encryption").(string),
		Permission: d.Get("permission").(string),
	}

	log.Printf("[INFO] Creating database user")
	databaseUser, _, err := client.Database.CreateUser(ctx, databaseID, req)
	if err != nil {
		return diag.Errorf("error creating database user: %v", err)
	}

	d.SetId(databaseUser.Username)

	// Redis/Valkey user access control can only be updated after creation
	if accessControl, accessControlOK := d.GetOk("access_control"); accessControlOK {
		if err := updateUserACL(ctx, client, databaseID, d, accessControl); err != nil {
			return diag.Errorf("error updating user access control: %v", err)
		}
	}

	return resourceVultrDatabaseUserRead(ctx, d, meta)
}

func resourceVultrDatabaseUserRead(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	client := meta.(*Client).govultrClient()

	databaseID := d.Get("database_id").(string)

	databaseUser, _, err := client.Database.GetUser(ctx, databaseID, d.Id())
	if err != nil {
		return diag.Errorf("error getting database user (%s): %v", d.Id(), err)
	}

	if err := d.Set("username", databaseUser.Username); err != nil {
		return diag.Errorf("unable to set resource database user `username` read value: %v", err)
	}

	if err := d.Set("password", databaseUser.Password); err != nil {
		return diag.Errorf("unable to set resource database user `password` read value: %v", err)
	}

	if databaseUser.Encryption != "" {
		var encryptionRaw string
		switch databaseUser.Encryption {
		case "Legacy (MySQL 5.x)":
			encryptionRaw = "mysql_native_password"
		default:
			encryptionRaw = "caching_sha2_password"
		}
		if err := d.Set("encryption", encryptionRaw); err != nil {
			return diag.Errorf("unable to set resource database user `encryption` read value: %v", err)
		}
	}

	if err := d.Set("permission", databaseUser.Permission); err != nil {
		return diag.Errorf("unable to set resource database user `permission` read value: %v", err)
	}

	if err := d.Set("access_key", databaseUser.AccessKey); err != nil {
		return diag.Errorf("unable to set resource database user `access_key` read value: %v", err)
	}

	if err := d.Set("access_cert", databaseUser.AccessCert); err != nil {
		return diag.Errorf("unable to set resource database user `access_cert` read value: %v", err)
	}

	if databaseUser.AccessControl != nil {
		if err := d.Set("access_control", flattenUserACL(databaseUser)); err != nil {
			return diag.Errorf("unable to set resource database user `access_control` read value: %v", err)
		}
	}

	return nil
}

func resourceVultrDatabaseUserUpdate(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	client := meta.(*Client).govultrClient()

	databaseID := d.Get("database_id").(string)

	if d.HasChange("password") {
		log.Printf("[INFO] Updating Password")
		_, newVal := d.GetChange("password")
		password := newVal.(string)
		req := &govultr.DatabaseUserUpdateReq{
			Password: password,
		}
		if _, _, err := client.Database.UpdateUser(ctx, databaseID, d.Id(), req); err != nil {
			return diag.Errorf("error updating database user %s : %s", d.Id(), err.Error())
		}
	}

	if d.HasChange("access_control") {
		_, accessControl := d.GetChange("access_control")
		if err := updateUserACL(ctx, client, databaseID, d, accessControl); err != nil {
			return diag.Errorf("error updating user access control: %v", err)
		}
	}

	if d.HasChange("permission") {
		log.Printf("[INFO] Updating Permission")
		_, newVal := d.GetChange("permission")
		permission := newVal.(string)
		req2 := &govultr.DatabaseUserACLReq{
			Permission: permission,
		}
		if _, _, err := client.Database.UpdateUserACL(ctx, databaseID, d.Id(), req2); err != nil {
			return diag.Errorf("error updating user permission %s : %s", d.Id(), err.Error())
		}
	}

	return resourceVultrDatabaseUserRead(ctx, d, meta)
}

func resourceVultrDatabaseUserDelete(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	client := meta.(*Client).govultrClient()
	log.Printf("[INFO] Deleting database user (%s)", d.Id())

	databaseID := d.Get("database_id").(string)

	if err := client.Database.DeleteUser(ctx, databaseID, d.Id()); err != nil {
		return diag.Errorf("error destroying database user %s : %v", d.Id(), err)
	}

	return nil
}

func updateUserACL(ctx context.Context, client *govultr.Client, databaseID string, d *schema.ResourceData, accessControl interface{}) error { //nolint:lll
	// This should only loop once due to MaxItems: 1 in the resource definition
	for _, v := range accessControl.(*schema.Set).List() {
		var req = &govultr.DatabaseUserACLReq{}
		var aclCategories, aclChannels, aclCommands, aclKeys []string
		obj := v.(map[string]interface{})

		for _, r := range obj["acl_categories"].(*schema.Set).List() {
			aclCategories = append(aclCategories, r.(string))
		}
		req.ACLCategories = &aclCategories

		for _, r := range obj["acl_channels"].(*schema.Set).List() {
			aclChannels = append(aclChannels, r.(string))
		}
		req.ACLChannels = &aclChannels

		for _, r := range obj["acl_commands"].(*schema.Set).List() {
			aclCommands = append(aclCommands, r.(string))
		}
		req.ACLCommands = &aclCommands

		for _, r := range obj["acl_keys"].(*schema.Set).List() {
			aclKeys = append(aclKeys, r.(string))
		}
		req.ACLKeys = &aclKeys

		log.Printf("[INFO] Updating user access control")
		if _, _, err := client.Database.UpdateUserACL(ctx, databaseID, d.Id(), req); err != nil {
			return err
		}
	}

	return nil
}
