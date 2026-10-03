package storage

import (
	"context"
	"sort"
	"sync"
	"time"

	"example.com/spillway-gate-coordinator/internal/domain"
)

type MemoryRepository struct {
	mu        sync.RWMutex
	gates     map[string]domain.Gate
	commands  map[string]domain.GateCommand
	lockdowns map[string]domain.RegionLockdown
}

func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{
		gates:     make(map[string]domain.Gate),
		commands:  make(map[string]domain.GateCommand),
		lockdowns: make(map[string]domain.RegionLockdown),
	}
}

func (m *MemoryRepository) CreateGate(_ context.Context, gate domain.Gate) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.gates[gate.ID]; exists {
		return domain.Conflict("duplicate_gate_id", "gate id already exists")
	}
	m.gates[gate.ID] = gate
	return nil
}

func (m *MemoryRepository) Gate(_ context.Context, id string) (domain.Gate, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	gate, exists := m.gates[id]
	if !exists {
		return domain.Gate{}, domain.NotFound("gate_not_found", "gate id was not found")
	}
	return gate, nil
}

func (m *MemoryRepository) CreateCommand(_ context.Context, command domain.GateCommand) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	gate, exists := m.gates[command.GateID]
	if !exists {
		return domain.NotFound("gate_not_found", "gate id was not found")
	}
	if gate.LockdownID != "" {
		return domain.Conflict("gate_region_locked", "gate is covered by an active region lockdown")
	}
	if gate.Status == domain.GateFaulted {
		return domain.Conflict("gate_faulted", "faulted gates cannot receive new commands")
	}
	if command.StartingAperture != gate.Aperture {
		return domain.Conflict("gate_aperture_changed", "gate aperture changed before the command was stored")
	}
	if command.TargetAperture > gate.MaxAperture {
		return domain.Invalid("invalid_target_aperture", "target aperture is outside the gate limit")
	}
	if _, exists := m.commands[command.ID]; exists {
		return domain.Conflict("duplicate_command_id", "command id already exists")
	}

	m.commands[command.ID] = command
	return nil
}

func (m *MemoryRepository) Command(_ context.Context, id string) (domain.GateCommand, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	command, exists := m.commands[id]
	if !exists {
		return domain.GateCommand{}, domain.NotFound("command_not_found", "command id was not found")
	}
	return cloneCommand(command), nil
}

func (m *MemoryRepository) ReviewCommand(
	_ context.Context,
	commandID string,
	decision domain.InterlockDecision,
	token string,
	now time.Time,
) (domain.GateCommand, domain.Gate, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	command, exists := m.commands[commandID]
	if !exists {
		return domain.GateCommand{}, domain.Gate{}, domain.NotFound("command_not_found", "command id was not found")
	}
	gate, exists := m.gates[command.GateID]
	if !exists {
		return domain.GateCommand{}, domain.Gate{}, domain.NotFound("gate_not_found", "gate id was not found")
	}

	// A clear review would form a new gate occupation, which a lockdown
	// forbids. A blocked review neither occupies the gate nor creates a
	// command: it only refuses the pending adjustment, so it stays allowed.
	if gate.LockdownID != "" && decision.IsClear() {
		return domain.GateCommand{}, domain.Gate{}, domain.Conflict(
			"gate_region_locked",
			"clear interlock reviews cannot reserve a gate under an active region lockdown",
		)
	}

	nextCommand := cloneCommand(command)
	if err := nextCommand.Review(decision, token, now); err != nil {
		return domain.GateCommand{}, domain.Gate{}, err
	}

	nextGate := gate
	if decision.IsClear() {
		if err := nextGate.Reserve(nextCommand.ID, now); err != nil {
			return domain.GateCommand{}, domain.Gate{}, err
		}
	}

	m.commands[nextCommand.ID] = nextCommand
	m.gates[nextGate.ID] = nextGate
	return cloneCommand(nextCommand), nextGate, nil
}

func (m *MemoryRepository) ExecuteCommand(
	_ context.Context,
	commandID string,
	token string,
	operator string,
	simulateFault bool,
	now time.Time,
) (domain.GateCommand, domain.Gate, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	command, exists := m.commands[commandID]
	if !exists {
		return domain.GateCommand{}, domain.Gate{}, domain.NotFound("command_not_found", "command id was not found")
	}
	gate, exists := m.gates[command.GateID]
	if !exists {
		return domain.GateCommand{}, domain.Gate{}, domain.NotFound("gate_not_found", "gate id was not found")
	}

	nextCommand := cloneCommand(command)
	if err := nextCommand.BeginExecution(token, operator, now); err != nil {
		return domain.GateCommand{}, domain.Gate{}, err
	}

	nextGate := gate
	if err := nextGate.StartMovement(nextCommand.ID, now); err != nil {
		return domain.GateCommand{}, domain.Gate{}, err
	}

	if simulateFault {
		if err := nextGate.Settle(nextCommand.TargetAperture, true, now); err != nil {
			return domain.GateCommand{}, domain.Gate{}, err
		}
		if err := nextCommand.Abort("simulated_gate_fault", now); err != nil {
			return domain.GateCommand{}, domain.Gate{}, err
		}
	} else {
		if err := nextGate.Settle(nextCommand.TargetAperture, false, now); err != nil {
			return domain.GateCommand{}, domain.Gate{}, err
		}
		if err := nextCommand.Complete(now); err != nil {
			return domain.GateCommand{}, domain.Gate{}, err
		}
	}

	// A command that started before its region was locked keeps executing, but
	// once the gate settles it must respect the lockdown and remain blocked:
	// the marker stays until the lockdown is released.
	m.commands[nextCommand.ID] = nextCommand
	m.gates[nextGate.ID] = nextGate
	return cloneCommand(nextCommand), nextGate, nil
}

func (m *MemoryRepository) Lockdown(_ context.Context, id string) (domain.RegionLockdown, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	lockdown, exists := m.lockdowns[id]
	if !exists {
		return domain.RegionLockdown{}, domain.NotFound("lockdown_not_found", "lockdown id was not found")
	}
	return cloneLockdown(lockdown), nil
}

func (m *MemoryRepository) CreateLockdown(
	_ context.Context,
	lockdown domain.RegionLockdown,
	now time.Time,
) (domain.RegionLockdown, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.lockdowns[lockdown.ID]; exists {
		return domain.RegionLockdown{}, domain.Conflict("duplicate_lockdown_id", "lockdown id already exists")
	}

	// Overlap is rejected against the full intended scope before anything is
	// mutated, so a competing lockdown is a stable conflict rather than a
	// partial marking.
	active := m.activeLockdownsLocked()
	for _, gateID := range lockdown.GateIDs {
		if gate, exists := m.gates[gateID]; !exists {
			return domain.RegionLockdown{}, domain.NotFound("gate_not_found", "lockdown scope references an unknown gate: "+gateID)
		} else if gate.LockdownID != "" {
			return domain.RegionLockdown{}, domain.Conflict(
				"gate_already_locked",
				"gate "+gateID+" is already covered by an active region lockdown",
			)
		}
		for _, existing := range active {
			if existing.Covers(gateID) {
				return domain.RegionLockdown{}, domain.Conflict(
					"lockdown_scope_overlap",
					"gate "+gateID+" is already inside active lockdown "+existing.ID,
				)
			}
		}
	}

	nextLockdown := lockdown
	withdrawnIDs := make([]string, 0)

	// Mark every gate in scope first. Status is preserved; only the lockdown
	// marker is added, so idle/reserved/moving/faulted gates stay visible with
	// their true operating state plus the new region marker.
	scopedGates := make(map[string]domain.Gate, len(nextLockdown.GateIDs))
	for _, gateID := range nextLockdown.GateIDs {
		nextGate := m.gates[gateID]
		if err := nextGate.MarkRegionLocked(nextLockdown.ID, now); err != nil {
			return domain.RegionLockdown{}, err
		}
		scopedGates[gateID] = nextGate
	}

	// Approved-but-not-started commands are withdrawn together with their gate
	// reservations. Pending commands stay pending (no review can clear them),
	// executing commands run to completion, and terminal history is untouched.
	commandIDs := make([]string, 0, len(m.commands))
	for commandID := range m.commands {
		commandIDs = append(commandIDs, commandID)
	}
	sort.Strings(commandIDs)
	for _, commandID := range commandIDs {
		command := m.commands[commandID]
		if !nextLockdown.Covers(command.GateID) || command.Status != domain.CommandApproved {
			continue
		}

		nextCommand := cloneCommand(command)
		if err := nextCommand.Withdraw("region_lockdown", nextLockdown.ID, now); err != nil {
			return domain.RegionLockdown{}, err
		}
		nextGate, tracked := scopedGates[command.GateID]
		if !tracked {
			nextGate = m.gates[command.GateID]
		}
		if err := nextGate.ReleaseReservation(command.ID, now); err != nil {
			return domain.RegionLockdown{}, err
		}
		scopedGates[command.GateID] = nextGate
		m.commands[command.ID] = nextCommand
		withdrawnIDs = append(withdrawnIDs, command.ID)
	}

	for gateID, gate := range scopedGates {
		m.gates[gateID] = gate
	}
	nextLockdown.RecordWithdrawals(withdrawnIDs)
	m.lockdowns[nextLockdown.ID] = nextLockdown
	return cloneLockdown(nextLockdown), nil
}

func (m *MemoryRepository) ReleaseLockdown(
	_ context.Context,
	lockdownID string,
	releasedBy string,
	note string,
	now time.Time,
) (domain.RegionLockdown, []domain.Gate, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	lockdown, exists := m.lockdowns[lockdownID]
	if !exists {
		return domain.RegionLockdown{}, nil, domain.NotFound("lockdown_not_found", "lockdown id was not found")
	}
	nextLockdown := cloneLockdown(lockdown)
	if err := nextLockdown.Release(releasedBy, note, now); err != nil {
		return domain.RegionLockdown{}, nil, err
	}

	affectedGates := make([]domain.Gate, 0, len(nextLockdown.GateIDs))
	for _, gateID := range nextLockdown.GateIDs {
		gate, exists := m.gates[gateID]
		if !exists {
			// A gate cannot be removed after registration, so reaching here
			// would break repository consistency.
			return domain.RegionLockdown{}, nil, domain.Internal("gate_not_found", "lockdown scope references a missing gate: "+gateID)
		}
		if gate.LockdownID != lockdownID {
			continue
		}
		if err := gate.ClearRegionLockdown(now); err != nil {
			return domain.RegionLockdown{}, nil, err
		}
		m.gates[gateID] = gate
		affectedGates = append(affectedGates, gate)
	}

	m.lockdowns[lockdownID] = nextLockdown
	return cloneLockdown(nextLockdown), affectedGates, nil
}

func (m *MemoryRepository) activeLockdownsLocked() []domain.RegionLockdown {
	active := make([]domain.RegionLockdown, 0, len(m.lockdowns))
	for _, lockdown := range m.lockdowns {
		if lockdown.Status == domain.LockdownActive {
			active = append(active, lockdown)
		}
	}
	return active
}

func cloneCommand(command domain.GateCommand) domain.GateCommand {
	if command.Interlock == nil {
		return command
	}
	decision := *command.Interlock
	command.Interlock = &decision
	return command
}

func cloneLockdown(lockdown domain.RegionLockdown) domain.RegionLockdown {
	if lockdown.GateIDs != nil {
		lockdown.GateIDs = append([]string(nil), lockdown.GateIDs...)
	}
	if lockdown.WithdrawnCommandIDs != nil {
		lockdown.WithdrawnCommandIDs = append([]string(nil), lockdown.WithdrawnCommandIDs...)
	}
	return lockdown
}
