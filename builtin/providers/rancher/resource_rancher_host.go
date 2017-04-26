package rancher

import (
	"fmt"
	"log"
	"time"

	rancher "github.com/golvteppe/go-rancher/v2"
	"github.com/hashicorp/terraform/helper/resource"
	"github.com/hashicorp/terraform/helper/schema"
	"github.com/mitchellh/mapstructure"
)

// ro_labels are used internally by Rancher
// They are not documented and should not be set in Terraform
var ro_labels = []string{
	"io.rancher.host.agent_image",
	"io.rancher.host.docker_version",
	"io.rancher.host.kvm",
	"io.rancher.host.linux_kernel_version",
}

func resourceRancherHost() *schema.Resource {
	return &schema.Resource{
		Create: resourceRancherHostCreate,
		Read:   resourceRancherHostRead,
		Update: resourceRancherHostUpdate,
		Delete: resourceRancherHostDelete,
		Importer: &schema.ResourceImporter{
			State: schema.ImportStatePassthrough,
		},

		Schema: map[string]*schema.Schema{
			"id": &schema.Schema{
				Type:     schema.TypeString,
				Computed: true,
			},
			"description": &schema.Schema{
				Type:     schema.TypeString,
				Optional: true,
			},
			"environment_id": &schema.Schema{
				Type:     schema.TypeString,
				Required: true,
			},
			"hostname": &schema.Schema{
				Type:     schema.TypeString,
				Required: true,
			},
			"labels": {
				Type:     schema.TypeMap,
				Optional: true,
			},
			"driver": {
				Type:     schema.TypeString,
				Optional: true,
			},
			"driver_config": {
				Type:     schema.TypeMap,
				Optional: true,
			},
		},
	}
}

func resourceRancherHostCreate(d *schema.ResourceData, meta interface{}) error {
	log.Printf("[INFO][rancher] Creating Host: %s", d.Id())
	client, err := meta.(*Config).EnvironmentClient(d.Get("environment_id").(string))
	if err != nil {
		return err
	}

	hostname := d.Get("hostname").(string)
	description := d.Get("description").(string)
	labels := d.Get("labels").(map[string]interface{})
	driver := d.Get("driver").(string)
	driverConfigData := d.Get("driver_config").(map[string]interface{})

	var (
		digitaloceanConfig  rancher.DigitaloceanConfig
		vmwarevsphereConfig rancher.VmwarevsphereConfig
	)

	hostData := map[string]interface{}{
		"hostname":    &hostname,
		"description": &description,
		"labels":      &labels,
	}

	switch driver {
	case "digitalocean":
		mapstructure.Decode(driverConfigData, &digitaloceanConfig)
		hostData["digitaloceanConfig"] = &digitaloceanConfig
	case "vmwarevsphere":
		mapstructure.Decode(driverConfigData, &vmwarevsphereConfig)
		hostData["vmwarevsphereConfig"] = &vmwarevsphereConfig
	default:
		return fmt.Errorf("Invalid driver specified: %s", err)
	}

	var newHost rancher.Host
	if err := client.Create("host", hostData, &newHost); err != nil {
		return err
	}

	stateConf := &resource.StateChangeConf{
		Pending:    []string{"creating", "provisioning", "bootstrapping", "active", "activating"},
		Target:     []string{"active"},
		Refresh:    HostStateRefreshFunc(client, newHost.Id),
		Timeout:    10 * time.Minute,
		Delay:      1 * time.Second,
		MinTimeout: 3 * time.Second,
	}
	_, waitErr := stateConf.WaitForState()
	if waitErr != nil {
		return fmt.Errorf(
			"Error waiting for host (%s) to be created: %s", newHost.Id, waitErr)
	}

	d.SetId(newHost.Id)
	log.Printf("[INFO] Host ID: %s", d.Id())

	return resourceRancherHostRead(d, meta)
}

func resourceRancherHostRead(d *schema.ResourceData, meta interface{}) error {
	log.Printf("[INFO] Refreshing Host: %s", d.Id())
	client, err := meta.(*Config).EnvironmentClient(d.Get("environment_id").(string))
	if err != nil {
		return err
	}

	host, err := client.Host.ById(d.Id())
	if err != nil {
		return err
	}

	log.Printf("[INFO] Host Name: %s", host.Name)

	d.Set("description", host.Description)
	d.Set("name", host.Name)
	d.Set("hostname", host.Hostname)

	labels := host.Labels
	// Remove read-only labels
	for _, lbl := range ro_labels {
		delete(labels, lbl)
	}
	d.Set("labels", host.Labels)

	return nil
}

func resourceRancherHostUpdate(d *schema.ResourceData, meta interface{}) error {
	log.Printf("[INFO] Updating Host: %s", d.Id())
	client, err := meta.(*Config).EnvironmentClient(d.Get("environment_id").(string))
	if err != nil {
		return err
	}

	name := d.Get("name").(string)
	description := d.Get("description").(string)

	// Process labels: merge ro_labels into new labels
	labels := d.Get("labels").(map[string]interface{})
	host, err := client.Host.ById(d.Id())
	if err != nil {
		return err
	}
	for _, lbl := range ro_labels {
		labels[lbl] = host.Labels[lbl]
	}

	data := map[string]interface{}{
		"name":        &name,
		"description": &description,
		"labels":      &labels,
	}

	var newHost rancher.Host
	if err := client.Update("host", &host.Resource, data, &newHost); err != nil {
		return err
	}

	return resourceRancherHostRead(d, meta)
}

func resourceRancherHostDelete(d *schema.ResourceData, meta interface{}) error {
	log.Printf("[INFO] Deleting Host: %s", d.Id())
	id := d.Id()
	client, err := meta.(*Config).EnvironmentClient(d.Get("environment_id").(string))
	if err != nil {
		return err
	}

	host, err := client.Host.ById(id)
	if err != nil {
		return err
	}

	// Step 1: Deactivate
	if _, e := client.Host.ActionDeactivate(host); e != nil {
		return fmt.Errorf("Error deactivating Host: %s", err)
	}

	log.Printf("[DEBUG] Waiting for host (%s) to be deactivated", id)

	stateConf := &resource.StateChangeConf{
		Pending:    []string{"active", "inactive", "deactivating"},
		Target:     []string{"inactive"},
		Refresh:    HostStateRefreshFunc(client, id),
		Timeout:    10 * time.Minute,
		Delay:      1 * time.Second,
		MinTimeout: 3 * time.Second,
	}

	_, waitErr := stateConf.WaitForState()
	if waitErr != nil {
		return fmt.Errorf(
			"Error waiting for host (%s) to be deactivated: %s", id, waitErr)
	}

	// Update resource to reflect its state
	host, err = client.Host.ById(id)
	if err != nil {
		return fmt.Errorf("Failed to refresh state of deactivated host (%s): %s", id, err)
	}

	if err := client.Host.Delete(host); err != nil {
		return fmt.Errorf("Error deleting Host: %s", err)
	}

	log.Printf("[DEBUG] Waiting for host (%s) to be removed", id)

	stateConf = &resource.StateChangeConf{
		Pending:    []string{"active", "removed", "removing"},
		Target:     []string{"removed"},
		Refresh:    HostStateRefreshFunc(client, id),
		Timeout:    10 * time.Minute,
		Delay:      1 * time.Second,
		MinTimeout: 3 * time.Second,
	}

	_, waitErr = stateConf.WaitForState()
	if waitErr != nil {
		return fmt.Errorf(
			"Error waiting for host (%s) to be removed: %s", id, waitErr)
	}

	d.SetId("")
	return nil
}

// HostStateRefreshFunc returns a resource.StateRefreshFunc that is used to watch
// a Rancher Host.
func HostStateRefreshFunc(client *rancher.RancherClient, hostID string) resource.StateRefreshFunc {
	return func() (interface{}, string, error) {
		host, err := client.Host.ById(hostID)

		if err != nil {
			return nil, "", err
		}

		return host, host.State, nil
	}
}
