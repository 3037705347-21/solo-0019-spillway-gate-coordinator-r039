# 泄洪闸门操作协调服务

这是一个无外部依赖的 Go 后端，用本地内存仓库协调闸门登记、开度调整命令、安全联锁复核和执行结果。服务强调单闸门活动命令互斥、原子状态转换、执行令牌生命周期和稳定错误传播。

## 运行

```sh
go build ./...
go run ./cmd/server --addr 127.0.0.1:18119
```

健康入口为 `GET http://127.0.0.1:18119/health`。

## API

```text
POST /gates
GET  /gates/{id}
POST /commands
GET  /commands/{id}
POST /commands/{id}/interlock
POST /commands/{id}/execute
POST /lockdowns
GET  /lockdowns/{id}
POST /lockdowns/{id}/release
```

登记闸门示例：

```json
{
  "id": "gate-east",
  "name": "东侧泄洪闸",
  "max_aperture": 4.5,
  "initial_aperture": 0.5
}
```

提交命令示例：

```json
{
  "gate_id": "gate-east",
  "target_aperture": 2.25,
  "reason": "上游水位调整",
  "requested_by": "dispatcher-lin"
}
```

联锁复核示例：

```json
{
  "verdict": "clear",
  "verifier": "safety-wu",
  "note": "上下游条件满足"
}
```

执行示例：

```json
{
  "operator": "operator-chen",
  "token": "复核响应中的 execution_token",
  "simulate_fault": false
}
```

建立区域封锁示例（显式范围与业务依据；已批准未开始的命令会随闸门释放被撤回）：

```json
{
  "reason": "上游区域警报，暂停指定范围闸门调整",
  "authority": "duty-lead-wang",
  "gate_ids": ["gate-east", "gate-north"]
}
```

解除封锁示例：

```json
{
  "released_by": "duty-lead-wang",
  "note": "区域警报解除"
}
```

封锁期间范围内新命令与 clear 复核返回 `409 gate_region_locked`；重复或重叠封锁返回 `409`，重复解除返回 `409 lockdown_not_active`。

## 目录

- `cmd/server`：服务进程。
- `cmd/workflowcheck`：四条工作流的真实 HTTP 检查入口。
- `internal/bootstrap`：依赖装配。
- `internal/contracts`：公开数据契约。
- `internal/coordination`：业务工作流。
- `internal/domain`：实体、不变量和状态机。
- `internal/httpapi`：路由、解码和错误映射。
- `internal/storage`：互斥保护的内存仓库。

## 工作流检查

```sh
go run ./cmd/workflowcheck --workflow register-gate
go run ./cmd/workflowcheck --workflow request-gate-command
go run ./cmd/workflowcheck --workflow review-safety-interlock
go run ./cmd/workflowcheck --workflow execute-gate-command
go run ./cmd/workflowcheck --workflow establish-region-lockdown
go run ./cmd/workflowcheck --workflow concurrent-region-lockdown
```

这些检查会创建临时 HTTP 服务并调用真实路由。它们不调用外部网络，也不依赖数据库。

## 测试状态

领域状态机和内存仓库的封锁不变量由 `internal/domain`、`internal/storage` 下的单元测试覆盖（含 `-race` 并发场景）；端到端流程由 `cmd/workflowcheck` 的真实 HTTP 检查覆盖：

```sh
go test -race ./...
```
