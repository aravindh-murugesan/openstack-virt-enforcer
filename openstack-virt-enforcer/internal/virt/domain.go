package virt

import (
	"encoding/xml"
	"fmt"
	"log/slog"

	"github.com/digitalocean/go-libvirt"
)

// GetVirtualMachine retrieves a single virtual machine by name, fetching both its
// RPC handle and its full XML configuration.
//
// The retrieval follows a three-step pipeline:
//  1. Lookup: Obtains the [libvirt.Domain] reference for future RPC calls.
//  2. XML Fetch: Retrieves the raw XML description (using default flags).
//  3. Hydration: Unmarshals the XML into the [VirtualMachine] struct.
//
// It returns a wrapped error if any step in the pipeline fails.
func GetVirtualMachine(l *libvirt.Libvirt, name string) (VirtualMachine, error) {
	// 1. Get the RPC Handle
	d, err := l.DomainLookupByName(name)
	if err != nil {
		return VirtualMachine{}, fmt.Errorf("failed to fetch libvirt domain (%s): %w", name, err)
	}

	// 2. Get Domain XML
	// Flags=0 retrieves the current active configuration (or persistent if inactive)
	xmlDesc, err := l.DomainGetXMLDesc(d, 0)
	if err != nil {
		return VirtualMachine{}, fmt.Errorf("failed to fetch xml for domain (%s): %w", name, err)
	}

	// 3. Hydrate the Struct
	// We manually populate DomainRef because it is not present in the XML
	vm := VirtualMachine{DomainRef: d}

	if err := xml.Unmarshal([]byte(xmlDesc), &vm); err != nil {
		// TODO(aravindh): Consider integrating webhook notification here for parsing failures
		return VirtualMachine{}, fmt.Errorf("failed to parse xml for domain (%s): %w", name, err)
	}

	return vm, nil
}

// GetAllVirtualMachines discovers and hydrates all virtual machines managed by Libvirt.
//
// It queries for both Active (Running) and Inactive (Stopped) domains unless
// restricted by the onlyRunning parameter.
//
// This function is designed to be fault-tolerant: if an individual VM fails to
// be retrieved or parsed, it logs the failure via [slog] and continues
// processing the remaining VMs rather than returning an error.
func GetAllVirtualMachines(l *libvirt.Libvirt, onlyRunning bool) ([]VirtualMachine, error) {

	var flags libvirt.ConnectListAllDomainsFlags
	if onlyRunning {
		flags = libvirt.ConnectListDomainsActive
	} else {
		flags = libvirt.ConnectListDomainsActive | libvirt.ConnectListDomainsInactive
	}

	// The first argument '1' tells libvirt we want the actual list of domains, not just the count.
	domains, _, err := l.ConnectListAllDomains(1, flags)
	if err != nil {
		return nil, fmt.Errorf("failed to discover libvirt domains: %w", err)
	}

	vms := make([]VirtualMachine, 0, len(domains))

	for _, d := range domains {
		vm, err := GetVirtualMachine(l, d.Name)
		if err != nil {
			// Log individual failures but do not crash the entire discovery loop
			slog.Error("Domain describe failure", "name", d.Name, "err", err, "action", "skipped")
			continue
		}

		vms = append(vms, vm)
	}

	return vms, nil
}
