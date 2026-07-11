// Package agent implements managed mode: when the panel runs on a hosting VM,
// the agent enrolls with the ComputeBox Craft Cloud control plane and reports
// telemetry / applies desired state over an outbound-only poll loop.
//
// The wire contract here is the single source of truth shared with the
// TypeScript control plane (there are no generated types across the language
// boundary). Bump ProtocolVersion on any breaking change.
package agent

const ProtocolVersion = 1

// EnrollRequest is sent once, using the single-use enrollment token that the
// Infra API's bootstrap injected into the VM.
type EnrollRequest struct {
	ProtocolVersion int    `json:"protocolVersion"`
	InstanceID      string `json:"instanceId"`
	EnrollmentToken string `json:"enrollmentToken"`
	AgentVersion    string `json:"agentVersion"`
	PanelVersion    string `json:"panelVersion"`
	BootID          string `json:"bootId"`
}

// EnrollResponse returns the long-lived instance token used to authenticate
// every sync, plus the control plane's SSO public key (PEM, Ed25519) that the
// panel pins to verify single-sign-on jumps.
type EnrollResponse struct {
	InstanceToken string `json:"instanceToken"`
	SSOPublicKey  string `json:"ssoPublicKey"`
}

// HostStat is VM-wide resource usage.
type HostStat struct {
	CPUPct      float64 `json:"cpuPct"`
	MemUsedMB   int     `json:"memUsedMB"`
	MemTotalMB  int     `json:"memTotalMB"`
	DiskUsedGB  int     `json:"diskUsedGB"`
	DiskTotalGB int     `json:"diskTotalGB"`
	UptimeS     int64   `json:"uptimeS"`
}

// ServerStat is the per-Minecraft-server slice of telemetry.
type ServerStat struct {
	ID      string  `json:"id"`
	Name    string  `json:"name"`
	Type    string  `json:"type"`
	Version string  `json:"version"`
	Status  string  `json:"status"`
	Online  int     `json:"online"`
	Max     int     `json:"max"`
	RSSMB   int     `json:"rssMB"`
	CPUPct  float64 `json:"cpuPct"`
	DiskMB  int64   `json:"diskMB"`
}

// CommandResult reports the outcome of a previously delivered one-off command.
type CommandResult struct {
	CommandID string `json:"commandId"`
	Status    string `json:"status"` // done | error
	Detail    string `json:"detail,omitempty"`
}

// SyncRequest is the observed state the agent reports every tick.
type SyncRequest struct {
	ProtocolVersion int             `json:"protocolVersion"`
	InstanceID      string          `json:"instanceId"`
	Seq             int64           `json:"seq"`
	BootID          string          `json:"bootId"`
	AgentVersion    string          `json:"agentVersion"`
	PanelVersion    string          `json:"panelVersion"`
	Host            HostStat        `json:"host"`
	Servers         []ServerStat    `json:"servers"`
	Results         []CommandResult `json:"results,omitempty"`
}

// Desired is the target state the control plane wants this instance to hold.
type Desired struct {
	PanelVersion string `json:"panelVersion,omitempty"`
	Lock         string `json:"lock,omitempty"` // none | grace | locked | suspended
}

// Command is a one-off imperative action (backup now, export world, …).
type Command struct {
	ID   string         `json:"id"`
	Kind string         `json:"kind"`
	Args map[string]any `json:"args,omitempty"`
}

// SyncResponse is the control plane's answer to a sync.
type SyncResponse struct {
	ServerTime int64     `json:"serverTime"`
	Desired    Desired   `json:"desired"`
	Commands   []Command `json:"commands,omitempty"`
}

// Lock levels.
const (
	LockNone      = "none"
	LockGrace     = "grace"
	LockLocked    = "locked"
	LockSuspended = "suspended"
)
