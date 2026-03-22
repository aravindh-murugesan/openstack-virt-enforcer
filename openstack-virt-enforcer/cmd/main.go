package main

import (
	"fmt"
	"os"

	"github.com/aravindh-murugesan/openstack-virt-enforcer/openstack-virt-enforcer/internal/cli"
)

func main() {
	if err := cli.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
