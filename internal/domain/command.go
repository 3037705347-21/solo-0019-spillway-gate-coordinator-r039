package domain

import (
	"strings"
	"time"
)

type CommandStatus string

const (
	CommandPending   CommandStatus = "pending"
	CommandApproved  CommandStatus = "approved"
	CommandRejected  CommandStatus = "rejected"
	CommandExecuting CommandStatus = "executing"
	CommandCompleted CommandStatus = "completed"
	CommandAborted   CommandStatus = "aborted"
	CommandWithdrawn CommandStatus = "withdrawn"
)

type GateCommand struct {
	ID               string
	GateID           string
	TargetAperture   float64
	StartingAperture float64
	Reason           string
	RequestedBy      string
	Status           CommandStatus
	Interlock        *InterlockDecision
	ExecutionToken   string
	ExecutedBy       string
	TerminalReason   string
	LockdownID       string
	CreatedAt        time.Time
	UpdatedAt        time.Time
	Version          int64
}

func NewGateCommand(
	commandID string,
	gateID string,
	targetAperture float64,
	startingAperture float64,
	maxAperture float64,
	reason string,
	requestedBy string,
	now time.Time,
) (GateCommand, error) {
	commandID = strings.TrimSpace(commandID)
	if commandID == "" {
		return GateCommand{}, Invalid("invalid_command_id", "command id is required")
	}

	gateID = strings.TrimSpace(gateID)
	if gateID == "" {
		return GateCommand{}, Invalid("invalid_gate_reference", "gate id is required")
	}

	reason = strings.TrimSpace(reason)
	if reason == "" {
		return GateCommand{}, Invalid("invalid_command_reason", "reason is required")
	}

	requestedBy = strings.TrimSpace(requestedBy)
	if requestedBy == "" {
		return GateCommand{}, Invalid("invalid_requested_by", "requested_by is required")
	}

	if startingAperture < 0 || startingAperture > maxAperture {
		return GateCommand{}, Invalid("invalid_starting_aperture", "starting aperture is outside the gate limit")
	}
	if targetAperture < 0 || targetAperture > maxAperture {
		return GateCommand{}, Invalid("invalid_target_aperture", "target aperture is outside the gate limit")
	}
	if targetAperture == startingAperture {
		return GateCommand{}, Invalid("no_aperture_change", "target aperture must differ from the current aperture")
	}

	return GateCommand{
		ID:               commandID,
		GateID:           gateID,
		TargetAperture:   targetAperture,
		StartingAperture: startingAperture,
		Reason:           reason,
		RequestedBy:      requestedBy,
		Status:           CommandPending,
		CreatedAt:        now.UTC(),
		UpdatedAt:        now.UTC(),
		Version:          1,
	}, nil
}

func (c *GateCommand) Review(decision InterlockDecision, token string, now time.Time) error {
	if c.Status != CommandPending {
		return Conflict("command_not_pending", "only pending commands can be reviewed")
	}

	decisionCopy := decision
	c.Interlock = &decisionCopy
	c.UpdatedAt = now.UTC()
	c.Version++

	if decision.IsClear() {
		token = strings.TrimSpace(token)
		if token == "" {
			return Invalid("missing_execution_token", "clear decisions require an execution token")
		}
		c.ExecutionToken = token
		c.Status = CommandApproved
		return nil
	}

	c.ExecutionToken = ""
	c.Status = CommandRejected
	return nil
}

func (c *GateCommand) BeginExecution(token, operator string, now time.Time) error {
	if c.Status != CommandApproved {
		return Conflict("command_not_executable", "only approved commands can execute")
	}
	if strings.TrimSpace(operator) == "" {
		return Invalid("invalid_operator", "operator is required")
	}
	if strings.TrimSpace(token) == "" || token != c.ExecutionToken {
		return Conflict("invalid_execution_token", "execution token is invalid")
	}

	c.ExecutedBy = strings.TrimSpace(operator)
	c.Status = CommandExecuting
	c.UpdatedAt = now.UTC()
	c.Version++
	return nil
}

func (c *GateCommand) Complete(now time.Time) error {
	if c.Status != CommandExecuting {
		return Conflict("command_not_executing", "command is not executing")
	}

	c.ExecutionToken = ""
	c.Status = CommandCompleted
	c.UpdatedAt = now.UTC()
	c.Version++
	return nil
}

func (c *GateCommand) Abort(reason string, now time.Time) error {
	if c.Status != CommandExecuting {
		return Conflict("command_not_executing", "command is not executing")
	}

	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "gate_execution_failed"
	}
	c.ExecutionToken = ""
	c.TerminalReason = reason
	c.Status = CommandAborted
	c.UpdatedAt = now.UTC()
	c.Version++
	return nil
}

// Withdraw terminates an approved command that has not started executing when
// its gate is covered by a region lockdown. The execution token is invalidated
// together with the gate reservation. Terminal or in-flight commands cannot be
// withdrawn: their history must remain intact.
func (c *GateCommand) Withdraw(reason, lockdownID string, now time.Time) error {
	if c.Status != CommandApproved {
		return Conflict("command_not_withdrawable", "only approved commands that have not started can be withdrawn")
	}
	lockdownID = strings.TrimSpace(lockdownID)
	if lockdownID == "" {
		return Invalid("invalid_lockdown_reference", "lockdown id is required")
	}

	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "region_lockdown"
	}
	c.ExecutionToken = ""
	c.TerminalReason = reason
	c.LockdownID = lockdownID
	c.Status = CommandWithdrawn
	c.UpdatedAt = now.UTC()
	c.Version++
	return nil
}
