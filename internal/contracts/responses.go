package contracts

type ErrorResponse struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type InterlockDecisionView struct {
	Verdict    string `json:"verdict"`
	Verifier   string `json:"verifier"`
	Note       string `json:"note"`
	ReviewedAt string `json:"reviewed_at"`
}

type GateView struct {
	ID              string  `json:"id"`
	Name            string  `json:"name"`
	Status          string  `json:"status"`
	Aperture        float64 `json:"aperture"`
	MaxAperture     float64 `json:"max_aperture"`
	ActiveCommandID string  `json:"active_command_id,omitempty"`
	LockdownID      string  `json:"lockdown_id,omitempty"`
	RegionLocked    bool    `json:"region_locked"`
	UpdatedAt       string  `json:"updated_at"`
	Version         int64   `json:"version"`
}

type CommandView struct {
	ID               string                 `json:"id"`
	GateID           string                 `json:"gate_id"`
	TargetAperture   float64                `json:"target_aperture"`
	StartingAperture float64                `json:"starting_aperture"`
	Reason           string                 `json:"reason"`
	RequestedBy      string                 `json:"requested_by"`
	Status           string                 `json:"status"`
	Interlock        *InterlockDecisionView `json:"interlock,omitempty"`
	ExecutionToken   string                 `json:"execution_token,omitempty"`
	ExecutedBy       string                 `json:"executed_by,omitempty"`
	TerminalReason   string                 `json:"terminal_reason,omitempty"`
	LockdownID       string                 `json:"lockdown_id,omitempty"`
	CreatedAt        string                 `json:"created_at"`
	UpdatedAt        string                 `json:"updated_at"`
	Version          int64                  `json:"version"`
}

type LockdownView struct {
	ID                  string   `json:"id"`
	Reason              string   `json:"reason"`
	Authority           string   `json:"authority"`
	GateIDs             []string `json:"gate_ids"`
	Status              string   `json:"status"`
	WithdrawnCommandIDs []string `json:"withdrawn_command_ids"`
	CreatedAt           string   `json:"created_at"`
	ReleasedAt          string   `json:"released_at,omitempty"`
	ReleasedBy          string   `json:"released_by,omitempty"`
	ReleaseNote         string   `json:"release_note,omitempty"`
	Version             int64    `json:"version"`
}

type RegisterGateResponse struct {
	Gate GateView `json:"gate"`
}

type GateResponse struct {
	Gate GateView `json:"gate"`
}

type CommandResponse struct {
	Command CommandView `json:"command"`
}

type ReviewCommandResponse struct {
	Command CommandView `json:"command"`
	Gate    GateView    `json:"gate"`
}

type ExecuteCommandResponse struct {
	Command CommandView `json:"command"`
	Gate    GateView    `json:"gate"`
}

type LockdownResponse struct {
	Lockdown LockdownView `json:"lockdown"`
}

type ReleaseLockdownResponse struct {
	Lockdown LockdownView `json:"lockdown"`
	Gates    []GateView   `json:"gates"`
}
