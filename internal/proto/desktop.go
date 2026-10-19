package proto

// Wire types of the desktop app's phase 5-6 engine gaps (desktop design
// 5-6, 6.7; G56). The theme types are in theme.go and the sign-in types
// in auth.go.

// CPUAffinityRequest turns the agent CPUs on or off (G-16).
type CPUAffinityRequest struct {
	Enabled bool `json:"enabled"`
}

// CPUAffinity is the CPU list the engine now runs on; "" when off (G-16).
type CPUAffinity struct {
	CPUs string `json:"cpus"`
}
