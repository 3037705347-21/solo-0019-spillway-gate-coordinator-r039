package domain

import (
	"testing"
	"time"
)

func testNow() time.Time {
	return time.Date(2026, 10, 3, 9, 0, 0, 0, time.UTC)
}

func TestNewRegionLockdownValidatesScopeAndBasis(t *testing.T) {
	now := testNow()

	lockdown, err := NewRegionLockdown("lck-alpha", "上游区域警报", "duty-lead-wang", []string{" gate-a ", "gate-b"}, now)
	if err != nil {
		t.Fatalf("expected valid lockdown, got %v", err)
	}
	if lockdown.Status != LockdownActive {
		t.Fatalf("expected active status, got %q", lockdown.Status)
	}
	if got, want := len(lockdown.GateIDs), 2; got != want {
		t.Fatalf("expected %d scoped gates, got %d", want, got)
	}
	if lockdown.GateIDs[0] != "gate-a" {
		t.Fatalf("expected trimmed gate id, got %q", lockdown.GateIDs[0])
	}
	if !lockdown.Covers("gate-b") || lockdown.Covers("gate-c") {
		t.Fatal("coverage membership returned an inconsistent result")
	}

	cases := []struct {
		name      string
		id        string
		reason    string
		authority string
		gateIDs   []string
		code      string
	}{
		{"bad id", "BAD-ID", "reason", "lead", []string{"gate-a"}, "invalid_lockdown_id"},
		{"missing reason", "lck-alpha", " ", "lead", []string{"gate-a"}, "invalid_lockdown_reason"},
		{"missing authority", "lck-alpha", "reason", " ", []string{"gate-a"}, "invalid_lockdown_authority"},
		{"empty scope", "lck-alpha", "reason", "lead", nil, "invalid_lockdown_scope"},
		{"empty gate entry", "lck-alpha", "reason", "lead", []string{"gate-a", " "}, "invalid_lockdown_scope"},
		{"duplicate gate", "lck-alpha", "reason", "lead", []string{"gate-a", "gate-a"}, "invalid_lockdown_scope"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewRegionLockdown(tc.id, tc.reason, tc.authority, tc.gateIDs, now)
			if ErrorCodeOf(err) != tc.code {
				t.Fatalf("expected error code %q, got %v", tc.code, err)
			}
		})
	}
}

func TestRegionLockdownReleaseIsOneWay(t *testing.T) {
	now := testNow()
	lockdown, err := NewRegionLockdown("lck-alpha", "reason", "lead", []string{"gate-a"}, now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := lockdown.Release(" ", "missing actor", now); ErrorCodeOf(err) != "invalid_released_by" {
		t.Fatalf("expected invalid_released_by, got %v", err)
	}
	if err := lockdown.Release("lead", "all clear", now); err != nil {
		t.Fatalf("release failed: %v", err)
	}
	if lockdown.Status != LockdownReleased {
		t.Fatalf("expected released status, got %q", lockdown.Status)
	}
	if !lockdown.ReleasedAt.Equal(now) {
		t.Fatalf("expected release timestamp, got %v", lockdown.ReleasedAt)
	}

	if err := lockdown.Release("lead", "again", now); ErrorCodeOf(err) != "lockdown_not_active" {
		t.Fatalf("expected lockdown_not_active on repeat release, got %v", err)
	}
	if err := lockdown.Release(" ", "missing actor", now); ErrorCodeOf(err) != "lockdown_not_active" {
		t.Fatalf("repeat release always conflicts as not active, got %v", err)
	}
}

func TestGateLockdownMarkersPreserveOperatingStatus(t *testing.T) {
	now := testNow()
	gate, err := NewGate("gate-a", "一号闸", 5, 0.5, now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := gate.MarkRegionLocked("lck-alpha", now); err != nil {
		t.Fatalf("mark failed: %v", err)
	}
	if gate.Status != GateIdle || !gate.IsRegionLocked() || gate.LockdownID != "lck-alpha" {
		t.Fatalf("marker changed operating state: %+v", gate)
	}
	if err := gate.MarkRegionLocked("lck-beta", now); ErrorCodeOf(err) != "gate_already_locked" {
		t.Fatalf("expected gate_already_locked, got %v", err)
	}
	if err := gate.ClearRegionLockdown(now); err != nil {
		t.Fatalf("clear marker failed: %v", err)
	}
	if gate.Status != GateIdle || gate.IsRegionLocked() {
		t.Fatalf("clearing marker changed operating state: %+v", gate)
	}
	if err := gate.ClearRegionLockdown(now); ErrorCodeOf(err) != "gate_not_locked" {
		t.Fatalf("expected gate_not_locked, got %v", err)
	}
}

func TestReleaseReservationOnlyForMatchingReservedGate(t *testing.T) {
	now := testNow()
	gate, err := NewGate("gate-a", "一号闸", 5, 0.5, now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := gate.Reserve("cmd-1", now); err != nil {
		t.Fatalf("reserve failed: %v", err)
	}
	if err := gate.ReleaseReservation("cmd-2", now); ErrorCodeOf(err) != "gate_command_mismatch" {
		t.Fatalf("expected mismatch error, got %v", err)
	}
	if err := gate.ReleaseReservation("cmd-1", now); err != nil {
		t.Fatalf("release reservation failed: %v", err)
	}
	if gate.Status != GateIdle || gate.ActiveCommandID != "" || gate.Aperture != 0.5 {
		t.Fatalf("gate not returned to idle unchanged: %+v", gate)
	}
}

func TestCommandWithdrawIsTerminalAndInvalidatesToken(t *testing.T) {
	now := testNow()
	command, err := NewGateCommand("cmd-1", "gate-a", 2.0, 0.5, 5, "reason", "dispatcher-lin", now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	command.Review(
		InterlockDecision{Verdict: InterlockClear, Verifier: "safety-wu", ReviewedAt: now},
		"tok-1",
		now,
	)

	if err := command.Withdraw("region_lockdown", "lck-alpha", now); err != nil {
		t.Fatalf("withdraw failed: %v", err)
	}
	if command.Status != CommandWithdrawn || command.ExecutionToken != "" {
		t.Fatalf("withdrawn command state wrong: %+v", command)
	}
	if command.LockdownID != "lck-alpha" || command.TerminalReason == "" {
		t.Fatalf("withdraw audit fields missing: %+v", command)
	}

	// Terminal history cannot be rewritten by another withdrawal.
	if err := command.Withdraw("region_lockdown", "lck-beta", now); ErrorCodeOf(err) != "command_not_withdrawable" {
		t.Fatalf("expected command_not_withdrawable, got %v", err)
	}
	// Pending commands are not withdrawable (they hold no reservation).
	pending, _ := NewGateCommand("cmd-2", "gate-a", 2.0, 0.5, 5, "reason", "dispatcher-lin", now)
	if err := pending.Withdraw("region_lockdown", "lck-alpha", now); ErrorCodeOf(err) != "command_not_withdrawable" {
		t.Fatalf("pending withdraw should be rejected, got %v", err)
	}
}
