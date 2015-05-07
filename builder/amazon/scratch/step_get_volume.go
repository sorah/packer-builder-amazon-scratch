package scratch

import (
	"fmt"
	"github.com/mitchellh/goamz/ec2"
	"github.com/mitchellh/multistep"
	"github.com/mitchellh/packer/packer"
)

type StepGetVolume struct {
	DeviceName string
}

func (step *StepGetVolume) Run(state multistep.StateBag) multistep.StepAction {
	ui := state.Get("ui").(packer.Ui)
	instance := state.Get("instance").(*ec2.Instance)

	for _, blockDevice := range instance.BlockDevices {
		if step.DeviceName == blockDevice.DeviceName {
			state.Put("volume_id", blockDevice.VolumeId)
			return multistep.ActionContinue
		}
	}

	err := fmt.Errorf("couldn't find target EBS")
	state.Put("error", err)
	ui.Error(err.Error())
	return multistep.ActionHalt
}

func (step *StepGetVolume) Cleanup(state multistep.StateBag) {
	return
}
