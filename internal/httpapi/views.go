package httpapi

import (
	"time"

	"example.com/spillway-gate-coordinator/internal/contracts"
	"example.com/spillway-gate-coordinator/internal/domain"
)

func formatTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}

func gateView(gate domain.Gate) contracts.GateView {
	return contracts.GateView{
		ID:              gate.ID,
		Name:            gate.Name,
		Status:          string(gate.Status),
		Aperture:        gate.Aperture,
		MaxAperture:     gate.MaxAperture,
		ActiveCommandID: gate.ActiveCommandID,
		LockdownID:      gate.LockdownID,
		RegionLocked:    gate.IsRegionLocked(),
		UpdatedAt:       formatTime(gate.UpdatedAt),
		Version:         gate.Version,
	}
}

func commandView(command domain.GateCommand, includeToken bool) contracts.CommandView {
	view := contracts.CommandView{
		ID:               command.ID,
		GateID:           command.GateID,
		TargetAperture:   command.TargetAperture,
		StartingAperture: command.StartingAperture,
		Reason:           command.Reason,
		RequestedBy:      command.RequestedBy,
		Status:           string(command.Status),
		ExecutedBy:       command.ExecutedBy,
		TerminalReason:   command.TerminalReason,
		LockdownID:       command.LockdownID,
		CreatedAt:        formatTime(command.CreatedAt),
		UpdatedAt:        formatTime(command.UpdatedAt),
		Version:          command.Version,
	}

	if includeToken && command.Status == domain.CommandApproved {
		view.ExecutionToken = command.ExecutionToken
	}
	if command.Interlock != nil {
		view.Interlock = &contracts.InterlockDecisionView{
			Verdict:    string(command.Interlock.Verdict),
			Verifier:   command.Interlock.Verifier,
			Note:       command.Interlock.Note,
			ReviewedAt: formatTime(command.Interlock.ReviewedAt),
		}
	}
	return view
}

func lockdownView(lockdown domain.RegionLockdown) contracts.LockdownView {
	view := contracts.LockdownView{
		ID:                  lockdown.ID,
		Reason:              lockdown.Reason,
		Authority:           lockdown.Authority,
		GateIDs:             lockdown.GateIDs,
		Status:              string(lockdown.Status),
		WithdrawnCommandIDs: lockdown.WithdrawnCommandIDs,
		CreatedAt:           formatTime(lockdown.CreatedAt),
		ReleasedBy:          lockdown.ReleasedBy,
		ReleaseNote:         lockdown.ReleaseNote,
		Version:             lockdown.Version,
	}
	if view.GateIDs == nil {
		view.GateIDs = []string{}
	}
	if view.WithdrawnCommandIDs == nil {
		view.WithdrawnCommandIDs = []string{}
	}
	if !lockdown.ReleasedAt.IsZero() {
		view.ReleasedAt = formatTime(lockdown.ReleasedAt)
	}
	return view
}
