package main

import (
	"fmt"
	"net/http"
	"sync"
)

func runConcurrentRegionLockdown(client *apiClient) error {
	// Two overlapping lockdowns requested concurrently over a shared gate must
	// produce exactly one winner; the loser gets a stable conflict and no gate
	// is ever left marked twice.
	if _, err := fixtureGate(client, "gate-c1", "并发封锁一号闸", 5, 0.5); err != nil {
		return err
	}
	if _, err := fixtureGate(client, "gate-c2", "并发封锁二号闸", 5, 0.5); err != nil {
		return err
	}

	type attempt struct {
		status int
		id     string
	}
	attempts := make([]attempt, 2)
	var wg sync.WaitGroup
	for i, scope := range [][]string{
		{"gate-c1", "gate-c2"},
		{"gate-c2"},
	} {
		wg.Add(1)
		go func(index int, gateIDs []string) {
			defer wg.Done()
			result, callErr := client.post("/lockdowns", map[string]any{
				"reason":    "并发区域警报封锁",
				"authority": "duty-lead-wang",
				"gate_ids":  gateIDs,
			})
			if callErr != nil {
				attempts[index] = attempt{status: 0}
				return
			}
			id := ""
			if lockdown, ok := result.Body["lockdown"].(map[string]any); ok {
				id, _ = stringField(lockdown, "id")
			}
			attempts[index] = attempt{status: result.Status, id: id}
		}(i, scope)
	}
	wg.Wait()

	created := 0
	conflicted := 0
	for _, result := range attempts {
		switch result.status {
		case http.StatusCreated:
			created++
		case http.StatusConflict:
			conflicted++
		default:
			return fmt.Errorf("concurrent lockdown returned unexpected status %d", result.status)
		}
	}
	if created != 1 || conflicted != 1 {
		return fmt.Errorf("concurrent lockdown outcomes: expected 1 created / 1 conflict, got %d / %d", created, conflicted)
	}

	// Concurrent execute vs lockdown of the same approved command: either the
	// command completes and settles before the lock lands (history immutable,
	// gate ends idle but region-locked), or the lockdown withdraws it and the
	// token execution is rejected. The two never partially interleave.
	if _, err := fixtureGate(client, "gate-c3", "并发封锁三号闸", 5, 0.5); err != nil {
		return err
	}
	command, err := fixtureCommand(client, "gate-c3", 2.0, "执行与封锁并发", "dispatcher-lin")
	if err != nil {
		return err
	}
	commandID, _ := stringField(command, "id")
	approvedView, err := fixtureClearReview(client, commandID, "safety-wu")
	if err != nil {
		return err
	}
	token, _ := stringField(approvedView, "execution_token")

	var executeResult, lockdownResult apiResult
	var executeErr, lockdownErr error
	var race sync.WaitGroup
	race.Add(2)
	go func() {
		defer race.Done()
		executeResult, executeErr = client.post("/commands/"+commandID+"/execute", map[string]any{
			"operator":       "operator-chen",
			"token":          token,
			"simulate_fault": false,
		})
	}()
	go func() {
		defer race.Done()
		lockdownResult, lockdownErr = client.post("/lockdowns", map[string]any{
			"reason":    "与执行并发的封锁",
			"authority": "duty-lead-wang",
			"gate_ids":  []string{"gate-c3"},
		})
	}()
	race.Wait()
	if executeErr != nil || lockdownErr != nil {
		return fmt.Errorf("concurrent execute/lockdown calls failed: execute=%v lockdown=%v", executeErr, lockdownErr)
	}

	finalCommand, err := readCommand(client, commandID)
	if err != nil {
		return err
	}
	finalGate, err := readGate(client, "gate-c3")
	if err != nil {
		return err
	}
	finalStatus, _ := stringField(finalCommand, "status")
	gateStatus, _ := stringField(finalGate, "status")
	locked, _ := finalGate["region_locked"].(bool)
	activeCommand, _ := stringField(finalGate, "active_command_id")

	switch finalStatus {
	case "completed":
		if executeResult.Status != http.StatusOK {
			return fmt.Errorf("command completed but execute call returned %d", executeResult.Status)
		}
		if lockdownResult.Status != http.StatusCreated {
			return fmt.Errorf("lockdown after completion: expected 201, got %d", lockdownResult.Status)
		}
		if gateStatus != "idle" || !locked || activeCommand != "" {
			return fmt.Errorf("execute-first outcome mismatch: gate status=%q locked=%v active=%q", gateStatus, locked, activeCommand)
		}
	case "withdrawn":
		if executeResult.Status != http.StatusConflict {
			return fmt.Errorf("command withdrawn but execute call returned %d", executeResult.Status)
		}
		if lockdownResult.Status != http.StatusCreated {
			return fmt.Errorf("lockdown-first: expected 201, got %d", lockdownResult.Status)
		}
		if gateStatus != "idle" || !locked || activeCommand != "" {
			return fmt.Errorf("lockdown-first outcome mismatch: gate status=%q locked=%v active=%q", gateStatus, locked, activeCommand)
		}
	default:
		return fmt.Errorf("concurrent execute/lockdown left command in unexpected status %q", finalStatus)
	}

	fmt.Println("concurrent-region-lockdown: single lockdown winner and atomic execute/withdraw ordering")
	return nil
}
