package storage

import (
	"context"
	"sync"
	"time"

	"example.com/spillway-gate-coordinator/internal/domain"
)

type MemoryRepository struct {
	mu        sync.RWMutex
	gates     map[string]domain.Gate
	commands  map[string]domain.GateCommand
	lockdowns map[string]domain.Lockdown
}

func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{
		gates:     make(map[string]domain.Gate),
		commands:  make(map[string]domain.GateCommand),
		lockdowns: make(map[string]domain.Lockdown),
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
	if gate.Status == domain.GateFaulted {
		return domain.Conflict("gate_faulted", "faulted gates cannot receive new commands")
	}
	if gate.LockdownID != "" {
		return domain.Conflict("gate_lockdown_active", "gate is under an active regional lockdown")
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
	if decision.IsClear() && gate.LockdownID != "" {
		return domain.GateCommand{}, domain.Gate{}, domain.Conflict(
			"gate_lockdown_active",
			"gate is under an active regional lockdown",
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

	m.commands[nextCommand.ID] = nextCommand
	m.gates[nextGate.ID] = nextGate
	return cloneCommand(nextCommand), nextGate, nil
}

func (m *MemoryRepository) EstablishLockdown(
	_ context.Context,
	lockdown domain.Lockdown,
	now time.Time,
) (domain.Lockdown, []domain.GateCommand, []domain.Gate, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.lockdowns[lockdown.ID]; exists {
		return domain.Lockdown{}, nil, nil, domain.Conflict("duplicate_lockdown_id", "lockdown id already exists")
	}

	gates := make([]domain.Gate, 0, len(lockdown.GateIDs))
	for _, gateID := range lockdown.GateIDs {
		gate, exists := m.gates[gateID]
		if !exists {
			return domain.Lockdown{}, nil, nil, domain.NotFound("gate_not_found", "gate id was not found")
		}
		if gate.LockdownID != "" {
			return domain.Lockdown{}, nil, nil, domain.Conflict(
				"gate_already_locked",
				"gate is already covered by an active lockdown",
			)
		}
		gates = append(gates, gate)
	}

	revoked := make([]domain.GateCommand, 0)
	updatedCommands := make(map[string]domain.GateCommand)
	updatedGates := make([]domain.Gate, 0, len(gates))
	for _, gate := range gates {
		nextGate := gate
		if nextGate.Status == domain.GateReserved && nextGate.ActiveCommandID != "" {
			command, exists := m.commands[nextGate.ActiveCommandID]
			if !exists {
				return domain.Lockdown{}, nil, nil, domain.Internal(
					"active_command_missing",
					"gate references an unknown active command",
				)
			}
			nextCommand := cloneCommand(command)
			if nextCommand.Status == domain.CommandApproved {
				if err := nextCommand.Revoke("regional_lockdown", now); err != nil {
					return domain.Lockdown{}, nil, nil, err
				}
				updatedCommands[nextCommand.ID] = nextCommand
				revoked = append(revoked, cloneCommand(nextCommand))
			}
			if err := nextGate.Release(nextCommand.ID, now); err != nil {
				return domain.Lockdown{}, nil, nil, err
			}
		}
		if err := nextGate.Lock(lockdown.ID, now); err != nil {
			return domain.Lockdown{}, nil, nil, err
		}
		updatedGates = append(updatedGates, nextGate)
	}

	for id, command := range updatedCommands {
		m.commands[id] = command
	}
	for _, gate := range updatedGates {
		m.gates[gate.ID] = gate
	}
	stored := cloneLockdown(lockdown)
	m.lockdowns[stored.ID] = stored
	return cloneLockdown(stored), revoked, updatedGates, nil
}

func (m *MemoryRepository) Lockdown(_ context.Context, id string) (domain.Lockdown, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	lockdown, exists := m.lockdowns[id]
	if !exists {
		return domain.Lockdown{}, domain.NotFound("lockdown_not_found", "lockdown id was not found")
	}
	return cloneLockdown(lockdown), nil
}

func (m *MemoryRepository) LiftLockdown(
	_ context.Context,
	lockdownID string,
	liftedBy string,
	now time.Time,
) (domain.Lockdown, []domain.Gate, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	lockdown, exists := m.lockdowns[lockdownID]
	if !exists {
		return domain.Lockdown{}, nil, domain.NotFound("lockdown_not_found", "lockdown id was not found")
	}

	next := cloneLockdown(lockdown)
	if err := next.Lift(liftedBy, now); err != nil {
		return domain.Lockdown{}, nil, err
	}

	gates := make([]domain.Gate, 0, len(next.GateIDs))
	for _, gateID := range next.GateIDs {
		gate, exists := m.gates[gateID]
		if !exists {
			return domain.Lockdown{}, nil, domain.NotFound("gate_not_found", "gate id was not found")
		}
		nextGate := gate
		if nextGate.LockdownID == next.ID {
			if err := nextGate.Unlock(next.ID, now); err != nil {
				return domain.Lockdown{}, nil, err
			}
		}
		gates = append(gates, nextGate)
	}

	m.lockdowns[next.ID] = cloneLockdown(next)
	for _, gate := range gates {
		m.gates[gate.ID] = gate
	}
	return cloneLockdown(next), gates, nil
}

func cloneLockdown(lockdown domain.Lockdown) domain.Lockdown {
	lockdown.GateIDs = append([]string(nil), lockdown.GateIDs...)
	if lockdown.LiftedAt != nil {
		liftedAt := *lockdown.LiftedAt
		lockdown.LiftedAt = &liftedAt
	}
	return lockdown
}

func cloneCommand(command domain.GateCommand) domain.GateCommand {
	if command.Interlock == nil {
		return command
	}
	decision := *command.Interlock
	command.Interlock = &decision
	return command
}
