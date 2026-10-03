package domain

import (
	"strings"
	"time"
)

type LockdownStatus string

const (
	LockdownActive LockdownStatus = "active"
	LockdownLifted LockdownStatus = "lifted"
)

type Lockdown struct {
	ID            string
	GateIDs       []string
	Reason        string
	EstablishedBy string
	Status        LockdownStatus
	LiftedBy      string
	CreatedAt     time.Time
	LiftedAt      *time.Time
	UpdatedAt     time.Time
	Version       int64
}

func NewLockdown(id string, gateIDs []string, reason, establishedBy string, now time.Time) (Lockdown, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return Lockdown{}, Invalid("invalid_lockdown_id", "lockdown id is required")
	}

	if len(gateIDs) == 0 {
		return Lockdown{}, Invalid("invalid_lockdown_scope", "lockdown scope must list at least one gate")
	}
	scope := make([]string, 0, len(gateIDs))
	seen := make(map[string]struct{}, len(gateIDs))
	for _, gateID := range gateIDs {
		gateID = strings.TrimSpace(gateID)
		if gateID == "" {
			return Lockdown{}, Invalid("invalid_lockdown_scope", "lockdown scope cannot contain an empty gate id")
		}
		if _, duplicate := seen[gateID]; duplicate {
			return Lockdown{}, Invalid("invalid_lockdown_scope", "lockdown scope cannot list the same gate twice")
		}
		seen[gateID] = struct{}{}
		scope = append(scope, gateID)
	}

	reason = strings.TrimSpace(reason)
	if reason == "" {
		return Lockdown{}, Invalid("invalid_lockdown_reason", "lockdown reason is required")
	}

	establishedBy = strings.TrimSpace(establishedBy)
	if establishedBy == "" {
		return Lockdown{}, Invalid("invalid_established_by", "established_by is required")
	}

	return Lockdown{
		ID:            id,
		GateIDs:       scope,
		Reason:        reason,
		EstablishedBy: establishedBy,
		Status:        LockdownActive,
		CreatedAt:     now.UTC(),
		UpdatedAt:     now.UTC(),
		Version:       1,
	}, nil
}

func (l *Lockdown) Lift(liftedBy string, now time.Time) error {
	if l.Status != LockdownActive {
		return Conflict("lockdown_not_active", "only an active lockdown can be lifted")
	}

	liftedBy = strings.TrimSpace(liftedBy)
	if liftedBy == "" {
		return Invalid("invalid_lifted_by", "lifted_by is required")
	}

	liftedAt := now.UTC()
	l.Status = LockdownLifted
	l.LiftedBy = liftedBy
	l.LiftedAt = &liftedAt
	l.UpdatedAt = now.UTC()
	l.Version++
	return nil
}
