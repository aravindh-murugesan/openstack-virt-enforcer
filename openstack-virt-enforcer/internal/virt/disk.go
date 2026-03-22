package virt

import (
	"reflect"
	"strings"

	"github.com/digitalocean/go-libvirt"
)

// SetIOLimits applies Storage QoS (Quality of Service) parameters to a specific VM disk.
//
// It uses Go reflection to dynamically map the fields of the [IOTune] struct
// to the C-style TypedParams required by the underlying Libvirt RPC API.
//
// Key behaviors:
//   - Reflection-based Mapping: It reads the `xml` struct tags to determine the
//     correct libvirt parameter names.
//   - Dual Persistence: Changes use DomainAffectLive and DomainAffectConfig,
//     taking effect immediately and persisting across reboots.
//
// It returns nil if no non-zero limits are provided in the [IOTune] struct.
func SetIOLimits(l *libvirt.Libvirt, vm VirtualMachine, io IOTune, disk string) error {

	params := []libvirt.TypedParam{}

	val := reflect.ValueOf(io)
	typ := reflect.TypeOf(io)

	for i := 0; i < val.NumField(); i++ {
		field := val.Field(i)
		fieldType := typ.Field(i)

		// 1. Get the tag and strip options like ",omitempty"
		rawTag := fieldType.Tag.Get("xml")
		if rawTag == "" {
			continue
		}

		// "total_bytes_sec,omitempty" -> "total_bytes_sec"
		paramName := strings.Split(rawTag, ",")[0]

		// 2. Switch on the KIND of the field to handle different types correctly
		switch field.Kind() {

		case reflect.Uint64:
			value := field.Uint()
			// Only apply if > 0. Libvirt treats 0 as "unlimited" usually,
			// but we generally want to avoid sending 0s unless explicitly intended.
			if value > 0 {
				params = append(params, libvirt.TypedParam{
					Field: paramName,
					Value: libvirt.TypedParamValue{
						D: uint32(libvirt.TypedParamUllong),
						I: value,
					},
				})
			}
		}
	}

	if len(params) == 0 {
		return nil
	}

	flags := libvirt.DomainAffectLive | libvirt.DomainAffectConfig
	if err := l.DomainSetBlockIOTune(vm.DomainRef, disk, params, uint32(flags)); err != nil {
		return err
	}

	return nil
}
