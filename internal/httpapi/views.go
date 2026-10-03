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

func lockdownView(lockdown domain.Lockdown) contracts.LockdownView {
	view := contracts.LockdownView{
		ID:            lockdown.ID,
		GateIDs:       append([]string(nil), lockdown.GateIDs...),
		Reason:        lockdown.Reason,
		EstablishedBy: lockdown.EstablishedBy,
		Status:        string(lockdown.Status),
		LiftedBy:      lockdown.LiftedBy,
		CreatedAt:     formatTime(lockdown.CreatedAt),
		UpdatedAt:     formatTime(lockdown.UpdatedAt),
		Version:       lockdown.Version,
	}
	if lockdown.LiftedAt != nil {
		view.LiftedAt = formatTime(*lockdown.LiftedAt)
	}
	return view
}

func commandViews(commands []domain.GateCommand) []contracts.CommandView {
	views := make([]contracts.CommandView, 0, len(commands))
	for _, command := range commands {
		views = append(views, commandView(command, false))
	}
	return views
}

func gateViews(gates []domain.Gate) []contracts.GateView {
	views := make([]contracts.GateView, 0, len(gates))
	for _, gate := range gates {
		views = append(views, gateView(gate))
	}
	return views
}
