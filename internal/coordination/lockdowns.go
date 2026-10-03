package coordination

import (
	"context"

	"example.com/spillway-gate-coordinator/internal/contracts"
	"example.com/spillway-gate-coordinator/internal/domain"
)

func (s *Service) CreateLockdown(
	ctx context.Context,
	request contracts.CreateLockdownRequest,
) (domain.RegionLockdown, error) {
	lockdownID, err := s.newLockdownID()
	if err != nil {
		return domain.RegionLockdown{}, domain.Internal("lockdown_id_generation_failed", "lockdown id could not be generated")
	}

	lockdown, err := domain.NewRegionLockdown(
		lockdownID,
		request.Reason,
		request.Authority,
		request.GateIDs,
		s.now(),
	)
	if err != nil {
		return domain.RegionLockdown{}, err
	}

	return s.repository.CreateLockdown(ctx, lockdown, s.now())
}

func (s *Service) Lockdown(ctx context.Context, id string) (domain.RegionLockdown, error) {
	return s.repository.Lockdown(ctx, id)
}

func (s *Service) ReleaseLockdown(
	ctx context.Context,
	lockdownID string,
	request contracts.ReleaseLockdownRequest,
) (domain.RegionLockdown, []domain.Gate, error) {
	return s.repository.ReleaseLockdown(
		ctx,
		lockdownID,
		request.ReleasedBy,
		request.Note,
		s.now(),
	)
}
