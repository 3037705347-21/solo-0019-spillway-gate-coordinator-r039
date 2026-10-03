package domain

import (
	"regexp"
	"strings"
	"time"
)

type GateStatus string

const (
	GateIdle     GateStatus = "idle"
	GateReserved GateStatus = "reserved"
	GateMoving   GateStatus = "moving"
	GateFaulted  GateStatus = "faulted"
)

var gateIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{2,31}$`)

type Gate struct {
	ID              string
	Name            string
	Status          GateStatus
	Aperture        float64
	MaxAperture     float64
	ActiveCommandID string
	LockdownID      string
	UpdatedAt       time.Time
	Version         int64
}

func (g Gate) IsRegionLocked() bool {
	return g.LockdownID != ""
}

func NewGate(id, name string, maxAperture, initialAperture float64, now time.Time) (Gate, error) {
	id = strings.TrimSpace(id)
	if !gateIDPattern.MatchString(id) {
		return Gate{}, Invalid("invalid_gate_id", "gate id must use lowercase letters, digits, and hyphens")
	}

	name = strings.TrimSpace(name)
	if name == "" {
		return Gate{}, Invalid("invalid_gate_name", "gate name is required")
	}
	if maxAperture <= 0 || maxAperture > 1000 {
		return Gate{}, Invalid("invalid_max_aperture", "max aperture must be greater than zero")
	}
	if initialAperture < 0 || initialAperture > maxAperture {
		return Gate{}, Invalid("invalid_initial_aperture", "initial aperture must be within the gate limit")
	}

	return Gate{
		ID:          id,
		Name:        name,
		Status:      GateIdle,
		Aperture:    initialAperture,
		MaxAperture: maxAperture,
		UpdatedAt:   now.UTC(),
		Version:     1,
	}, nil
}

func (g *Gate) Reserve(commandID string, now time.Time) error {
	if g.Status != GateIdle {
		return Conflict("gate_not_idle", "gate is not available for a new command")
	}
	if g.ActiveCommandID != "" {
		return Conflict("gate_already_reserved", "gate already references an active command")
	}
	if strings.TrimSpace(commandID) == "" {
		return Invalid("invalid_active_command", "active command id is required")
	}

	g.Status = GateReserved
	g.ActiveCommandID = commandID
	g.UpdatedAt = now.UTC()
	g.Version++
	return nil
}

func (g *Gate) StartMovement(commandID string, now time.Time) error {
	if g.Status != GateReserved {
		return Conflict("gate_not_reserved", "gate must be reserved before movement starts")
	}
	if g.ActiveCommandID != commandID {
		return Conflict("gate_command_mismatch", "gate is reserved by a different command")
	}

	g.Status = GateMoving
	g.UpdatedAt = now.UTC()
	g.Version++
	return nil
}

func (g *Gate) Settle(finalAperture float64, faulted bool, now time.Time) error {
	if g.Status != GateMoving {
		return Conflict("gate_not_moving", "gate must be moving before it can settle")
	}
	if finalAperture < 0 || finalAperture > g.MaxAperture {
		return Invalid("invalid_final_aperture", "final aperture must be within the gate limit")
	}

	g.Aperture = finalAperture
	g.ActiveCommandID = ""
	if faulted {
		g.Status = GateFaulted
	} else {
		g.Status = GateIdle
	}
	g.UpdatedAt = now.UTC()
	g.Version++
	return nil
}

// MarkRegionLocked adds an active lockdown marker to a gate. Gate operating
// status (idle/reserved/moving/faulted) is preserved so the field view can show
// both the physical gate state and the region lockdown that covers it.
func (g *Gate) MarkRegionLocked(lockdownID string, now time.Time) error {
	if strings.TrimSpace(lockdownID) == "" {
		return Invalid("invalid_lockdown_reference", "lockdown id is required")
	}
	if g.LockdownID != "" {
		return Conflict("gate_already_locked", "gate is already covered by an active region lockdown")
	}
	g.LockdownID = lockdownID
	g.UpdatedAt = now.UTC()
	g.Version++
	return nil
}

// ReleaseReservation clears an approved-but-not-started command's reservation
// without settling the aperture: no gate movement has happened, so the current
// aperture and the operating status return to idle together. It can only be
// applied to a gate reserved by the named command.
func (g *Gate) ReleaseReservation(commandID string, now time.Time) error {
	if g.Status != GateReserved {
		return Conflict("gate_not_reserved", "gate must be reserved before its reservation can be released")
	}
	if g.ActiveCommandID != commandID {
		return Conflict("gate_command_mismatch", "gate is reserved by a different command")
	}
	g.Status = GateIdle
	g.ActiveCommandID = ""
	g.UpdatedAt = now.UTC()
	g.Version++
	return nil
}

// ClearRegionLockdown removes a lockdown marker. The operating status itself
// is never changed: a moving gate keeps moving, a faulted gate stays faulted.
func (g *Gate) ClearRegionLockdown(now time.Time) error {
	if g.LockdownID == "" {
		return Conflict("gate_not_locked", "gate is not covered by an active region lockdown")
	}
	g.LockdownID = ""
	g.UpdatedAt = now.UTC()
	g.Version++
	return nil
}
