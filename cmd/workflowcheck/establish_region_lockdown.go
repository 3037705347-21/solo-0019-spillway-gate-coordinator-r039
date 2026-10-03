package main

import (
	"fmt"
	"net/http"
)

func runEstablishRegionLockdown(client *apiClient) error {
	// Five gates in the future lockdown scope, each carrying a different
	// command generation: pending, approved-but-not-started, completed,
	// aborted, and pending-destined-for-blocked-review. A sixth gate stays
	// outside the region as a control sample.
	if _, err := fixtureGate(client, "gate-r1", "封锁区一号闸", 5, 0.5); err != nil {
		return err
	}
	if _, err := fixtureGate(client, "gate-r2", "封锁区二号闸", 5, 0.5); err != nil {
		return err
	}
	if _, err := fixtureGate(client, "gate-r3", "封锁区三号闸", 5, 0.5); err != nil {
		return err
	}
	if _, err := fixtureGate(client, "gate-r4", "封锁区四号闸", 5, 0.5); err != nil {
		return err
	}
	if _, err := fixtureGate(client, "gate-r5", "封锁区五号闸", 5, 0.5); err != nil {
		return err
	}
	if _, err := fixtureGate(client, "gate-out", "封锁区外对照闸", 5, 0.5); err != nil {
		return err
	}

	pending, err := fixtureCommand(client, "gate-r1", 2.0, "区域警报前待复核", "dispatcher-lin")
	if err != nil {
		return err
	}
	pendingID, err := stringField(pending, "id")
	if err != nil {
		return err
	}

	approved, err := fixtureCommand(client, "gate-r2", 2.0, "区域警报前已批准", "dispatcher-lin")
	if err != nil {
		return err
	}
	approvedID, err := stringField(approved, "id")
	if err != nil {
		return err
	}
	approvedView, err := fixtureClearReview(client, approvedID, "safety-wu")
	if err != nil {
		return err
	}
	token, err := stringField(approvedView, "execution_token")
	if err != nil {
		return err
	}

	completed, err := fixtureCommand(client, "gate-r3", 2.0, "已经完成的调整", "dispatcher-lin")
	if err != nil {
		return err
	}
	completedID, err := stringField(completed, "id")
	if err != nil {
		return err
	}
	completedApproved, err := fixtureClearReview(client, completedID, "safety-wu")
	if err != nil {
		return err
	}
	completedToken, err := stringField(completedApproved, "execution_token")
	if err != nil {
		return err
	}
	completedResult, err := client.post("/commands/"+completedID+"/execute", map[string]any{
		"operator":       "operator-chen",
		"token":          completedToken,
		"simulate_fault": false,
	})
	if err != nil {
		return err
	}
	if err := requireStatus(completedResult, http.StatusOK, "complete history command"); err != nil {
		return err
	}

	aborted, err := fixtureCommand(client, "gate-r4", 2.0, "已经中止的调整", "dispatcher-lin")
	if err != nil {
		return err
	}
	abortedID, err := stringField(aborted, "id")
	if err != nil {
		return err
	}
	abortedApproved, err := fixtureClearReview(client, abortedID, "safety-wu")
	if err != nil {
		return err
	}
	abortedToken, err := stringField(abortedApproved, "execution_token")
	if err != nil {
		return err
	}
	faultResult, err := client.post("/commands/"+abortedID+"/execute", map[string]any{
		"operator":       "operator-chen",
		"token":          abortedToken,
		"simulate_fault": true,
	})
	if err != nil {
		return err
	}
	if err := requireStatus(faultResult, http.StatusOK, "abort history command"); err != nil {
		return err
	}

	refusedPending, err := fixtureCommand(client, "gate-r5", 1.0, "封锁期间将被拒绝的命令", "dispatcher-lin")
	if err != nil {
		return err
	}
	refusedPendingID, err := stringField(refusedPending, "id")
	if err != nil {
		return err
	}

	// The duty lead establishes the region lockdown with an explicit scope and
	// business basis.
	created, err := client.post("/lockdowns", map[string]any{
		"reason":    "上游区域警报，暂停指定范围闸门调整",
		"authority": "duty-lead-wang",
		"gate_ids":  []string{"gate-r1", "gate-r2", "gate-r3", "gate-r4", "gate-r5"},
	})
	if err != nil {
		return err
	}
	if err := requireStatus(created, http.StatusCreated, "establish region lockdown"); err != nil {
		return err
	}
	lockdownBody, err := objectField(created.Body, "lockdown")
	if err != nil {
		return err
	}
	lockdownID, err := stringField(lockdownBody, "id")
	if err != nil {
		return err
	}
	status, err := stringField(lockdownBody, "status")
	if err != nil {
		return err
	}
	if status != "active" {
		return fmt.Errorf("new lockdown status: expected active, got %q", status)
	}
	withdrawn, err := stringListField(lockdownBody, "withdrawn_command_ids")
	if err != nil {
		return err
	}
	if len(withdrawn) != 1 || withdrawn[0] != approvedID {
		return fmt.Errorf("withdrawn commands: expected only %q, got %v", approvedID, withdrawn)
	}

	// The approved-not-started command is withdrawn together with the gate
	// reservation release; its token is invalidated.
	approvedResult, err := client.get("/commands/" + approvedID)
	if err != nil {
		return err
	}
	if err := requireStatus(approvedResult, http.StatusOK, "read withdrawn command"); err != nil {
		return err
	}
	approvedCurrent, err := objectField(approvedResult.Body, "command")
	if err != nil {
		return err
	}
	if current, err := stringField(approvedCurrent, "status"); err != nil || current != "withdrawn" {
		return fmt.Errorf("approved command after lockdown: expected withdrawn, got %q", current)
	}
	if linked, err := stringField(approvedCurrent, "lockdown_id"); err != nil || linked != lockdownID {
		return fmt.Errorf("withdrawn command lockdown_id: expected %q, got %q", lockdownID, linked)
	}
	if terminal, err := stringField(approvedCurrent, "terminal_reason"); err != nil || terminal == "" {
		return fmt.Errorf("withdrawn command terminal_reason: expected non-empty, got %q", terminal)
	}
	if value, exists := approvedCurrent["execution_token"]; exists && value != "" {
		return fmt.Errorf("withdrawn command still exposes token: %v", value)
	}

	reservedGate, err := readGate(client, "gate-r2")
	if err != nil {
		return err
	}
	if gateStatus, _ := stringField(reservedGate, "status"); gateStatus != "idle" {
		return fmt.Errorf("released gate status: expected idle, got %q", gateStatus)
	}
	if active, _ := stringField(reservedGate, "active_command_id"); active != "" {
		return fmt.Errorf("released gate still references active command %q", active)
	}
	if locked, _ := reservedGate["region_locked"].(bool); !locked {
		return fmt.Errorf("released gate should still show region_locked=true")
	}
	if marker, _ := stringField(reservedGate, "lockdown_id"); marker != lockdownID {
		return fmt.Errorf("released gate lockdown marker: expected %q, got %q", lockdownID, marker)
	}
	if aperture, _ := floatField(reservedGate, "aperture"); aperture != 0.5 {
		return fmt.Errorf("withdrawn gate aperture must be unchanged, got %v", aperture)
	}

	// The withdrawn token cannot start the command anymore.
	staleExecute, err := client.post("/commands/"+approvedID+"/execute", map[string]any{
		"operator":       "operator-chen",
		"token":          token,
		"simulate_fault": false,
	})
	if err != nil {
		return err
	}
	if err := requireStatus(staleExecute, http.StatusConflict, "reject withdrawn token execution"); err != nil {
		return err
	}

	// The pending command survives unchanged. A clear review cannot acquire
	// the gate while the lockdown is active; a blocked review neither occupies
	// nor creates anything, so it may still refuse the pending adjustment.
	pendingResult, err := client.get("/commands/" + pendingID)
	if err != nil {
		return err
	}
	pendingCurrent, err := objectField(pendingResult.Body, "command")
	if err != nil {
		return err
	}
	if current, _ := stringField(pendingCurrent, "status"); current != "pending" {
		return fmt.Errorf("pending command after lockdown: expected pending, got %q", current)
	}
	blockedClear, err := client.post("/commands/"+pendingID+"/interlock", map[string]any{
		"verdict":  "clear",
		"verifier": "safety-wu",
		"note":     "封锁期间试图占用",
	})
	if err != nil {
		return err
	}
	if err := requireStatus(blockedClear, http.StatusConflict, "reject new reservation under lockdown"); err != nil {
		return err
	}
	pendingStill, err := readCommand(client, pendingID)
	if err != nil {
		return err
	}
	if current, _ := stringField(pendingStill, "status"); current != "pending" {
		return fmt.Errorf("rejected clear review must leave command pending, got %q", current)
	}

	// A pending command can still be refused by a blocked review without
	// occupying anything; this is permitted even while the region is locked.
	refused, err := client.post("/commands/"+refusedPendingID+"/interlock", map[string]any{
		"verdict":  "blocked",
		"verifier": "safety-wu",
		"note":     "区域封锁期间拒绝该调整",
	})
	if err != nil {
		return err
	}
	if err := requireStatus(refused, http.StatusOK, "blocked review allowed under lockdown"); err != nil {
		return err
	}
	refusedBody, err := objectField(refused.Body, "command")
	if err != nil {
		return err
	}
	if verdict, _ := stringField(refusedBody, "status"); verdict != "rejected" {
		return fmt.Errorf("blocked review status: expected rejected, got %q", verdict)
	}
	refusedGate, err := readGate(client, "gate-r5")
	if err != nil {
		return err
	}
	if active, _ := stringField(refusedGate, "active_command_id"); active != "" {
		return fmt.Errorf("blocked review must not occupy gate, active=%q", active)
	}

	// No new command can be created inside the scope.
	blockedCommand, err := client.post("/commands", map[string]any{
		"gate_id":         "gate-r1",
		"target_aperture": 3.0,
		"reason":          "封锁期间的新命令",
		"requested_by":    "dispatcher-lin",
	})
	if err != nil {
		return err
	}
	if err := requireStatus(blockedCommand, http.StatusConflict, "reject new command under lockdown"); err != nil {
		return err
	}

	// Completed and aborted history is immutable.
	completedCurrent, err := readCommand(client, completedID)
	if err != nil {
		return err
	}
	if current, _ := stringField(completedCurrent, "status"); current != "completed" {
		return fmt.Errorf("completed history rewritten: got %q", current)
	}
	abortedCurrent, err := readCommand(client, abortedID)
	if err != nil {
		return err
	}
	if current, _ := stringField(abortedCurrent, "status"); current != "aborted" {
		return fmt.Errorf("aborted history rewritten: got %q", current)
	}
	if linked, exists := abortedCurrent["lockdown_id"]; exists && linked != "" {
		return fmt.Errorf("aborted history unexpectedly linked to lockdown: %v", linked)
	}
	faultedGate, err := readGate(client, "gate-r4")
	if err != nil {
		return err
	}
	if gateStatus, _ := stringField(faultedGate, "status"); gateStatus != "faulted" {
		return fmt.Errorf("faulted gate status rewritten by lockdown: got %q", gateStatus)
	}

	// A repeated or overlapping lockdown is a stable conflict.
	duplicate, err := client.post("/lockdowns", map[string]any{
		"reason":    "重复封锁尝试",
		"authority": "duty-lead-wang",
		"gate_ids":  []string{"gate-r2"},
	})
	if err != nil {
		return err
	}
	if err := requireStatus(duplicate, http.StatusConflict, "reject overlapping lockdown"); err != nil {
		return err
	}

	// Gates outside the region keep accepting and completing normal work.
	outside, err := fixtureCommand(client, "gate-out", 3.0, "范围外正常调度", "dispatcher-lin")
	if err != nil {
		return err
	}
	outsideID, _ := stringField(outside, "id")
	outsideApproved, err := fixtureClearReview(client, outsideID, "safety-wu")
	if err != nil {
		return err
	}
	outsideToken, _ := stringField(outsideApproved, "execution_token")
	outsideResult, err := client.post("/commands/"+outsideID+"/execute", map[string]any{
		"operator":       "operator-chen",
		"token":          outsideToken,
		"simulate_fault": false,
	})
	if err != nil {
		return err
	}
	if err := requireStatus(outsideResult, http.StatusOK, "outside-scope command still executes"); err != nil {
		return err
	}

	// Releasing restores schedulability based on the then-current real state.
	released, err := client.post("/lockdowns/"+lockdownID+"/release", map[string]any{
		"released_by": "duty-lead-wang",
		"note":        "区域警报解除",
	})
	if err != nil {
		return err
	}
	if err := requireStatus(released, http.StatusOK, "release region lockdown"); err != nil {
		return err
	}

	secondRelease, err := client.post("/lockdowns/"+lockdownID+"/release", map[string]any{
		"released_by": "duty-lead-wang",
		"note":        "重复解除",
	})
	if err != nil {
		return err
	}
	if err := requireStatus(secondRelease, http.StatusConflict, "reject repeated release"); err != nil {
		return err
	}

	for _, gateID := range []string{"gate-r1", "gate-r2", "gate-r3", "gate-r4", "gate-r5"} {
		gate, err := readGate(client, gateID)
		if err != nil {
			return err
		}
		if locked, _ := gate["region_locked"].(bool); locked {
			return fmt.Errorf("gate %s still marked region_locked after release", gateID)
		}
		if marker, _ := stringField(gate, "lockdown_id"); marker != "" {
			return fmt.Errorf("gate %s still carries lockdown marker %q after release", gateID, marker)
		}
	}

	// The previously pending command can now be reviewed and executed.
	cleared, err := client.post("/commands/"+pendingID+"/interlock", map[string]any{
		"verdict":  "clear",
		"verifier": "safety-wu",
		"note":     "封锁解除后复核通过",
	})
	if err != nil {
		return err
	}
	if err := requireStatus(cleared, http.StatusOK, "review pending command after release"); err != nil {
		return err
	}
	clearedCommand, err := objectField(cleared.Body, "command")
	if err != nil {
		return err
	}
	newToken, _ := stringField(clearedCommand, "execution_token")
	executed, err := client.post("/commands/"+pendingID+"/execute", map[string]any{
		"operator":       "operator-chen",
		"token":          newToken,
		"simulate_fault": false,
	})
	if err != nil {
		return err
	}
	if err := requireStatus(executed, http.StatusOK, "execute command after release"); err != nil {
		return err
	}

	// A new lockdown can now be established over the same range.
	reLocked, err := client.post("/lockdowns", map[string]any{
		"reason":    "二次区域警报",
		"authority": "duty-lead-wang",
		"gate_ids":  []string{"gate-r1", "gate-r2"},
	})
	if err != nil {
		return err
	}
	if err := requireStatus(reLocked, http.StatusCreated, "re-lockdown released scope"); err != nil {
		return err
	}

	fmt.Println("establish-region-lockdown: scope locked, approvals withdrawn, history intact, release re-schedules consistently")
	return nil
}

func readGate(client *apiClient, id string) (map[string]any, error) {
	result, err := client.get("/gates/" + id)
	if err != nil {
		return nil, err
	}
	if err := requireStatus(result, http.StatusOK, "read gate "+id); err != nil {
		return nil, err
	}
	return objectField(result.Body, "gate")
}

func readCommand(client *apiClient, id string) (map[string]any, error) {
	result, err := client.get("/commands/" + id)
	if err != nil {
		return nil, err
	}
	if err := requireStatus(result, http.StatusOK, "read command "+id); err != nil {
		return nil, err
	}
	return objectField(result.Body, "command")
}

func stringListField(object map[string]any, name string) ([]string, error) {
	value, exists := object[name]
	if !exists {
		return nil, fmt.Errorf("object is missing list field %q", name)
	}
	raw, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("object field %q is not a list", name)
	}
	items := make([]string, 0, len(raw))
	for _, entry := range raw {
		text, ok := entry.(string)
		if !ok {
			return nil, fmt.Errorf("object field %q contains a non-string entry", name)
		}
		items = append(items, text)
	}
	return items, nil
}
