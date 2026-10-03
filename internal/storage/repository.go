package storage

import (
	"context"
	"time"

	"example.com/spillway-gate-coordinator/internal/domain"
)

type Repository interface {
	CreateGate(ctx context.Context, gate domain.Gate) error
	Gate(ctx context.Context, id string) (domain.Gate, error)
	CreateCommand(ctx context.Context, command domain.GateCommand) error
	Command(ctx context.Context, id string) (domain.GateCommand, error)
	ReviewCommand(
		ctx context.Context,
		commandID string,
		decision domain.InterlockDecision,
		token string,
		now time.Time,
	) (domain.GateCommand, domain.Gate, error)
	ExecuteCommand(
		ctx context.Context,
		commandID string,
		token string,
		operator string,
		simulateFault bool,
		now time.Time,
	) (domain.GateCommand, domain.Gate, error)
	EstablishLockdown(
		ctx context.Context,
		lockdown domain.Lockdown,
		now time.Time,
	) (domain.Lockdown, []domain.GateCommand, []domain.Gate, error)
	Lockdown(ctx context.Context, id string) (domain.Lockdown, error)
	LiftLockdown(
		ctx context.Context,
		lockdownID string,
		liftedBy string,
		now time.Time,
	) (domain.Lockdown, []domain.Gate, error)
}
