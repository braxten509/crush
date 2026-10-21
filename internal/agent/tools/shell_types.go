package tools

type BashParams struct {
	Description         string `json:"description" description:"A brief description of what the command does, try to keep it under 30 characters or so"`
	Command             string `json:"command" description:"The command to execute"`
	WorkingDir          string `json:"working_dir,omitempty" description:"The working directory to execute the command in (defaults to current directory)"`
	RunInBackground     bool   `json:"run_in_background,omitempty" description:"Set to true (boolean) to run this command in the background. Use job_output to read the output later."`
	AutoBackgroundAfter int    `json:"auto_background_after,omitempty" description:"Seconds to wait before automatically moving the command to a background job (default: 60)"`
}

type BashPermissionsParams struct {
	Description         string `json:"description"`
	Command             string `json:"command"`
	WorkingDir          string `json:"working_dir"`
	RunInBackground     bool   `json:"run_in_background"`
	AutoBackgroundAfter int    `json:"auto_background_after"`
}

type BashResponseMetadata struct {
	StartTime        int64  `json:"start_time"`
	EndTime          int64  `json:"end_time"`
	Output           string `json:"output"`
	Description      string `json:"description"`
	WorkingDirectory string `json:"working_directory"`
	Background       bool   `json:"background,omitempty"`
	ShellID          string `json:"shell_id,omitempty"`
}

const (
	BashToolName = "bash"

	DefaultAutoBackgroundAfter = 60  // Commands taking longer automatically become background jobs
	MaxAutoBackgroundAfter     = 120 // No command stays in the foreground longer
	MaxOutputLength            = 30000
	BashNoOutput               = "no output"
)

const (
	JobOutputToolName = "job_output"
)

type JobOutputParams struct {
	ShellID string `json:"shell_id" description:"The ID of the background shell to retrieve output from"`
	Wait    bool   `json:"wait" description:"If true, block until the background shell completes before returning output"`
}

type JobOutputResponseMetadata struct {
	ShellID          string `json:"shell_id"`
	Command          string `json:"command"`
	Description      string `json:"description"`
	Done             bool   `json:"done"`
	WorkingDirectory string `json:"working_directory"`
}

const (
	JobKillToolName = "job_kill"
)

type JobKillParams struct {
	ShellID string `json:"shell_id" description:"The ID of the background shell to terminate"`
}

type JobKillResponseMetadata struct {
	ShellID     string `json:"shell_id"`
	Command     string `json:"command"`
	Description string `json:"description"`
}
