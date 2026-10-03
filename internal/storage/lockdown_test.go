package storage

import (
	"context"
	"sync"
	"testing"
	"time"

	"example.com/spillway-gate-coordinator/internal/domain"
)

type fixture struct {
	repo    *MemoryRepository
	gateA   domain.Gate
	gateB   domain.Gate
	gateOut domain.Gate
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	now := time.Date(2026, 10, 3, 9, 0, 0, 0, time.UTC)
	repo := NewMemoryRepository()
	ctx := context.Background()

	gateA, err := domain.NewGate("gate-a", "甲闸", 5, 0.5, now)
	if err != nil {
		t.Fatalf("new gate: %v", err)
	}
	gateB, _ := domain.NewGate("gate-b", "乙闸", 5, 0.5, now)
	gateOut, _ := domain.NewGate("gate-out", "外闸", 5, 0.5, now)
	for _, gate := range []domain.Gate{gateA, gateB, gateOut} {
		if err := repo.CreateGate(ctx, gate); err != nil {
			t.Fatalf("create gate: %v", err)
		}
	}
	return fixture{repo: repo, gateA: gateA, gateB: gateB, gateOut: gateOut}
}

func approvedCommand(t *testing.T, repo *MemoryRepository, gateID, commandID string, now time.Time) (domain.GateCommand, string) {
	t.Helper()
	ctx := context.Background()
	gate, err := repo.Gate(ctx, gateID)
	if err != nil {
		t.Fatalf("load gate: %v", err)
	}
	command, err := domain.NewGateCommand(commandID, gateID, 2.0, gate.Aperture, gate.MaxAperture, "调整", "dispatcher-lin", now)
	if err != nil {
		t.Fatalf("new command: %v", err)
	}
	if err := repo.CreateCommand(ctx, command); err != nil {
		t.Fatalf("create command: %v", err)
	}
	decision := domain.InterlockDecision{Verdict: domain.InterlockClear, Verifier: "safety-wu", Note: "ok", ReviewedAt: now}
	reviewed, _, err := repo.ReviewCommand(ctx, commandID, decision, "tok-"+commandID, now)
	if err != nil {
		t.Fatalf("review command: %v", err)
	}
	return reviewed, reviewed.ExecutionToken
}

func pendingCommand(t *testing.T, repo *MemoryRepository, gateID, commandID string, now time.Time) domain.GateCommand {
	t.Helper()
	ctx := context.Background()
	gate, err := repo.Gate(ctx, gateID)
	if err != nil {
		t.Fatalf("load gate: %v", err)
	}
	command, err := domain.NewGateCommand(commandID, gateID, 2.0, gate.Aperture, gate.MaxAperture, "调整", "dispatcher-lin", now)
	if err != nil {
		t.Fatalf("new command: %v", err)
	}
	if err := repo.CreateCommand(ctx, command); err != nil {
		t.Fatalf("create command: %v", err)
	}
	return command
}

func TestCreateLockdownWithdrawsApprovalsAndReleasesReservations(t *testing.T) {
	now := time.Date(2026, 10, 3, 9, 0, 0, 0, time.UTC)
	f := newFixture(t)
	ctx := context.Background()

	approved, token := approvedCommand(t, f.repo, "gate-a", "cmd-approved", now)
	if token == "" {
		t.Fatal("expected execution token")
	}
	pending := pendingCommand(t, f.repo, "gate-b", "cmd-pending", now)

	lockdown, err := domain.NewRegionLockdown("lck-one", "上游区域警报", "duty-lead-wang", []string{"gate-a", "gate-b"}, now)
	if err != nil {
		t.Fatalf("new lockdown: %v", err)
	}
	stored, err := f.repo.CreateLockdown(ctx, lockdown, now)
	if err != nil {
		t.Fatalf("create lockdown: %v", err)
	}
	if got := stored.WithdrawnCommandIDs; len(got) != 1 || got[0] != approved.ID {
		t.Fatalf("withdrawn ids = %v, want [%s]", got, approved.ID)
	}

	gateA, _ := f.repo.Gate(ctx, "gate-a")
	if gateA.Status != domain.GateIdle || gateA.ActiveCommandID != "" || gateA.LockdownID != "lck-one" {
		t.Fatalf("gate-a after lockdown: %+v", gateA)
	}
	if gateA.Aperture != 0.5 {
		t.Fatalf("aperture changed on withdrawal: %v", gateA.Aperture)
	}
	gateB, _ := f.repo.Gate(ctx, "gate-b")
	if gateB.Status != domain.GateIdle || gateB.LockdownID != "lck-one" {
		t.Fatalf("gate-b after lockdown: %+v", gateB)
	}

	withdrawnCommand, _ := f.repo.Command(ctx, approved.ID)
	if withdrawnCommand.Status != domain.CommandWithdrawn {
		t.Fatalf("expected withdrawn, got %q", withdrawnCommand.Status)
	}
	if withdrawnCommand.ExecutionToken != "" || withdrawnCommand.LockdownID != "lck-one" {
		t.Fatalf("withdrawn command fields wrong: %+v", withdrawnCommand)
	}
	pendingAfter, _ := f.repo.Command(ctx, pending.ID)
	if pendingAfter.Status != domain.CommandPending || pendingAfter.Version != 1 {
		t.Fatalf("pending command should stay untouched, got %+v", pendingAfter)
	}
}

func TestLockdownBlocksNewCommandsAndReviews(t *testing.T) {
	now := time.Date(2026, 10, 3, 9, 0, 0, 0, time.UTC)
	f := newFixture(t)
	ctx := context.Background()
	pending := pendingCommand(t, f.repo, "gate-a", "cmd-pending", now)

	lockdown, _ := domain.NewRegionLockdown("lck-one", "reason", "lead", []string{"gate-a"}, now)
	if _, err := f.repo.CreateLockdown(ctx, lockdown, now); err != nil {
		t.Fatalf("create lockdown: %v", err)
	}

	gateA, _ := f.repo.Gate(ctx, "gate-a")
	newCommand, err := domain.NewGateCommand("cmd-new", "gate-a", 3.0, gateA.Aperture, gateA.MaxAperture, "new", "dispatcher-lin", now)
	if err != nil {
		t.Fatalf("new command: %v", err)
	}
	if err := f.repo.CreateCommand(ctx, newCommand); domain.ErrorCodeOf(err) != "gate_region_locked" {
		t.Fatalf("expected gate_region_locked on create, got %v", err)
	}

	decision := domain.InterlockDecision{Verdict: domain.InterlockClear, Verifier: "safety-wu", ReviewedAt: now}
	if _, _, err := f.repo.ReviewCommand(ctx, pending.ID, decision, "tok", now); domain.ErrorCodeOf(err) != "gate_region_locked" {
		t.Fatalf("expected gate_region_locked on clear review, got %v", err)
	}
	unchanged, _ := f.repo.Command(ctx, pending.ID)
	if unchanged.Status != domain.CommandPending {
		t.Fatalf("rejected clear review must not mutate command, got %q", unchanged.Status)
	}
	gateAfter, _ := f.repo.Gate(ctx, "gate-a")
	if gateAfter.Status != domain.GateIdle || gateAfter.ActiveCommandID != "" {
		t.Fatalf("rejected clear review must not occupy gate: %+v", gateAfter)
	}

	// A blocked review occupies nothing and may still refuse the command.
	blocked := domain.InterlockDecision{Verdict: domain.InterlockBlocked, Verifier: "safety-wu", Note: "locked", ReviewedAt: now}
	refused, _, err := f.repo.ReviewCommand(ctx, pending.ID, blocked, "", now)
	if err != nil {
		t.Fatalf("blocked review under lockdown should succeed: %v", err)
	}
	if refused.Status != domain.CommandRejected {
		t.Fatalf("expected rejected after blocked review, got %q", refused.Status)
	}
	blockedGate, _ := f.repo.Gate(ctx, "gate-a")
	if blockedGate.ActiveCommandID != "" || blockedGate.Status != domain.GateIdle {
		t.Fatalf("blocked review must not reserve gate: %+v", blockedGate)
	}

	// Scope outside the lockdown stays operable.
	outside := pendingCommand(t, f.repo, "gate-out", "cmd-out", now)
	clear := domain.InterlockDecision{Verdict: domain.InterlockClear, Verifier: "safety-wu", ReviewedAt: now}
	if _, _, err := f.repo.ReviewCommand(ctx, outside.ID, clear, "tok-out", now); err != nil {
		t.Fatalf("outside-scope review should succeed: %v", err)
	}
}

func TestDuplicateAndOverlappingLockdownRejected(t *testing.T) {
	now := time.Date(2026, 10, 3, 9, 0, 0, 0, time.UTC)
	f := newFixture(t)
	ctx := context.Background()

	first, _ := domain.NewRegionLockdown("lck-one", "reason", "lead", []string{"gate-a"}, now)
	if _, err := f.repo.CreateLockdown(ctx, first, now); err != nil {
		t.Fatalf("create first lockdown: %v", err)
	}

	sameID, _ := domain.NewRegionLockdown("lck-one", "reason", "lead", []string{"gate-b"}, now)
	if _, err := f.repo.CreateLockdown(ctx, sameID, now); domain.ErrorCodeOf(err) != "duplicate_lockdown_id" {
		t.Fatalf("expected duplicate_lockdown_id, got %v", err)
	}

	overlap, _ := domain.NewRegionLockdown("lck-two", "reason", "lead", []string{"gate-a", "gate-b"}, now)
	if _, err := f.repo.CreateLockdown(ctx, overlap, now); domain.ErrorCodeOf(err) != "gate_already_locked" {
		t.Fatalf("expected gate_already_locked on overlap, got %v", err)
	}
	other, _ := domain.NewRegionLockdown("lck-three", "reason", "lead", []string{"gate-b"}, now)
	if _, err := f.repo.CreateLockdown(ctx, other, now); err != nil {
		t.Fatalf("disjoint lockdown should succeed: %v", err)
	}

	unknown, _ := domain.NewRegionLockdown("lck-x", "reason", "lead", []string{"gate-missing"}, now)
	if _, err := f.repo.CreateLockdown(ctx, unknown, now); domain.ErrorKindOf(err) != domain.KindNotFound {
		t.Fatalf("expected not found for unknown scope gate, got %v", err)
	}

	// After a rejected attempt the marked state is unchanged: gate-a stays on
	// the first lockdown and gate-b carries the disjoint one.
	gateA, _ := f.repo.Gate(ctx, "gate-a")
	gateB, _ := f.repo.Gate(ctx, "gate-b")
	if gateA.LockdownID != "lck-one" || gateB.LockdownID != "lck-three" {
		t.Fatalf("markers after rejected attempts wrong: %q %q", gateA.LockdownID, gateB.LockdownID)
	}
}

func TestReleaseRestoresSchedulingAndIsIdempotentConflict(t *testing.T) {
	now := time.Date(2026, 10, 3, 9, 0, 0, 0, time.UTC)
	f := newFixture(t)
	ctx := context.Background()
	pending := pendingCommand(t, f.repo, "gate-a", "cmd-pending", now)

	lockdown, _ := domain.NewRegionLockdown("lck-one", "reason", "lead", []string{"gate-a"}, now)
	if _, err := f.repo.CreateLockdown(ctx, lockdown, now); err != nil {
		t.Fatalf("create lockdown: %v", err)
	}

	released, gates, err := f.repo.ReleaseLockdown(ctx, "lck-one", "lead", "all clear", now)
	if err != nil {
		t.Fatalf("release: %v", err)
	}
	if released.Status != domain.LockdownReleased || len(gates) != 1 {
		t.Fatalf("release result wrong: %+v gates=%d", released, len(gates))
	}
	if _, _, err := f.repo.ReleaseLockdown(ctx, "lck-one", "lead", "again", now); domain.ErrorCodeOf(err) != "lockdown_not_active" {
		t.Fatalf("repeat release should conflict, got %v", err)
	}
	if _, _, err := f.repo.ReleaseLockdown(ctx, "lck-missing", "lead", "", now); domain.ErrorKindOf(err) != domain.KindNotFound {
		t.Fatalf("missing release should be not found, got %v", err)
	}

	gateA, _ := f.repo.Gate(ctx, "gate-a")
	if gateA.LockdownID != "" || gateA.Status != domain.GateIdle {
		t.Fatalf("gate not restored after release: %+v", gateA)
	}
	clear := domain.InterlockDecision{Verdict: domain.InterlockClear, Verifier: "safety-wu", ReviewedAt: now}
	if _, gateAfter, err := f.repo.ReviewCommand(ctx, pending.ID, clear, "tok-1", now); err != nil {
		t.Fatalf("review after release should succeed: %v", err)
	} else if gateAfter.Status != domain.GateReserved {
		t.Fatalf("expected reserved gate, got %q", gateAfter.Status)
	}

	// Re-lockdown the same scope is allowed once released.
	again, _ := domain.NewRegionLockdown("lck-two", "reason", "lead", []string{"gate-a"}, now)
	if _, err := f.repo.CreateLockdown(ctx, again, now); err != nil {
		t.Fatalf("re-lockdown should succeed: %v", err)
	}
}

func TestTerminalHistorySurvivesLockdown(t *testing.T) {
	now := time.Date(2026, 10, 3, 9, 0, 0, 0, time.UTC)
	f := newFixture(t)
	ctx := context.Background()

	// Completed command on gate-a.
	completed, completedToken := approvedCommand(t, f.repo, "gate-a", "cmd-done", now)
	if _, _, err := f.repo.ExecuteCommand(ctx, completed.ID, completedToken, "op", false, now); err != nil {
		t.Fatalf("execute: %v", err)
	}
	// Aborted (fault) command on gate-b.
	aborted, abortedToken := approvedCommand(t, f.repo, "gate-b", "cmd-aborted", now)
	if _, _, err := f.repo.ExecuteCommand(ctx, aborted.ID, abortedToken, "op", true, now); err != nil {
		t.Fatalf("execute fault: %v", err)
	}

	lockdown, _ := domain.NewRegionLockdown("lck-one", "reason", "lead", []string{"gate-a", "gate-b"}, now)
	stored, err := f.repo.CreateLockdown(ctx, lockdown, now)
	if err != nil {
		t.Fatalf("create lockdown: %v", err)
	}
	if len(stored.WithdrawnCommandIDs) != 0 {
		t.Fatalf("terminal commands must not be withdrawn: %v", stored.WithdrawnCommandIDs)
	}

	doneAfter, _ := f.repo.Command(ctx, completed.ID)
	abortedAfter, _ := f.repo.Command(ctx, aborted.ID)
	if doneAfter.Status != domain.CommandCompleted || doneAfter.Version != 4 {
		t.Fatalf("completed history changed: %+v", doneAfter)
	}
	if abortedAfter.Status != domain.CommandAborted || abortedAfter.Version != 4 || abortedAfter.LockdownID != "" {
		t.Fatalf("aborted history changed: %+v", abortedAfter)
	}
	gateB, _ := f.repo.Gate(ctx, "gate-b")
	if gateB.Status != domain.GateFaulted || gateB.LockdownID != "lck-one" {
		t.Fatalf("faulted gate should stay faulted but marked locked: %+v", gateB)
	}
}

func TestSettleUnderLockdownKeepsMarker(t *testing.T) {
	now := time.Date(2026, 10, 3, 9, 0, 0, 0, time.UTC)
	f := newFixture(t)
	ctx := context.Background()

	approved, _ := approvedCommand(t, f.repo, "gate-a", "cmd-moving", now)
	f.repo.mu.Lock()
	moving := f.repo.gates["gate-a"]
	if err := moving.StartMovement(approved.ID, now); err != nil {
		t.Fatalf("start movement: %v", err)
	}
	executing := f.repo.commands[approved.ID]
	if err := executing.BeginExecution(executing.ExecutionToken, "op", now); err != nil {
		t.Fatalf("begin execution: %v", err)
	}
	f.repo.gates["gate-a"] = moving
	f.repo.commands[approved.ID] = executing
	f.repo.mu.Unlock()

	lockdown, _ := domain.NewRegionLockdown("lck-one", "reason", "lead", []string{"gate-a"}, now)
	if _, err := f.repo.CreateLockdown(ctx, lockdown, now); err != nil {
		t.Fatalf("create lockdown: %v", err)
	}

	// Simulate the natural settle step via the domain gate while locked, then
	// persist exactly as the execute workflow would.
	f.repo.mu.Lock()
	gate := f.repo.gates["gate-a"]
	command := f.repo.commands[approved.ID]
	if err := gate.Settle(approved.TargetAperture, false, now); err != nil {
		t.Fatalf("settle: %v", err)
	}
	if err := command.Complete(now); err != nil {
		t.Fatalf("complete: %v", err)
	}
	f.repo.gates["gate-a"] = gate
	f.repo.commands[approved.ID] = command
	f.repo.mu.Unlock()

	gateAfter, _ := f.repo.Gate(ctx, "gate-a")
	if gateAfter.Status != domain.GateIdle || gateAfter.LockdownID != "lck-one" || gateAfter.ActiveCommandID != "" {
		t.Fatalf("settled gate under lockdown wrong: %+v", gateAfter)
	}
	if gateAfter.Aperture != approved.TargetAperture {
		t.Fatalf("settled aperture wrong: %v", gateAfter.Aperture)
	}
	commandAfter, _ := f.repo.Command(ctx, approved.ID)
	if commandAfter.Status != domain.CommandCompleted || commandAfter.LockdownID != "" {
		t.Fatalf("completed command should have no lockdown link: %+v", commandAfter)
	}

	// After release the gate is schedulable again on real (settled) state.
	if _, _, err := f.repo.ReleaseLockdown(ctx, "lck-one", "lead", "clear", now); err != nil {
		t.Fatalf("release: %v", err)
	}
	released, _ := f.repo.Gate(ctx, "gate-a")
	if released.LockdownID != "" || released.Aperture != approved.TargetAperture {
		t.Fatalf("released gate wrong: %+v", released)
	}
	again, err := domain.NewGateCommand("cmd-again", "gate-a", 1.0, released.Aperture, released.MaxAperture, "re-schedule", "dispatcher-lin", now)
	if err != nil {
		t.Fatalf("new command after release: %v", err)
	}
	if err := f.repo.CreateCommand(ctx, again); err != nil {
		t.Fatalf("create command after release: %v", err)
	}
	clear := domain.InterlockDecision{Verdict: domain.InterlockClear, Verifier: "safety-wu", ReviewedAt: now}
	if _, gateAfter, err := f.repo.ReviewCommand(ctx, again.ID, clear, "tok-again", now); err != nil {
		t.Fatalf("re-scheduling after release should work: %v", err)
	} else if gateAfter.Status != domain.GateReserved {
		t.Fatalf("expected reserved after re-schedule, got %q", gateAfter.Status)
	}
}

func TestConcurrentLockdownCreationHasSingleWinner(t *testing.T) {
	now := time.Date(2026, 10, 3, 9, 0, 0, 0, time.UTC)
	f := newFixture(t)
	ctx := context.Background()

	const count = 16
	var wg sync.WaitGroup
	errs := make(chan error, count)
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			lockdown, err := domain.NewRegionLockdown(
				"lck-race-"+string(rune('a'+index)),
				"reason",
				"lead",
				[]string{"gate-a"},
				now,
			)
			if err != nil {
				errs <- err
				return
			}
			_, err = f.repo.CreateLockdown(ctx, lockdown, now)
			errs <- err
		}(i)
	}
	wg.Wait()
	close(errs)

	created, conflicts := 0, 0
	for err := range errs {
		switch {
		case err == nil:
			created++
		case domain.ErrorCodeOf(err) == "gate_already_locked":
			conflicts++
		default:
			t.Fatalf("unexpected concurrent error: %v", err)
		}
	}
	if created != 1 || conflicts != count-1 {
		t.Fatalf("expected 1 created and %d conflicts, got %d / %d", count-1, created, conflicts)
	}
	gateA, _ := f.repo.Gate(ctx, "gate-a")
	if gateA.LockdownID == "" {
		t.Fatal("winning lockdown did not mark the gate")
	}
}

func TestExecuteVsLockdownEndsConsistently(t *testing.T) {
	now := time.Date(2026, 10, 3, 9, 0, 0, 0, time.UTC)
	f := newFixture(t)
	ctx := context.Background()

	approved, token := approvedCommand(t, f.repo, "gate-a", "cmd-race", now)

	var wg sync.WaitGroup
	var executeErr, lockdownErr error
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, _, executeErr = f.repo.ExecuteCommand(ctx, approved.ID, token, "op", false, now)
	}()
	go func() {
		defer wg.Done()
		lockdown, err := domain.NewRegionLockdown("lck-one", "reason", "lead", []string{"gate-a"}, now)
		if err != nil {
			lockdownErr = err
			return
		}
		_, lockdownErr = f.repo.CreateLockdown(ctx, lockdown, now)
	}()
	wg.Wait()

	finalCommand, _ := f.repo.Command(ctx, approved.ID)
	finalGate, _ := f.repo.Gate(ctx, "gate-a")
	switch finalCommand.Status {
	case domain.CommandCompleted:
		if executeErr != nil || lockdownErr != nil {
			t.Fatalf("execute-first path should have both succeed: execute=%v lockdown=%v", executeErr, lockdownErr)
		}
		if finalGate.Status != domain.GateIdle || finalGate.ActiveCommandID != "" || finalGate.LockdownID != "lck-one" {
			t.Fatalf("execute-first gate state wrong: %+v", finalGate)
		}
	case domain.CommandWithdrawn:
		if executeErr == nil || lockdownErr != nil {
			t.Fatalf("lockdown-first path: execute must conflict, lockdown succeed; got execute=%v lockdown=%v", executeErr, lockdownErr)
		}
		if finalGate.Status != domain.GateIdle || finalGate.ActiveCommandID != "" || finalGate.LockdownID != "lck-one" {
			t.Fatalf("lockdown-first gate state wrong: %+v", finalGate)
		}
	default:
		t.Fatalf("command ended in inconsistent status %q", finalCommand.Status)
	}
}
