package proto

// Wire types of the desktop app's phase 5-6 engine gaps (desktop design
// 5-6, 6.7; G56). The theme types are in theme.go and the sign-in types
// in auth.go.

// CustomCommand is a custom command or a user-invocable skill, as the
// terminal's command palette lists them under User (G-7).
type CustomCommand struct {
	ID        string                  `json:"id"`
	Name      string                  `json:"name"`
	Content   string                  `json:"content"`
	Arguments []CustomCommandArgument `json:"arguments"`
	// Skill is set when the entry is a skill, null otherwise.
	Skill *CustomCommandSkill `json:"skill"`
}

// CustomCommandArgument is a $NAME placeholder of a custom command.
type CustomCommandArgument struct {
	Name     string `json:"name"`
	Title    string `json:"title"`
	Required bool   `json:"required"`
}

// CustomCommandSkill is the skill a palette entry attaches.
type CustomCommandSkill struct {
	Name          string `json:"name"`
	Description   string `json:"description"`
	SkillFilePath string `json:"skill_file_path"`
}

// ServerPort is a port a chat's background work listens on (G-18).
type ServerPort struct {
	Port int `json:"port"`
	// Program is the program holding the port.
	Program string `json:"program"`
	// Name is the job's name, or else the program its command runs.
	Name      string `json:"name"`
	Command   string `json:"command"`
	JobID     string `json:"job_id,omitempty"`
	PID       int    `json:"pid,omitempty"`
	SessionID string `json:"session_id"`
}

// ProvidersRefresh answers a provider refresh (G-9).
type ProvidersRefresh struct {
	Results []ProviderRefreshResult `json:"results"`
}

// ProviderRefreshResult is one source's outcome: "catwalk" or "hyper".
type ProviderRefreshResult struct {
	Source string `json:"source"`
	OK     bool   `json:"ok"`
	Error  string `json:"error,omitempty"`
}

// HyperCredits is the Hyper credits balance; Credits is null when Hyper is
// not configured or shows no balance (G-25).
type HyperCredits struct {
	Credits *int `json:"credits"`
}

// CPUAffinityRequest turns the agent CPUs on or off (G-16).
type CPUAffinityRequest struct {
	Enabled bool `json:"enabled"`
}

// CPUAffinity is the CPU list the engine now runs on; "" when off (G-16).
type CPUAffinity struct {
	CPUs string `json:"cpus"`
}
