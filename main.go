package main

import (
	"github.com/mitchellh/packer/packer/plugin"
	"github.com/sorah/packer-builder-amazon-scratch/builder/amazon/scratch"
)

func main() {
	server, err := plugin.Server()
	if err != nil {
		panic(err)
	}
	server.RegisterBuilder(new(scratch.Builder))
	server.Serve()
}
