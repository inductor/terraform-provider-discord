package discord

import (
	"fmt"

	"github.com/andersfylling/disgord"
	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/polds/imgbase64"
	"golang.org/x/net/context"
)

func baseServerSchema() map[string]*schema.Schema {
	return map[string]*schema.Schema{
		"region": {
			Type:     schema.TypeString,
			Optional: true,
		},
		"verification_level": {
			Type:     schema.TypeInt,
			Optional: true,
			Default:  0,
			ValidateFunc: func(val interface{}, key string) (warns []string, errors []error) {
				v := val.(int)
				if v > 4 || v < 0 {
					errors = append(errors, fmt.Errorf("verification_level must be between 0 and 4 inclusive, got: %d", v))
				}

				return
			},
		},
		"explicit_content_filter": {
			Type:     schema.TypeInt,
			Optional: true,
			Default:  0,
			ValidateFunc: func(val interface{}, key string) (warns []string, errors []error) {
				v := val.(int)
				if v > 2 || v < 0 {
					errors = append(errors, fmt.Errorf("explicit_content_filter must be between 0 and 2 inclusive, got: %d", v))
				}

				return
			},
		},
		"default_message_notifications": {
			Type:     schema.TypeInt,
			Optional: true,
			Default:  0,
			ValidateFunc: func(val interface{}, key string) (warns []string, errors []error) {
				v := val.(int)
				if v != 0 && v != 1 {
					errors = append(errors, fmt.Errorf("default_message_notifications must be 0 or 1, got: %d", v))
				}

				return
			},
		},
		"afk_channel_id": {
			Type:     schema.TypeString,
			Optional: true,
		},
		"afk_timeout": {
			Type:     schema.TypeInt,
			Optional: true,
			Default:  300,
			ValidateFunc: func(val interface{}, key string) (warns []string, errors []error) {
				v := val.(int)
				// See: https://discord.com/developers/docs/resources/guild#guild-object-guild-structure
				expected := []int{60, 300, 900, 1800, 3600}
				if !contains(expected, v) {
					errors = append(errors, fmt.Errorf("afk_timeout must be set to one of the following values: %d, but got: %d", expected, v))
				}

				return
			},
		},
		"icon_url": {
			Type:     schema.TypeString,
			Optional: true,
		},
		"icon_data_uri": {
			Type:     schema.TypeString,
			Optional: true,
		},
		"icon_hash": {
			Type:     schema.TypeString,
			Computed: true,
		},
		"splash_url": {
			Type:     schema.TypeString,
			Optional: true,
		},
		"splash_data_uri": {
			Type:     schema.TypeString,
			Optional: true,
		},
		"splash_hash": {
			Type:     schema.TypeString,
			Computed: true,
		},
		"owner_id": {
			Type:     schema.TypeString,
			Optional: true,
		},
	}
}

func managedServerSchema() map[string]*schema.Schema {
	res := baseServerSchema()

	res["server_id"] = &schema.Schema{
		Type:     schema.TypeString,
		Required: true,
	}
	res["name"] = &schema.Schema{
		Type:     schema.TypeString,
		Optional: true,
	}

	return res
}

func serverSchema() map[string]*schema.Schema {
	res := baseServerSchema()

	res["server_id"] = &schema.Schema{
		Type:     schema.TypeString,
		Computed: true,
	}
	res["name"] = &schema.Schema{
		Type:     schema.TypeString,
		Required: true,
	}

	return res
}

func resourceDiscordServer() *schema.Resource {
	return &schema.Resource{
		CreateContext: resourceServerCreate,
		ReadContext:   resourceServerRead,
		UpdateContext: resourceServerUpdate,
		DeleteContext: resourceServerDelete,
		Importer: &schema.ResourceImporter{
			StateContext: schema.ImportStatePassthroughContext,
		},

		Schema: serverSchema(),
	}
}

func resourceDiscordManagedServer() *schema.Resource {
	return &schema.Resource{
		CreateContext: resourceServerManagedCreate,
		ReadContext:   resourceServerRead,
		UpdateContext: resourceServerUpdate,
		DeleteContext: resourceServerManagedDelete,
		Importer: &schema.ResourceImporter{
			StateContext: schema.ImportStatePassthroughContext,
		},

		Schema: managedServerSchema(),
	}
}

func resourceServerCreate(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
	var diags diag.Diagnostics
	client := m.(*Context).Client

	icon := ""
	if v, ok := d.GetOk("icon_url"); ok {
		icon = imgbase64.FromRemote(v.(string))
	}
	if v, ok := d.GetOk("icon_data_uri"); ok {
		icon = v.(string)
	}

	name := d.Get("name").(string)
	server, err := client.CreateGuild(name, &disgord.CreateGuild{
		Region:                  d.Get("region").(string),
		Icon:                    icon,
		VerificationLvl:         d.Get("verification_level").(int),
		DefaultMsgNotifications: disgord.DefaultMessageNotificationLvl(d.Get("default_message_notifications").(int)),
		ExplicitContentFilter:   disgord.ExplicitContentFilterLvl(d.Get("explicit_content_filter").(int)),
		Channels:                nil,
	})
	if err != nil {
		return diag.Errorf("Failed to create server: %s", err.Error())
	}

	channels, err := client.Guild(server.ID).GetChannels()
	if err != nil {
		return diag.Errorf("Failed to fetch channels for new server: %s", err.Error())
	}

	for _, channel := range channels {
		if _, err := client.Channel(channel.ID).Delete(); err != nil {
			return diag.Errorf("Failed to delete channel for new server: %s", err.Error())
		}
	}

	splash := ""
	edit := false
	if v, ok := d.GetOk("splash_url"); ok {
		splash = imgbase64.FromRemote(v.(string))
	}
	if v, ok := d.GetOk("splash_data_uri"); ok {
		splash = v.(string)
	}
	if splash != "" {
		edit = true
	}

	afkChannel := server.AfkChannelID
	if v, ok := d.GetOk("afk_channel_id"); ok {
		afkChannel = disgord.ParseSnowflakeString(v.(string))
		edit = true
	}
	afkTimeOut := server.AfkTimeout
	if v, ok := d.GetOk("afk_timeout"); ok {
		// The value has been already validated, so this cast is safe.
		afkTimeOut = uint(v.(int))
		edit = true
	}

	if edit {
		if _, err = client.Guild(server.ID).Update(&disgord.UpdateGuild{
			Splash:       &splash,
			AFKChannelID: &afkChannel,
		}); err != nil {
			return diag.Errorf("Failed to edit server: %s", err.Error())
		}
		// FIXME: AFKTimeout doesn't exist in UpdateGuild struct.
		if _, err = client.Guild(server.ID).UpdateBuilder().SetAfkTimeout(int(afkTimeOut)).Execute(); err != nil {
			return diag.Errorf("Failed to edit server: %s", err.Error())
		}
	}

	// Update owner's ID if the specified one is not as same as default,
	// because we will receive "User is already owner" error if update to the same one.
	if v, ok := d.GetOk("owner_id"); ok {
		newOwnerId := disgord.ParseSnowflakeString(v.(string))

		if server.OwnerID != newOwnerId {
			if _, err = client.Guild(server.ID).Update(&disgord.UpdateGuild{
				OwnerID: &newOwnerId,
			}); err != nil {
				return diag.Errorf("Failed to edit server: %s", err.Error())
			}
		}
	}

	d.SetId(server.ID.String())
	if _, ok := d.GetOk("owner_id"); !ok {
		d.Set("owner", server.OwnerID.String())
	}
	d.Set("icon_hash", server.Icon)
	d.Set("splash_hash", server.Splash)

	return diags
}

func resourceServerManagedCreate(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
	var diags diag.Diagnostics

	serverIdInterface, ok := d.GetOk("server_id")
	if !ok {
		return diag.Errorf("Error: server_id must be set")
	}
	serverId, ok := serverIdInterface.(string)
	if !ok {
		return diag.Errorf("Error: server_id must be a string")
	}

	d.SetId(serverId)

	return diags
}

func resourceServerRead(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
	var diags diag.Diagnostics
	client := m.(*Context).Client

	server, err := client.Guild(getId(d.Id())).Get()
	if err != nil {
		return diag.Errorf("Error fetching server: %s", err.Error())
	}

	d.Set("name", server.Name)
	d.Set("region", server.Region)
	d.Set("default_message_notifications", server.DefaultMessageNotifications)
	d.Set("afk_timeout", server.AfkTimeout)
	d.Set("icon_hash", server.Icon)
	d.Set("splash_hash", server.Splash)
	d.Set("verification_level", server.VerificationLevel)
	d.Set("default_message_notifications", server.DefaultMessageNotifications)
	d.Set("explicit_content_filter", server.ExplicitContentFilter)
	if !server.AfkChannelID.IsZero() {
		d.Set("afk_channel_id", server.AfkChannelID.String())
	}

	// We don't want to set the owner to null, should only change this if its changing to something else
	if d.Get("owner_id").(string) != "" && !server.OwnerID.IsZero() {
		d.Set("owner_id", server.OwnerID.String())
	}

	return diags
}

func toString(v interface{}) string {
	return v.(string)
}

func resourceServerUpdate(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
	var diags diag.Diagnostics
	client := m.(*Context).Client

	server, err := client.Guild(getId(d.Id())).Get()
	if err != nil {
		return diag.Errorf("Error fetching server: %s", err.Error())
	}

	// FIXME: Update()に書き換え
	builder := client.Guild(server.ID).UpdateBuilder()
	edit := false

	if d.HasChange("icon_url") {
		builder.SetIcon(imgbase64.FromRemote(d.Get("icon_url").(string)))
		edit = true
	}
	if d.HasChange("icon_data_uri") {
		builder.SetIcon(d.Get("icon_data_uri").(string))
		edit = true
	}
	if d.HasChange("splash_url") {
		builder.SetIcon(imgbase64.FromRemote(d.Get("splash_url").(string)))
		edit = true
	}
	if d.HasChange("splash_data_uri") {
		builder.SetIcon(d.Get("splash_data_uri").(string))
		edit = true
	}
	if d.HasChange("afk_channel_id") {
		builder.SetAfkChannelID(disgord.ParseSnowflakeString(d.Get("afk_channel_id").(string)))
		edit = true
	}
	if d.HasChange("afk_timeout") {
		builder.SetAfkTimeout(d.Get("afk_timeout").(int))
		edit = true
	}

	if d.HasChange("owner_id") {
		builder.SetOwnerID(disgord.ParseSnowflakeString(d.Get("owner_id").(string)))
		edit = true
	}
	if d.HasChange("verification_level") {
		builder.SetVerificationLevel(d.Get("verification_level").(int))
		edit = true
	}

	if d.HasChange("default_message_notifications") {
		builder.SetDefaultMessageNotifications(disgord.DefaultMessageNotificationLvl(d.Get("default_message_notifications").(int)))
		edit = true
	}
	if d.HasChange("explicit_content_filter") {
		builder.SetExplicitContentFilter(disgord.ExplicitContentFilterLvl(d.Get("explicit_content_filter").(int)))
		edit = true
	}
	if d.HasChange("name") {
		builder.SetName(d.Get("name").(string))
		edit = true
	}
	if d.HasChange("region") {
		builder.SetRegion(d.Get("region").(string))
		edit = true
	}

	ownerId, hasOwner := d.GetOk("owner_id")
	if d.HasChange("owner_id") {
		if hasOwner {
			builder.SetOwnerID(getId(ownerId.(string)))
			edit = true
		}
	} else {
		if hasOwner {
			builder.SetOwnerID(server.OwnerID)
			edit = true
		}
	}

	if edit {
		if _, err = builder.Execute(); err != nil {
			return diag.Errorf("Failed to edit server: %s", err.Error())
		}
	}

	return diags
}

func resourceServerDelete(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
	var diags diag.Diagnostics
	client := m.(*Context).Client

	if err := client.Guild(getId(d.Id())).Delete(); err != nil {
		return diag.Errorf("Failed to delete server: %s", err)
	}

	return diags
}

func resourceServerManagedDelete(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
	var diags diag.Diagnostics

	// noop

	return diags
}
