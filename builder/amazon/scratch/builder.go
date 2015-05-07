package scratch

import (
	"fmt"
	"log"

	"github.com/mitchellh/goamz/ec2"
	"github.com/mitchellh/multistep"
	"github.com/mitchellh/packer/builder/amazon/chroot"
	awscommon "github.com/mitchellh/packer/builder/amazon/common"
	"github.com/mitchellh/packer/common"
	"github.com/mitchellh/packer/packer"
)

const BuilderId = "sorah.amazon.scratch"

type Config struct {
	common.PackerConfig    `mapstructure:",squash"`
	awscommon.AccessConfig `mapstructure:",squash"`
	awscommon.AMIConfig    `mapstructure:",squash"`
	awscommon.RunConfig    `mapstructure:",squash"`
	awscommon.BlockDevices `mapstructure:",squash"`

	WorkerDeviceName string `mapstructure:"worker_device_name"`
	VolumeSize       int64  `mapstructure:"volume_size"`
	VolumeType       string `mapstructure:"volume_type"`
	RootDeviceName   string `mapstructure:"root_device_name"`

	tpl *packer.ConfigTemplate
}

type wrappedCommandTemplate struct {
	Command string
}

type Builder struct {
	config Config
	runner multistep.Runner
}

func (builder *Builder) Prepare(raws ...interface{}) ([]string, error) {
	md, err := common.DecodeConfig(&builder.config, raws...)
	if err != nil {
		return nil, err
	}

	builder.config.tpl, err = packer.NewConfigTemplate()
	if err != nil {
		return nil, err
	}
	builder.config.tpl.UserVars = builder.config.PackerUserVars
	builder.config.tpl.Funcs(awscommon.TemplateFuncs)

	if builder.config.VolumeSize == 0 {
		builder.config.VolumeSize = 12
	}

	if builder.config.VolumeType == "" {
		builder.config.VolumeType = "standard"
	}

	if builder.config.RootDeviceName == "" {
		builder.config.RootDeviceName = "/dev/xvda"
	}

	builder.ensureWorkerDeviceMapping()

	// Accumulate any errors
	errs := common.CheckUnusedConfig(md)
	errs = packer.MultiErrorAppend(errs, builder.config.AccessConfig.Prepare(builder.config.tpl)...)
	errs = packer.MultiErrorAppend(errs, builder.config.BlockDevices.Prepare(builder.config.tpl)...)
	errs = packer.MultiErrorAppend(errs, builder.config.AMIConfig.Prepare(builder.config.tpl)...)
	errs = packer.MultiErrorAppend(errs, builder.config.RunConfig.Prepare(builder.config.tpl)...)

	templates := map[string]*string{
		"worker_device_name": &builder.config.WorkerDeviceName,
		"root_device_name":   &builder.config.RootDeviceName,
	}

	for n, ptr := range templates {
		var err error
		*ptr, err = builder.config.tpl.Process(*ptr, nil)
		if err != nil {
			errs = packer.MultiErrorAppend(
				errs, fmt.Errorf("Error processing %s: %s", n, err))
		}
	}

	if errs != nil && len(errs.Errors) > 0 {
		return nil, errs
	}

	log.Println(common.ScrubConfig(builder.config, builder.config.AccessKey, builder.config.SecretKey))
	return nil, nil
}

func (builder *Builder) Run(ui packer.Ui, hook packer.Hook, cache packer.Cache) (packer.Artifact, error) {
	region, err := builder.config.Region()
	if err != nil {
		return nil, err
	}

	auth, err := builder.config.AccessConfig.Auth()
	if err != nil {
		return nil, err
	}

	ec2conn := ec2.New(auth, region)

	// Setup the state bag and initial state for the steps
	state := new(multistep.BasicStateBag)
	state.Put("config", &builder.config)
	state.Put("ec2", ec2conn)
	state.Put("hook", hook)
	state.Put("ui", ui)

	// Build the steps
	steps := []multistep.Step{
		&awscommon.StepSourceAMIInfo{
			SourceAmi:          builder.config.SourceAmi,
			EnhancedNetworking: builder.config.AMIEnhancedNetworking,
		},
		&awscommon.StepKeyPair{
			Debug:          builder.config.PackerDebug,
			DebugKeyPath:   fmt.Sprintf("ec2_%s.pem", builder.config.PackerBuildName),
			KeyPairName:    builder.config.TemporaryKeyPairName,
			PrivateKeyFile: builder.config.SSHPrivateKeyFile,
		},
		&awscommon.StepSecurityGroup{
			SecurityGroupIds: builder.config.SecurityGroupIds,
			SSHPort:          builder.config.SSHPort,
			VpcId:            builder.config.VpcId,
		},
		&awscommon.StepRunSourceInstance{
			Debug:                    builder.config.PackerDebug,
			ExpectedRootDevice:       "ebs",
			SpotPrice:                builder.config.SpotPrice,
			SpotPriceProduct:         builder.config.SpotPriceAutoProduct,
			InstanceType:             builder.config.InstanceType,
			UserData:                 builder.config.UserData,
			UserDataFile:             builder.config.UserDataFile,
			SourceAMI:                builder.config.SourceAmi,
			IamInstanceProfile:       builder.config.IamInstanceProfile,
			SubnetId:                 builder.config.SubnetId,
			AssociatePublicIpAddress: builder.config.AssociatePublicIpAddress,
			AvailabilityZone:         builder.config.AvailabilityZone,
			BlockDevices:             builder.config.BlockDevices,
			Tags:                     builder.config.RunTags,
		},
		&common.StepConnectSSH{
			SSHAddress: awscommon.SSHAddress(
				ec2conn, builder.config.SSHPort, builder.config.SSHPrivateIp),
			SSHConfig:      awscommon.SSHConfig(builder.config.SSHUsername),
			SSHWaitTimeout: builder.config.SSHTimeout(),
		},
		&StepGetVolume{
			DeviceName: builder.config.WorkerDeviceName,
		},
		&common.StepProvision{},
		&StepStopInstance{SpotPrice: builder.config.SpotPrice},
		&chroot.StepSnapshot{},
		&StepRegisterAMI{
			RootDeviceName: builder.config.RootDeviceName,
		},
		&awscommon.StepAMIRegionCopy{
			Regions: builder.config.AMIRegions,
		},
		&awscommon.StepModifyAMIAttributes{
			Description: builder.config.AMIDescription,
			Users:       builder.config.AMIUsers,
			Groups:      builder.config.AMIGroups,
		},
		&awscommon.StepCreateTags{
			Tags: builder.config.AMITags,
		},
	}

	// Run!
	if builder.config.PackerDebug {
		builder.runner = &multistep.DebugRunner{
			Steps:   steps,
			PauseFn: common.MultistepDebugFn(ui),
		}
	} else {
		builder.runner = &multistep.BasicRunner{Steps: steps}
	}

	builder.runner.Run(state)

	// If there was an error, return that
	if rawErr, ok := state.GetOk("error"); ok {
		return nil, rawErr.(error)
	}

	// If there are no AMIs, then just return
	if _, ok := state.GetOk("amis"); !ok {
		return nil, nil
	}

	// Build the artifact and return it
	artifact := &awscommon.Artifact{
		Amis:           state.Get("amis").(map[string]string),
		BuilderIdValue: BuilderId,
		Conn:           ec2conn,
	}

	return artifact, nil
}

func (builder *Builder) Cancel() {
	if builder.runner != nil {
		log.Println("Cancelling the step runner...")
		builder.runner.Cancel()
	}
}

func (builder *Builder) ensureWorkerDeviceMapping() {
	if builder.config.WorkerDeviceName == "" {
		builder.config.WorkerDeviceName = searchAvailableDeviceName(builder.config.BlockDevices.LaunchMappings)
	}

	for _, blockDevice := range builder.config.BlockDevices.LaunchMappings {
		if blockDevice.DeviceName == builder.config.WorkerDeviceName {
			return
		}
	}

	builder.config.BlockDevices.LaunchMappings = append(builder.config.BlockDevices.LaunchMappings, awscommon.BlockDevice{
		DeviceName:          builder.config.WorkerDeviceName,
		DeleteOnTermination: true,
		VolumeType:          builder.config.VolumeType,
		VolumeSize:          builder.config.VolumeSize,
	})
}

func searchAvailableDeviceName(blockDevices []awscommon.BlockDevice) string {
	usedDeviceNames := map[string]bool{
		"/dev/sdb": false,
		"/dev/sdc": false,
		"/dev/sdd": false,
		"/dev/sde": false,
		"/dev/sdf": false,
		"/dev/sdg": false,
	}
	for _, blockDevice := range blockDevices {
		usedDeviceNames[blockDevice.DeviceName] = true
	}
	for name, used := range usedDeviceNames {
		if !used {
			return name
		}
	}
	return ""
}
