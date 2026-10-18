// Package contract defines the provider-neutral model and the CloudDriver
// interface that every provider driver (OpenStack, Proxmox, Fake, …) must
// implement. See roadmap §3.1.
package contract

import (
	"strings"
	"time"
)

// ProviderType identifies a driver implementation.
type ProviderType string

const (
	ProviderOpenStack ProviderType = "openstack"
	ProviderProxmox   ProviderType = "proxmox"
	ProviderFake      ProviderType = "fake"
)

// Arch is a CPU architecture in Go/Kubernetes naming.
type Arch string

const (
	ArchAMD64   Arch = "amd64"
	ArchARM64   Arch = "arm64"
	ArchUnknown Arch = "unknown"
)

// NormalizeArch maps provider spellings (x86_64, aarch64, …) to Arch.
func NormalizeArch(s string) Arch {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "amd64", "x86_64", "x86-64":
		return ArchAMD64
	case "arm64", "aarch64":
		return ArchARM64
	default:
		return ArchUnknown
	}
}

// Valid reports whether a is one of the known values, including ArchUnknown.
func (a Arch) Valid() bool {
	return a == ArchAMD64 || a == ArchARM64 || a == ArchUnknown
}

// Capabilities is the static description of what a driver supports. The UI
// uses it to hide unsupported actions; optional behaviour is additionally
// expressed as optional interfaces (see driver.go).
type Capabilities struct {
	Architectures    []Arch `json:"architectures"`
	FloatingIP       bool   `json:"floatingIp"`
	SecurityGroups   bool   `json:"securityGroups"`
	Volumes          bool   `json:"volumes"`
	SnapshotRevert   bool   `json:"snapshotRevert"`
	SnapshotMemory   bool   `json:"snapshotMemory"`
	RevertLatestOnly bool   `json:"revertLatestOnly"`
	Hotplug          bool   `json:"hotplug"`
	ConsoleVNC       bool   `json:"consoleVnc"`
	ConsoleSerial    bool   `json:"consoleSerial"`
}

// SupportsArch reports whether arch is listed in c.Architectures.
func (c Capabilities) SupportsArch(arch Arch) bool {
	for _, a := range c.Architectures {
		if a == arch {
			return true
		}
	}
	return false
}

// ProviderInfo is returned by Validate.
type ProviderInfo struct {
	// Fingerprint is a stable identity of the backing cloud (e.g. Keystone
	// URL + region, PVE cluster name). It must not change when credentials
	// rotate, so re-registration can be detected (roadmap §3.5).
	Fingerprint string `json:"fingerprint"`
	// APIVersions holds the maximum API (micro)version per service.
	APIVersions map[string]string `json:"apiVersions,omitempty"`
}

// NodeStatus is the state of a hypervisor node.
type NodeStatus string

const (
	NodeUp      NodeStatus = "up"
	NodeDown    NodeStatus = "down"
	NodeUnknown NodeStatus = "unknown"
)

// HypervisorNode is a compute host (Nova compute / PVE node).
type HypervisorNode struct {
	ExternalID    string     `json:"externalId"`
	Name          string     `json:"name"`
	Arch          Arch       `json:"arch"`
	Status        NodeStatus `json:"status"`
	VCPUs         int        `json:"vcpus"`
	VCPUsUsed     int        `json:"vcpusUsed"`
	MemoryMiB     int64      `json:"memoryMiB"`
	MemoryMiBUsed int64      `json:"memoryMiBUsed"`
}

// VMStatus is the normalized power/lifecycle state of a VM.
type VMStatus string

const (
	VMRunning  VMStatus = "running"
	VMStopped  VMStatus = "stopped"
	VMPaused   VMStatus = "paused"
	VMBuilding VMStatus = "building"
	VMError    VMStatus = "error"
	VMUnknown  VMStatus = "unknown"
)

// Valid reports whether s is a known VMStatus.
func (s VMStatus) Valid() bool {
	switch s {
	case VMRunning, VMStopped, VMPaused, VMBuilding, VMError, VMUnknown:
		return true
	}
	return false
}

// VirtualMachine is a Nova server or PVE QEMU VM.
type VirtualMachine struct {
	ExternalID string   `json:"externalId"`
	Name       string   `json:"name"`
	Status     VMStatus `json:"status"`
	Arch       Arch     `json:"arch"`
	// HypervisorID references HypervisorNode.ExternalID; empty if unknown
	// (e.g. not visible to the discovery credential).
	HypervisorID string `json:"hypervisorId,omitempty"`
	// ExternalScope is the provider-side ownership scope (OpenStack project
	// ID, PVE pool) used by ProjectMapping. Empty means none.
	ExternalScope string            `json:"externalScope,omitempty"`
	VCPUs         int               `json:"vcpus"`
	MemoryMiB     int64             `json:"memoryMiB"`
	ImageID       string            `json:"imageId,omitempty"`
	IPAddresses   []string          `json:"ipAddresses,omitempty"`
	Tags          map[string]string `json:"tags,omitempty"`
	UpdatedAt     time.Time         `json:"updatedAt"`
}

// MachineImage is a Glance image or PVE template.
type MachineImage struct {
	ExternalID    string `json:"externalId"`
	Name          string `json:"name"`
	Arch          Arch   `json:"arch"`
	ExternalScope string `json:"externalScope,omitempty"`
}

// VirtualNetwork is a Neutron network or PVE bridge/VNet.
type VirtualNetwork struct {
	ExternalID    string   `json:"externalId"`
	Name          string   `json:"name"`
	ExternalScope string   `json:"externalScope,omitempty"`
	CIDRs         []string `json:"cidrs,omitempty"`
}

// Volume is a Cinder volume or PVE disk.
type Volume struct {
	ExternalID    string `json:"externalId"`
	Name          string `json:"name"`
	SizeGiB       int    `json:"sizeGiB"`
	ExternalScope string `json:"externalScope,omitempty"`
	// AttachedTo references VirtualMachine.ExternalID; empty if detached.
	AttachedTo string `json:"attachedTo,omitempty"`
}

// SecurityGroupRule is one rule of a security group.
type SecurityGroupRule struct {
	// Direction is "ingress" or "egress".
	Direction string `json:"direction"`
	// Protocol is "tcp", "udp", "icmp", … or "" for any.
	Protocol string `json:"protocol,omitempty"`
	PortMin  int    `json:"portMin,omitempty"`
	PortMax  int    `json:"portMax,omitempty"`
	// Remote is a CIDR, a remote group ID ("group:<id>"), or "" for any.
	Remote    string `json:"remote,omitempty"`
	EtherType string `json:"etherType,omitempty"`
}

// SecurityGroup is a Neutron security group.
type SecurityGroup struct {
	ExternalID    string              `json:"externalId"`
	Name          string              `json:"name"`
	Description   string              `json:"description,omitempty"`
	ExternalScope string              `json:"externalScope,omitempty"`
	Rules         []SecurityGroupRule `json:"rules"`
}
