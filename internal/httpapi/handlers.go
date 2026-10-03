package httpapi

import (
	"net/http"

	"example.com/spillway-gate-coordinator/internal/contracts"
)

func (h *handler) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *handler) registerGate(w http.ResponseWriter, r *http.Request) {
	var request contracts.RegisterGateRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeDomainError(w, err)
		return
	}

	gate, err := h.service.RegisterGate(r.Context(), request)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, contracts.RegisterGateResponse{Gate: gateView(gate)})
}

func (h *handler) gate(w http.ResponseWriter, r *http.Request) {
	gate, err := h.service.Gate(r.Context(), r.PathValue("id"))
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, contracts.GateResponse{Gate: gateView(gate)})
}

func (h *handler) requestCommand(w http.ResponseWriter, r *http.Request) {
	var request contracts.RequestCommandRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeDomainError(w, err)
		return
	}

	command, err := h.service.RequestCommand(r.Context(), request)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(
		w,
		http.StatusCreated,
		contracts.CommandResponse{Command: commandView(command, false)},
	)
}

func (h *handler) command(w http.ResponseWriter, r *http.Request) {
	command, err := h.service.Command(r.Context(), r.PathValue("id"))
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(
		w,
		http.StatusOK,
		contracts.CommandResponse{Command: commandView(command, false)},
	)
}

func (h *handler) reviewInterlock(w http.ResponseWriter, r *http.Request) {
	var request contracts.ReviewInterlockRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeDomainError(w, err)
		return
	}

	command, gate, err := h.service.ReviewInterlock(r.Context(), r.PathValue("id"), request)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(
		w,
		http.StatusOK,
		contracts.ReviewCommandResponse{
			Command: commandView(command, true),
			Gate:    gateView(gate),
		},
	)
}

func (h *handler) executeCommand(w http.ResponseWriter, r *http.Request) {
	var request contracts.ExecuteCommandRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeDomainError(w, err)
		return
	}

	command, gate, err := h.service.ExecuteCommand(r.Context(), r.PathValue("id"), request)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(
		w,
		http.StatusOK,
		contracts.ExecuteCommandResponse{
			Command: commandView(command, false),
			Gate:    gateView(gate),
		},
	)
}

func (h *handler) createLockdown(w http.ResponseWriter, r *http.Request) {
	var request contracts.CreateLockdownRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeDomainError(w, err)
		return
	}

	lockdown, err := h.service.CreateLockdown(r.Context(), request)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, contracts.LockdownResponse{Lockdown: lockdownView(lockdown)})
}

func (h *handler) lockdown(w http.ResponseWriter, r *http.Request) {
	lockdown, err := h.service.Lockdown(r.Context(), r.PathValue("id"))
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, contracts.LockdownResponse{Lockdown: lockdownView(lockdown)})
}

func (h *handler) releaseLockdown(w http.ResponseWriter, r *http.Request) {
	var request contracts.ReleaseLockdownRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeDomainError(w, err)
		return
	}

	lockdown, gates, err := h.service.ReleaseLockdown(r.Context(), r.PathValue("id"), request)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	gateViews := make([]contracts.GateView, 0, len(gates))
	for _, gate := range gates {
		gateViews = append(gateViews, gateView(gate))
	}
	writeJSON(
		w,
		http.StatusOK,
		contracts.ReleaseLockdownResponse{
			Lockdown: lockdownView(lockdown),
			Gates:    gateViews,
		},
	)
}
