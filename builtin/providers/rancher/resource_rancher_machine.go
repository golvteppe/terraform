package rancher

import (
	"fmt"
	"log"
	"time"

	rancherClient "github.com/golvteppe/go-rancher/client"
	"github.com/hashicorp/terraform/helper/resource"
	"github.com/hashicorp/terraform/helper/schema"
	"github.com/mitchellh/mapstructure"
)

func resourceRancherMachine() *schema.Resource {
	return &schema.Resource{
		Create: resourceRancherMachineCreate,
		Read:   resourceRancherMachineRead,
		Update: resourceRancherMachineUpdate,
		Delete: resourceRancherMachineDelete,
		Importer: &schema.ResourceImporter{
			State: resourceRancherMachineImport,
		},

		Schema: map[string]*schema.Schema{
			"id": &schema.Schema{
				Type:     schema.TypeString,
				Computed: true,
			},
			"name": &schema.Schema{
				Type:     schema.TypeString,
				Required: true,
			},
			"description": &schema.Schema{
				Type:     schema.TypeString,
				Optional: true,
			},
			"environment_id": &schema.Schema{
				Type:     schema.TypeString,
				Required: true,
				ForceNew: true,
			},
			"labels": &schema.Schema{
				Type:     schema.TypeMap,
				Optional: true,
			},
			"image": &schema.Schema{
				Type:     schema.TypeString,
				Optional: true,
			},
			"driver_config": &schema.Schema{
				Type:     schema.TypeMap,
				Optional: true,
			},
			"driver": &schema.Schema{
				Type:     schema.TypeString,
				Optional: true,
			},
		},
	}
}

func resourceRancherMachineCreate(d *schema.ResourceData, meta interface{}) error {
	log.Printf("[INFO] Creating Machine: %s", d.Id())
	client, err := meta.(*Config).EnvironmentClient(d.Get("environment_id").(string))
	if err != nil {
		return err
	}

	name := d.Get("name").(string)
	description := d.Get("description").(string)
	driver := d.Get("driver").(string)
	driverConfigData := d.Get("driver_config").(map[string]interface{})

	var (
		digitaloceanConfig  rancherClient.DigitaloceanConfig
		vmwarevsphereConfig rancherClient.VmwarevsphereConfig
		amazonec2Config     rancherClient.Amazonec2Config
		azureConfig         rancherClient.AzureConfig
	)

	machineData := map[string]interface{}{
		"name":        &name,
		"description": &description,
	}

	switch driver {
	case "digitalocean":
		mapstructure.Decode(driverConfigData, &digitaloceanConfig)
		machineData["digitaloceanConfig"] = &digitaloceanConfig
	case "vmwarevsphere":
		mapstructure.Decode(driverConfigData, &vmwarevsphereConfig)
		machineData["vmwarevsphereConfig"] = &vmwarevsphereConfig
	case "aws":
		mapstructure.Decode(driverConfigData, &amazonec2Config)
		machineData["amazonec2Config"] = &amazonec2Config
	case "azure":
		mapstructure.Decode(driverConfigData, &azureConfig)
		machineData["azureConfig"] = &azureConfig
	default:
		return fmt.Errorf("Invalid driver specified: %s", err)
	}

	var newMachine rancherClient.Machine
	if err := client.Create("machine", machineData, &newMachine); err != nil {
		return err
	}

	stateConf := &resource.StateChangeConf{
		Pending:    []string{"creating", "provisioning", "bootstrapping", "active"},
		Target:     []string{"active"},
		Refresh:    MachineStateRefreshFunc(client, newMachine.Id),
		Timeout:    10 * time.Minute,
		Delay:      1 * time.Second,
		MinTimeout: 3 * time.Second,
	}
	_, waitErr := stateConf.WaitForState()
	if waitErr != nil {
		return fmt.Errorf(
			"Error waiting for machine (%s) to be created: %s", newMachine.Id, waitErr)
	}

	d.SetId(newMachine.Id)
	log.Printf("[INFO] Machine ID: %s", d.Id())

	return resourceRancherMachineRead(d, meta)
}

func resourceRancherMachineRead(d *schema.ResourceData, meta interface{}) error {
	log.Printf("[INFO] Refreshing Machine: %s", d.Id())
	client, err := meta.(*Config).EnvironmentClient(d.Get("environment_id").(string))
	if err != nil {
		return err
	}

	machine, err := client.Machine.ById(d.Id())
	if err != nil {
		return err
	}

	if machine == nil {
		log.Printf("[INFO] Machine %s not found", d.Id())
		d.SetId("")
		return nil
	}

	if removed(machine.State) {
		log.Printf("[INFO] Machine %s was removed on %v", d.Id(), machine.Removed)
		d.SetId("")
		return nil
	}

	log.Printf("[INFO] Machine Name: %s", machine.Name)

	d.Set("description", machine.Description)
	d.Set("name", machine.Name)
	d.Set("environment_id", machine.AccountId)

	return nil
}

func resourceRancherMachineUpdate(d *schema.ResourceData, meta interface{}) error {
	client, err := meta.(*Config).EnvironmentClient(d.Get("environment_id").(string))
	if err != nil {
		return err
	}

	machine, err := client.Machine.ById(d.Id())
	if err != nil {
		return err
	}

	name := d.Get("name").(string)
	description := d.Get("description").(string)

	machine.Name = name
	machine.Description = description
	client.Machine.Update(machine, &machine)

	return resourceRancherMachineRead(d, meta)
}

func resourceRancherMachineDelete(d *schema.ResourceData, meta interface{}) error {
	log.Printf("[INFO] Deleting Machine: %s", d.Id())
	id := d.Id()
	client, err := meta.(*Config).EnvironmentClient(d.Get("environment_id").(string))
	if err != nil {
		return err
	}

	reg, err := client.Machine.ById(id)
	if err != nil {
		return err
	}

	if _, err := client.Machine.ActionRemove(reg); err != nil {
		return fmt.Errorf("Error removing Machine: %s", err)
	}

	log.Printf("[DEBUG] Waiting for machine (%s) to be removed", id)

	stateConf := &resource.StateChangeConf{
		Pending:    []string{"active", "removed", "removing"},
		Target:     []string{"removed"},
		Refresh:    MachineStateRefreshFunc(client, id),
		Timeout:    10 * time.Minute,
		Delay:      1 * time.Second,
		MinTimeout: 3 * time.Second,
	}

	_, waitErr := stateConf.WaitForState()
	if waitErr != nil {
		return fmt.Errorf(
			"Error waiting for machine (%s) to be removed: %s", id, waitErr)
	}

	d.SetId("")
	return nil
}

func resourceRancherMachineImport(d *schema.ResourceData, meta interface{}) ([]*schema.ResourceData, error) {
	envID, resourceID := splitID(d.Id())
	d.SetId(resourceID)
	if envID != "" {
		d.Set("environment_id", envID)
	} else {
		client, err := meta.(*Config).GlobalClient()
		if err != nil {
			return []*schema.ResourceData{}, err
		}
		machine, err := client.Machine.ById(d.Id())
		if err != nil {
			return []*schema.ResourceData{}, err
		}
		d.Set("environment_id", machine.AccountId)
	}
	return []*schema.ResourceData{d}, nil
}

// MachineStateRefreshFunc returns a resource.StateRefreshFunc that is used to watch
// a Rancher Machine
func MachineStateRefreshFunc(client *rancherClient.RancherClient, machineID string) resource.StateRefreshFunc {
	return func() (interface{}, string, error) {
		env, err := client.Machine.ById(machineID)

		if err != nil {
			return nil, "", err
		}

		return env, env.State, nil
	}
}
