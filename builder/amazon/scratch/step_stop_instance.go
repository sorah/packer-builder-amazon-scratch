package scratch

import (
	"fmt"
	"github.com/mitchellh/goamz/ec2"
	"github.com/mitchellh/multistep"
	awscommon "github.com/mitchellh/packer/builder/amazon/common"
	"github.com/mitchellh/packer/packer"
)

type StepStopInstance struct {
	SpotPrice string
}

func (step *StepStopInstance) Run(state multistep.StateBag) multistep.StepAction {
	ec2conn := state.Get("ec2").(*ec2.EC2)
	instance := state.Get("instance").(*ec2.Instance)
	ui := state.Get("ui").(packer.Ui)

	// Skip when it is a spot instance
	if step.SpotPrice != "" {
		return multistep.ActionContinue
	}

	ui.Say("Stopping the source instance...")
	_, err := ec2conn.StopInstances(instance.InstanceId)
	if err != nil {
		err := fmt.Errorf("Error stopping instance: %s", err)
		state.Put("error", err)
		ui.Error(err.Error())
		return multistep.ActionHalt
	}

	ui.Say("Waiting for the instance to stop...")
	stateChange := awscommon.StateChangeConf{
		Pending:   []string{"running", "stopping"},
		Target:    "stopped",
		Refresh:   awscommon.InstanceStateRefreshFunc(ec2conn, instance),
		StepState: state,
	}
	_, err = awscommon.WaitForState(&stateChange)
	if err != nil {
		err := fmt.Errorf("Error waiting for instance to stop: %s", err)
		state.Put("error", err)
		ui.Error(err.Error())
		return multistep.ActionHalt
	}

	return multistep.ActionContinue
}

func (s *StepStopInstance) Cleanup(multistep.StateBag) {
	// No cleanup...
}
