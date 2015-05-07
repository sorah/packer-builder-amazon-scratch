# packer-builder-amazon-scratch

## What's this?

Packer builder plugin that allows to AMI from empty volume.

``` json
{
  "type": "amazon-scratch",
  "region": "ap-northeast-1",
  "source_ami": "ami-936d9d93",
  "subnet_id": "subnet-XXX",
  "associate_public_ip_address": true,
  "instance_type": "t2.micro",
  "ssh_username": "ubuntu",
  "ami_name": "packer {{timestamp}}",
  "worker_device_name": "/dev/sdf",
  "volume_size": 4,
  "volume_type": "gp2"
}
```

this will starts instance with 4GB gp2 EBS, using `ami-936d9d93`. Then provision your stuff on `/dev/sdf`.

Finally `/dev/sdf` will be used as root block device on new AMI.

## Difference with `amazon-chroot`

- amazon-chroot doesn't allow do something on host machine -- it runs all commands in chrooted environment.
- amazon-chroot requires run `packer` on an existing EC2 instance, but this doesn't.
- this launches `source_ami` to create AMI, each time.
- this creates _empty_ EBS. amazon-chroot always requires source AMI as base.

## Author

sorah

## License

MIT License
