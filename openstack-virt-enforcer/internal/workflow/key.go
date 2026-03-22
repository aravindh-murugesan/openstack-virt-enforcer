package workflow

// IOPOLICYKEYOVERRIDE is the OpenStack Volume metadata key used to manually
// define a Storage QoS policy bypass.
//
// The value must follow a comma-separated format: "total_iops,write_iops,read_iops".
//
// Expected Format:
//
//	"3000,0,0" -> Sets total IOPS to 3000, ignores specific read/write limits.
//
// Example OpenStack Metadata:
//
//	{ "x-virt-enforcer-io-policy-override": "3000,0,0" }
const IOPOLICYKEYOVERRIDE = "x-virt-enforcer-io-policy-override"
