package domain

import (
	"regexp"
	"strings"
	"time"
)

type LockdownStatus string

const (
	LockdownActive   LockdownStatus = "active"
	LockdownReleased LockdownStatus = "released"
)

var lockdownIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{2,31}$`)

type RegionLockdown struct {
	ID                  string
	Reason              string
	Authority           string
	GateIDs             []string
	Status              LockdownStatus
	WithdrawnCommandIDs []string
	CreatedAt           time.Time
	ReleasedAt          time.Time
	ReleasedBy          string
	ReleaseNote         string
	Version             int64
}

func NewRegionLockdown(
	id string,
	reason string,
	authority string,
	gateIDs []string,
	now time.Time,
) (RegionLockdown, error) {
	id = strings.TrimSpace(id)
	if !lockdownIDPattern.MatchString(id) {
		return RegionLockdown{}, Invalid("invalid_lockdown_id", "lockdown id must use lowercase letters, digits, and hyphens")
	}

	reason = strings.TrimSpace(reason)
	if reason == "" {
		return RegionLockdown{}, Invalid("invalid_lockdown_reason", "lockdown reason is required")
	}

	authority = strings.TrimSpace(authority)
	if authority == "" {
		return RegionLockdown{}, Invalid("invalid_lockdown_authority", "lockdown authority is required")
	}

	if len(gateIDs) == 0 {
		return RegionLockdown{}, Invalid("invalid_lockdown_scope", "lockdown must cover at least one gate")
	}

	normalized := make([]string, 0, len(gateIDs))
	seen := make(map[string]struct{}, len(gateIDs))
	for _, gateID := range gateIDs {
		gateID = strings.TrimSpace(gateID)
		if gateID == "" {
			return RegionLockdown{}, Invalid("invalid_lockdown_scope", "lockdown scope contains an empty gate id")
		}
		if _, duplicated := seen[gateID]; duplicated {
			return RegionLockdown{}, Invalid("invalid_lockdown_scope", "lockdown scope must not list a gate twice")
		}
		seen[gateID] = struct{}{}
		normalized = append(normalized, gateID)
	}

	return RegionLockdown{
		ID:        id,
		Reason:    reason,
		Authority: authority,
		GateIDs:   normalized,
		Status:    LockdownActive,
		CreatedAt: now.UTC(),
		Version:   1,
	}, nil
}

// RecordWithdrawals records the approved commands that were withdrawn together
// with gate reservation release when the lockdown became effective. It is part
// of the atomic lockdown creation, so timestamps and version stay unchanged.
func (l *RegionLockdown) RecordWithdrawals(commandIDs []string) {
	l.WithdrawnCommandIDs = append([]string(nil), commandIDs...)
}

func (l *RegionLockdown) Release(releasedBy, note string, now time.Time) error {
	if l.Status != LockdownActive {
		return Conflict("lockdown_not_active", "lockdown has already been released")
	}

	releasedBy = strings.TrimSpace(releasedBy)
	if releasedBy == "" {
		return Invalid("invalid_released_by", "released_by is required")
	}

	l.Status = LockdownReleased
	l.ReleasedBy = releasedBy
	l.ReleaseNote = strings.TrimSpace(note)
	l.ReleasedAt = now.UTC()
	l.Version++
	return nil
}

func (l RegionLockdown) Covers(gateID string) bool {
	for _, covered := range l.GateIDs {
		if covered == gateID {
			return true
		}
	}
	return false
}
