package scratch

import (
	"fmt"
	"github.com/mitchellh/goamz/ec2"
	"github.com/mitchellh/multistep"
	awscommon "github.com/mitchellh/packer/builder/amazon/common"
	"github.com/mitchellh/packer/packer"
)

type StepRegisterAMI struct {
	image          *ec2.Image
	RootDeviceName string
}

func (step *StepRegisterAMI) Run(state multistep.StateBag) multistep.StepAction {
	config := state.Get("config").(*Config)
	ec2conn := state.Get("ec2").(*ec2.EC2)
	instance := state.Get("instance").(*ec2.Instance)
	snapshotId := state.Get("snapshot_id").(string)
	ui := state.Get("ui").(packer.Ui)

	step.ensureRootDeviceMapping(snapshotId, config)

	// Create the image
	ui.Say(fmt.Sprintf("Registering the AMI: %s", config.AMIName))
	opts := &ec2.RegisterImage{
		Name:           config.AMIName,
		Description:    config.AMIDescription,
		Architecture:   instance.Architecture,
		BlockDevices:   config.BlockDevices.BuildAMIDevices(),
		RootDeviceName: step.RootDeviceName,
		VirtType:       instance.VirtType,
	}
	if config.AMIEnhancedNetworking {
		opts.SriovNetSupport = "simple"
	}

	registerResp, err := ec2conn.RegisterImage(opts)
	if err != nil {
		err := fmt.Errorf("Error registering AMI: %s", err)
		state.Put("error", err)
		ui.Error(err.Error())
		return multistep.ActionHalt
	}

	// Set the AMI ID in the state
	ui.Message(fmt.Sprintf("AMI: %s", registerResp.ImageId))
	amis := make(map[string]string)
	amis[ec2conn.Region.Name] = registerResp.ImageId
	state.Put("amis", amis)

	// Wait for the image to become ready
	stateChange := awscommon.StateChangeConf{
		Pending:   []string{"pending"},
		Target:    "available",
		Refresh:   awscommon.AMIStateRefreshFunc(ec2conn, registerResp.ImageId),
		StepState: state,
	}

	ui.Say("Waiting for AMI to become ready...")
	if _, err := awscommon.WaitForState(&stateChange); err != nil {
		err := fmt.Errorf("Error waiting for AMI: %s", err)
		state.Put("error", err)
		ui.Error(err.Error())
		return multistep.ActionHalt
	}

	imagesResp, err := ec2conn.Images([]string{registerResp.ImageId}, nil)
	if err != nil {
		err := fmt.Errorf("Error searching for AMI: %s", err)
		state.Put("error", err)
		ui.Error(err.Error())
		return multistep.ActionHalt
	}
	step.image = &imagesResp.Images[0]

	return multistep.ActionContinue
}

func (step *StepRegisterAMI) Cleanup(state multistep.StateBag) {
	if step.image == nil {
		return
	}

	_, cancelled := state.GetOk(multistep.StateCancelled)
	_, halted := state.GetOk(multistep.StateHalted)
	if !cancelled && !halted {
		return
	}

	ec2conn := state.Get("ec2").(*ec2.EC2)
	ui := state.Get("ui").(packer.Ui)

	ui.Say("Deregistering the AMI because cancelation or error...")
	if resp, err := ec2conn.DeregisterImage(step.image.Id); err != nil {
		ui.Error(fmt.Sprintf("Error deregistering AMI, may still be around: %s", err))
		return
	} else if resp.Return == false {
		ui.Error(fmt.Sprintf("Error deregistering AMI, may still be around: %t", resp.Return))
		return
	}
}

func (step *StepRegisterAMI) ensureRootDeviceMapping(snapshotId string, config *Config) {
	for _, blockDevice := range config.BlockDevices.AMIMappings {
		if step.RootDeviceName == blockDevice.DeviceName {
			return
		}
	}

	config.BlockDevices.AMIMappings = append(config.BlockDevices.AMIMappings, awscommon.BlockDevice{
		DeleteOnTermination: true,
		DeviceName:          step.RootDeviceName,
		SnapshotId:          snapshotId,
		VolumeSize:          config.VolumeSize,
		VolumeType:          config.VolumeType,
	})
}
