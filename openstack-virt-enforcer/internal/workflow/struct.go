package workflow

import (
	"context"
	"log/slog"

	"github.com/aravindh-murugesan/openstack-virt-enforcer/openstack-virt-enforcer/internal/cloud/openstack"
	ebnats "github.com/aravindh-murugesan/openstack-virt-enforcer/openstack-virt-enforcer/internal/event/eb-nats"
	"github.com/digitalocean/go-libvirt"
	"github.com/gophercloud/gophercloud/v2/openstack/blockstorage/v3/qos"
)

type ResultIOPSEnforcement string

const (
	IoTuneEnforcementSuccess      ResultIOPSEnforcement = "IOPS_ENFORCEMENT_SUCCESS"
	IoTuneEnforcementFailed       ResultIOPSEnforcement = "IOPS_ENFORCEMENT_FAILED"
	IoTuneEnforcementInPlace      ResultIOPSEnforcement = "IOPS_ENFORCEMENT_NOT_REQUIRED"
	IoTuneEnforcementNotRequested ResultIOPSEnforcement = "IOPS_ENFORCEMENT_NOT_REQUESTED_AUDIT_ONLY"
)

type ReactorConfig struct {
	EnableLibvirtLiveIopsEnforcement bool
	EnableLibvirtIopsFromNats        bool
}

type DaemonOpts struct {
	LibvirtControllers LibvirtDaemonOpts
}

type LibvirtDaemonOpts struct {
	IoTuneEnforcement EnforceIoTuneOpts
}

type Logger struct {
	Instance    *slog.Logger
	GlobalRunID string
	RunID       string
	Level       string
}

type Connections struct {
	Libvirt   *libvirt.Libvirt
	Openstack *openstack.Client
	Nats      *ebnats.NATSInstance
	Context   context.Context
	Timeout   int
}

type EnforceIoTuneOpts struct {
	DomainID               string            `json:"domain_id"`
	DiskID                 string            `json:"cinder_disk_id"`
	Enforce                bool              `json:"should_enforce"` // If this is false, it will only audit the discrapencies
	OpenstackQoS           []qos.QoS         `json:"openstack_qos"`
	BaseQoSPolicy          string            `json:"openstack_base_qos"`
	AuditInterval          int               `json:"iotune_audit"`
	VolumeTypeMaxIopsLimit map[string]string `json:"volume_type_max_iops_limit"`
}

type EnforceIoTuneResult struct {
	Request EnforceIoTuneOpts
	Result  ResultIOPSEnforcement
	Message string
}
