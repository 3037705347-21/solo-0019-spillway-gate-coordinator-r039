package coordination

import (
	"context"

	"example.com/spillway-gate-coordinator/internal/contracts"
	"example.com/spillway-gate-coordinator/internal/domain"
)

func (s *Service) EstablishLockdown(
	ctx context.Context,
	request contracts.EstablishLockdownRequest,
) (domain.Lockdown, []domain.GateCommand, []domain.Gate, error) {
	lockdownID, err := s.newID("lock")
	if err != nil {
		return domain.Lockdown{}, nil, nil, domain.Internal(
			"lockdown_id_generation_failed",
			"lockdown id could not be generated",
		)
	}

	now := s.now()
	lockdown, err := domain.NewLockdown(
		lockdownID,
		request.GateIDs,
		request.Reason,
		request.EstablishedBy,
		now,
	)
	if err != nil {
		return domain.Lockdown{}, nil, nil, err
	}

	return s.repository.EstablishLockdown(ctx, lockdown, now)
}

func (s *Service) Lockdown(ctx context.Context, id string) (domain.Lockdown, error) {
	return s.repository.Lockdown(ctx, id)
}

func (s *Service) LiftLockdown(
	ctx context.Context,
	lockdownID string,
	request contracts.LiftLockdownRequest,
) (domain.Lockdown, []domain.Gate, error) {
	return s.repository.LiftLockdown(ctx, lockdownID, request.LiftedBy, s.now())
}
