package main

import (
	"fmt"
	"net/http"
	"sync"
)

func runRegionalLockdown(client *apiClient) error {
	if _, err := fixtureGate(client, "gate-alpha", "上游甲泄洪闸", 5, 0.5); err != nil {
		return err
	}
	if _, err := fixtureGate(client, "gate-beta", "上游乙泄洪闸", 5, 1.0); err != nil {
		return err
	}

	// gate-beta 先完成一条命令，留下不可改写的完成历史。
	completed, err := fixtureCommand(client, "gate-beta", 2.0, "例行泄洪", "dispatcher-lin")
	if err != nil {
		return err
	}
	completedID, err := stringField(completed, "id")
	if err != nil {
		return err
	}
	completedReview, err := fixtureClearReview(client, completedID, "safety-wu")
	if err != nil {
		return err
	}
	completedToken, err := stringField(completedReview, "execution_token")
	if err != nil {
		return err
	}
	completedExec, err := client.post("/commands/"+completedID+"/execute", map[string]any{
		"operator":       "operator-chen",
		"token":          completedToken,
		"simulate_fault": false,
	})
	if err != nil {
		return err
	}
	if err := requireStatus(completedExec, http.StatusOK, "execute completed fixture command"); err != nil {
		return err
	}

	// gate-alpha 上留下一条已批准但尚未开始执行的授权。
	approved, err := fixtureCommand(client, "gate-alpha", 2.5, "上游水位预调", "dispatcher-lin")
	if err != nil {
		return err
	}
	approvedID, err := stringField(approved, "id")
	if err != nil {
		return err
	}
	approvedReview, err := fixtureClearReview(client, approvedID, "safety-wu")
	if err != nil {
		return err
	}
	approvedToken, err := stringField(approvedReview, "execution_token")
	if err != nil {
		return err
	}

	// gate-beta 上留两条待复核命令：一条在封锁期间被 blocked，一条解除后恢复调度。
	pendingHold, err := fixtureCommand(client, "gate-beta", 3.0, "警报期间暂缓", "dispatcher-lin")
	if err != nil {
		return err
	}
	pendingHoldID, err := stringField(pendingHold, "id")
	if err != nil {
		return err
	}
	pendingResume, err := fixtureCommand(client, "gate-beta", 2.5, "解除后恢复调度", "dispatcher-lin")
	if err != nil {
		return err
	}
	pendingResumeID, err := stringField(pendingResume, "id")
	if err != nil {
		return err
	}

	// 封锁范围与业务依据必须先通过校验。
	emptyScope, err := client.post("/lockdowns", map[string]any{
		"gate_ids":       []string{},
		"reason":         "上游区域警报",
		"established_by": "duty-chief-zhao",
	})
	if err != nil {
		return err
	}
	if err := requireStatus(emptyScope, http.StatusBadRequest, "reject empty lockdown scope"); err != nil {
		return err
	}
	if err := requireErrorCode(emptyScope, "invalid_lockdown_scope", "reject empty lockdown scope"); err != nil {
		return err
	}

	duplicateScope, err := client.post("/lockdowns", map[string]any{
		"gate_ids":       []string{"gate-alpha", "gate-alpha"},
		"reason":         "上游区域警报",
		"established_by": "duty-chief-zhao",
	})
	if err != nil {
		return err
	}
	if err := requireStatus(duplicateScope, http.StatusBadRequest, "reject duplicate gate in scope"); err != nil {
		return err
	}

	unknownScope, err := client.post("/lockdowns", map[string]any{
		"gate_ids":       []string{"gate-missing"},
		"reason":         "上游区域警报",
		"established_by": "duty-chief-zhao",
	})
	if err != nil {
		return err
	}
	if err := requireStatus(unknownScope, http.StatusNotFound, "reject unknown gate in scope"); err != nil {
		return err
	}
	if err := requireErrorCode(unknownScope, "gate_not_found", "reject unknown gate in scope"); err != nil {
		return err
	}

	missingReason, err := client.post("/lockdowns", map[string]any{
		"gate_ids":       []string{"gate-alpha"},
		"reason":         "",
		"established_by": "duty-chief-zhao",
	})
	if err != nil {
		return err
	}
	if err := requireStatus(missingReason, http.StatusBadRequest, "reject missing lockdown reason"); err != nil {
		return err
	}
	if err := requireErrorCode(missingReason, "invalid_lockdown_reason", "reject missing lockdown reason"); err != nil {
		return err
	}

	// 建立区域封锁：撤回未开始执行的授权并释放闸门，必须在同一事实里完成。
	established, err := client.post("/lockdowns", map[string]any{
		"gate_ids":       []string{"gate-alpha", "gate-beta"},
		"reason":         "上游区域警报",
		"established_by": "duty-chief-zhao",
	})
	if err != nil {
		return err
	}
	if err := requireStatus(established, http.StatusCreated, "establish regional lockdown"); err != nil {
		return err
	}
	lockdown, err := objectField(established.Body, "lockdown")
	if err != nil {
		return err
	}
	lockdownID, err := stringField(lockdown, "id")
	if err != nil {
		return err
	}
	lockdownStatus, err := stringField(lockdown, "status")
	if err != nil {
		return err
	}
	if lockdownStatus != "active" {
		return fmt.Errorf("lockdown status: expected active, got %q", lockdownStatus)
	}
	revoked, err := arrayField(established.Body, "revoked_commands")
	if err != nil {
		return err
	}
	if len(revoked) != 1 {
		return fmt.Errorf("revoked commands: expected 1, got %d", len(revoked))
	}
	revokedCommand, ok := revoked[0].(map[string]any)
	if !ok {
		return fmt.Errorf("revoked command entry is not an object")
	}
	revokedID, err := stringField(revokedCommand, "id")
	if err != nil {
		return err
	}
	revokedStatus, err := stringField(revokedCommand, "status")
	if err != nil {
		return err
	}
	if revokedID != approvedID || revokedStatus != "revoked" {
		return fmt.Errorf("revoked command mismatch: id=%q status=%q", revokedID, revokedStatus)
	}
	lockdownGates, err := arrayField(established.Body, "gates")
	if err != nil {
		return err
	}
	if len(lockdownGates) != 2 {
		return fmt.Errorf("lockdown gates: expected 2, got %d", len(lockdownGates))
	}

	// 闸门现场可见状态与封锁范围一致：gate-alpha 已释放并带有封锁标记。
	gateAlphaResult, err := client.get("/gates/gate-alpha")
	if err != nil {
		return err
	}
	if err := requireStatus(gateAlphaResult, http.StatusOK, "read locked gate"); err != nil {
		return err
	}
	gateAlpha, err := objectField(gateAlphaResult.Body, "gate")
	if err != nil {
		return err
	}
	gateAlphaStatus, err := stringField(gateAlpha, "status")
	if err != nil {
		return err
	}
	if gateAlphaStatus != "idle" {
		return fmt.Errorf("locked gate status: expected idle, got %q", gateAlphaStatus)
	}
	gateAlphaLockdown, err := stringField(gateAlpha, "lockdown_id")
	if err != nil {
		return err
	}
	if gateAlphaLockdown != lockdownID {
		return fmt.Errorf("locked gate marker: expected %q, got %q", lockdownID, gateAlphaLockdown)
	}
	if value, exists := gateAlpha["active_command_id"]; exists && value != "" {
		return fmt.Errorf("locked gate still references active command %v", value)
	}

	// 被撤回的授权进入终态，执行令牌同时失效。
	revokedRead, err := client.get("/commands/" + approvedID)
	if err != nil {
		return err
	}
	if err := requireStatus(revokedRead, http.StatusOK, "read revoked command"); err != nil {
		return err
	}
	revokedView, err := objectField(revokedRead.Body, "command")
	if err != nil {
		return err
	}
	terminalReason, err := stringField(revokedView, "terminal_reason")
	if err != nil {
		return err
	}
	if terminalReason != "regional_lockdown" {
		return fmt.Errorf("revoked terminal reason: expected regional_lockdown, got %q", terminalReason)
	}
	execRevoked, err := client.post("/commands/"+approvedID+"/execute", map[string]any{
		"operator":       "operator-chen",
		"token":          approvedToken,
		"simulate_fault": false,
	})
	if err != nil {
		return err
	}
	if err := requireStatus(execRevoked, http.StatusConflict, "reject execution of revoked command"); err != nil {
		return err
	}
	if err := requireErrorCode(execRevoked, "command_not_executable", "reject execution of revoked command"); err != nil {
		return err
	}

	// 已完成的历史不被改写。
	completedRead, err := client.get("/commands/" + completedID)
	if err != nil {
		return err
	}
	if err := requireStatus(completedRead, http.StatusOK, "read completed command"); err != nil {
		return err
	}
	completedView, err := objectField(completedRead.Body, "command")
	if err != nil {
		return err
	}
	completedStatus, err := stringField(completedView, "status")
	if err != nil {
		return err
	}
	if completedStatus != "completed" {
		return fmt.Errorf("completed history rewritten: expected completed, got %q", completedStatus)
	}

	// 封锁期间：范围内不能形成新命令。
	blockedCommand, err := client.post("/commands", map[string]any{
		"gate_id":         "gate-alpha",
		"target_aperture": 1.5,
		"reason":          "封锁期间试探",
		"requested_by":    "dispatcher-lin",
	})
	if err != nil {
		return err
	}
	if err := requireStatus(blockedCommand, http.StatusConflict, "reject new command during lockdown"); err != nil {
		return err
	}
	if err := requireErrorCode(blockedCommand, "gate_lockdown_active", "reject new command during lockdown"); err != nil {
		return err
	}

	// 封锁期间：范围内不能形成新的闸门占用。
	blockedClear, err := client.post("/commands/"+pendingResumeID+"/interlock", map[string]any{
		"verdict":  "clear",
		"verifier": "safety-wu",
		"note":     "封锁期间试探放行",
	})
	if err != nil {
		return err
	}
	if err := requireStatus(blockedClear, http.StatusConflict, "reject clear review during lockdown"); err != nil {
		return err
	}
	if err := requireErrorCode(blockedClear, "gate_lockdown_active", "reject clear review during lockdown"); err != nil {
		return err
	}
	stillPending, err := client.get("/commands/" + pendingResumeID)
	if err != nil {
		return err
	}
	stillPendingView, err := objectField(stillPending.Body, "command")
	if err != nil {
		return err
	}
	stillPendingStatus, err := stringField(stillPendingView, "status")
	if err != nil {
		return err
	}
	if stillPendingStatus != "pending" {
		return fmt.Errorf("pending command changed during lockdown: got %q", stillPendingStatus)
	}

	// blocked 复核不形成占用，仍然允许。
	blockedReview, err := client.post("/commands/"+pendingHoldID+"/interlock", map[string]any{
		"verdict":  "blocked",
		"verifier": "safety-wu",
		"note":     "区域警报期间不放行",
	})
	if err != nil {
		return err
	}
	if err := requireStatus(blockedReview, http.StatusOK, "allow blocked review during lockdown"); err != nil {
		return err
	}
	blockedView, err := objectField(blockedReview.Body, "command")
	if err != nil {
		return err
	}
	blockedStatus, err := stringField(blockedView, "status")
	if err != nil {
		return err
	}
	if blockedStatus != "rejected" {
		return fmt.Errorf("blocked review status: expected rejected, got %q", blockedStatus)
	}

	// 重复封锁与已有范围重叠时必须得到一致冲突。
	overlap, err := client.post("/lockdowns", map[string]any{
		"gate_ids":       []string{"gate-beta"},
		"reason":         "重复封锁",
		"established_by": "duty-chief-zhao",
	})
	if err != nil {
		return err
	}
	if err := requireStatus(overlap, http.StatusConflict, "reject overlapping lockdown"); err != nil {
		return err
	}
	if err := requireErrorCode(overlap, "gate_already_locked", "reject overlapping lockdown"); err != nil {
		return err
	}

	// 封锁状态可读取，范围与建立时一致。
	fetched, err := client.get("/lockdowns/" + lockdownID)
	if err != nil {
		return err
	}
	if err := requireStatus(fetched, http.StatusOK, "read active lockdown"); err != nil {
		return err
	}
	fetchedLockdown, err := objectField(fetched.Body, "lockdown")
	if err != nil {
		return err
	}
	fetchedGates, err := arrayField(fetchedLockdown, "gate_ids")
	if err != nil {
		return err
	}
	if len(fetchedGates) != 2 {
		return fmt.Errorf("fetched lockdown scope: expected 2 gates, got %d", len(fetchedGates))
	}

	// 解除封锁需要记录解除人。
	missingLifter, err := client.post("/lockdowns/"+lockdownID+"/lift", map[string]any{
		"lifted_by": "",
	})
	if err != nil {
		return err
	}
	if err := requireStatus(missingLifter, http.StatusBadRequest, "reject lift without lifted_by"); err != nil {
		return err
	}

	lifted, err := client.post("/lockdowns/"+lockdownID+"/lift", map[string]any{
		"lifted_by": "duty-chief-qian",
	})
	if err != nil {
		return err
	}
	if err := requireStatus(lifted, http.StatusOK, "lift regional lockdown"); err != nil {
		return err
	}
	liftedLockdown, err := objectField(lifted.Body, "lockdown")
	if err != nil {
		return err
	}
	liftedStatus, err := stringField(liftedLockdown, "status")
	if err != nil {
		return err
	}
	if liftedStatus != "lifted" {
		return fmt.Errorf("lifted lockdown status: expected lifted, got %q", liftedStatus)
	}
	liftedBy, err := stringField(liftedLockdown, "lifted_by")
	if err != nil {
		return err
	}
	if liftedBy != "duty-chief-qian" {
		return fmt.Errorf("lifted_by: expected duty-chief-qian, got %q", liftedBy)
	}
	if _, err := stringField(liftedLockdown, "lifted_at"); err != nil {
		return err
	}

	gateAlphaAfterLift, err := client.get("/gates/gate-alpha")
	if err != nil {
		return err
	}
	gateAlphaAfter, err := objectField(gateAlphaAfterLift.Body, "gate")
	if err != nil {
		return err
	}
	if value, exists := gateAlphaAfter["lockdown_id"]; exists && value != "" {
		return fmt.Errorf("lockdown marker not cleared after lift: %v", value)
	}

	relift, err := client.post("/lockdowns/"+lockdownID+"/lift", map[string]any{
		"lifted_by": "duty-chief-qian",
	})
	if err != nil {
		return err
	}
	if err := requireStatus(relift, http.StatusConflict, "reject repeated lift"); err != nil {
		return err
	}
	if err := requireErrorCode(relift, "lockdown_not_active", "reject repeated lift"); err != nil {
		return err
	}

	// 解除后按当时真实条件重新调度：封锁前待复核的命令可以继续放行并执行。
	resumeReview, err := client.post("/commands/"+pendingResumeID+"/interlock", map[string]any{
		"verdict":  "clear",
		"verifier": "safety-wu",
		"note":     "警报解除，恢复调度",
	})
	if err != nil {
		return err
	}
	if err := requireStatus(resumeReview, http.StatusOK, "approve pending command after lift"); err != nil {
		return err
	}
	resumeCommand, err := objectField(resumeReview.Body, "command")
	if err != nil {
		return err
	}
	resumeToken, err := stringField(resumeCommand, "execution_token")
	if err != nil {
		return err
	}
	resumeExec, err := client.post("/commands/"+pendingResumeID+"/execute", map[string]any{
		"operator":       "operator-chen",
		"token":          resumeToken,
		"simulate_fault": false,
	})
	if err != nil {
		return err
	}
	if err := requireStatus(resumeExec, http.StatusOK, "execute rescheduled command after lift"); err != nil {
		return err
	}
	resumeGate, err := objectField(resumeExec.Body, "gate")
	if err != nil {
		return err
	}
	resumeAperture, err := floatField(resumeGate, "aperture")
	if err != nil {
		return err
	}
	if resumeAperture != 2.5 {
		return fmt.Errorf("rescheduled gate aperture: expected 2.5, got %v", resumeAperture)
	}

	newAfterLift, err := client.post("/commands", map[string]any{
		"gate_id":         "gate-alpha",
		"target_aperture": 1.5,
		"reason":          "解除后新调度",
		"requested_by":    "dispatcher-lin",
	})
	if err != nil {
		return err
	}
	if err := requireStatus(newAfterLift, http.StatusCreated, "accept new command after lift"); err != nil {
		return err
	}

	// 已撤回的历史不被解除或重新调度改写。
	stillRevoked, err := client.get("/commands/" + approvedID)
	if err != nil {
		return err
	}
	stillRevokedView, err := objectField(stillRevoked.Body, "command")
	if err != nil {
		return err
	}
	stillRevokedStatus, err := stringField(stillRevokedView, "status")
	if err != nil {
		return err
	}
	if stillRevokedStatus != "revoked" {
		return fmt.Errorf("revoked history rewritten: expected revoked, got %q", stillRevokedStatus)
	}

	// 并发：再次封锁与命令执行竞争，两种真实顺序都必须得到一致结果。
	race, err := fixtureCommand(client, "gate-alpha", 2.0, "并发验证", "dispatcher-lin")
	if err != nil {
		return err
	}
	raceID, err := stringField(race, "id")
	if err != nil {
		return err
	}
	raceReview, err := fixtureClearReview(client, raceID, "safety-wu")
	if err != nil {
		return err
	}
	raceToken, err := stringField(raceReview, "execution_token")
	if err != nil {
		return err
	}

	var wait sync.WaitGroup
	var execResult, lockResult apiResult
	var execErr, lockErr error
	wait.Add(2)
	go func() {
		defer wait.Done()
		execResult, execErr = client.post("/commands/"+raceID+"/execute", map[string]any{
			"operator":       "operator-chen",
			"token":          raceToken,
			"simulate_fault": false,
		})
	}()
	go func() {
		defer wait.Done()
		lockResult, lockErr = client.post("/lockdowns", map[string]any{
			"gate_ids":       []string{"gate-alpha"},
			"reason":         "二次区域警报",
			"established_by": "duty-chief-zhao",
		})
	}()
	wait.Wait()
	if execErr != nil {
		return execErr
	}
	if lockErr != nil {
		return lockErr
	}
	if err := requireStatus(lockResult, http.StatusCreated, "establish concurrent lockdown"); err != nil {
		return err
	}
	raceLockdown, err := objectField(lockResult.Body, "lockdown")
	if err != nil {
		return err
	}
	raceLockdownID, err := stringField(raceLockdown, "id")
	if err != nil {
		return err
	}
	raceRevoked, err := arrayField(lockResult.Body, "revoked_commands")
	if err != nil {
		return err
	}
	raceRead, err := client.get("/commands/" + raceID)
	if err != nil {
		return err
	}
	raceView, err := objectField(raceRead.Body, "command")
	if err != nil {
		return err
	}
	raceStatus, err := stringField(raceView, "status")
	if err != nil {
		return err
	}
	if execResult.Status == http.StatusOK {
		// 执行先完成：封锁不再撤回任何授权，命令保持 completed。
		if len(raceRevoked) != 0 {
			return fmt.Errorf("lockdown revoked %d commands after execution completed", len(raceRevoked))
		}
		if raceStatus != "completed" {
			return fmt.Errorf("race command: expected completed, got %q", raceStatus)
		}
	} else {
		// 封锁先生效：授权被撤回，执行按当时真实状态被拒绝。
		if err := requireStatus(execResult, http.StatusConflict, "reject execute racing with lockdown"); err != nil {
			return err
		}
		if err := requireErrorCode(execResult, "command_not_executable", "reject execute racing with lockdown"); err != nil {
			return err
		}
		if len(raceRevoked) != 1 {
			return fmt.Errorf("race lockdown revoked commands: expected 1, got %d", len(raceRevoked))
		}
		if raceStatus != "revoked" {
			return fmt.Errorf("race command: expected revoked, got %q", raceStatus)
		}
	}

	raceGateRead, err := client.get("/gates/gate-alpha")
	if err != nil {
		return err
	}
	raceGate, err := objectField(raceGateRead.Body, "gate")
	if err != nil {
		return err
	}
	raceGateLockdown, err := stringField(raceGate, "lockdown_id")
	if err != nil {
		return err
	}
	if raceGateLockdown != raceLockdownID {
		return fmt.Errorf("race gate marker: expected %q, got %q", raceLockdownID, raceGateLockdown)
	}

	cleanup, err := client.post("/lockdowns/"+raceLockdownID+"/lift", map[string]any{
		"lifted_by": "duty-chief-zhao",
	})
	if err != nil {
		return err
	}
	if err := requireStatus(cleanup, http.StatusOK, "lift concurrent lockdown"); err != nil {
		return err
	}

	fmt.Println("regional-lockdown: lockdown revoked approved authorization atomically, blocked new work, lifted cleanly, and rescheduled on current conditions")
	return nil
}
