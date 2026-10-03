# 泄洪闸门操作协调服务

## 项目目标

该服务用于水工控制室协调泄洪闸门的命令申请、安全联锁复核和单次开度调整。系统在同一闸门上只允许一条已通过复核且尚未结束的执行命令，任何冲突都必须被显式拒绝，不能覆盖已经占用的闸门。

## 使用角色

- 运行协调员：登记闸门并提交开度调整命令。
- 安全复核员：提交联锁结论，决定命令是否通过。
- 闸门操作员：凭执行令牌启动命令并登记执行结果。

## 核心实体

- `gate`：闸门及其当前开度、运行状态、最大开度和活动命令。
- `gate_command`：一次开度调整申请，包含目标开度、申请原因、状态和执行令牌。
- `interlock_decision`：安全复核结论，状态为 clear 或 blocked。

## 业务流程

### register-gate

运行协调员通过 `POST /gates` 登记闸门。服务校验标识、名称、最大开度和初始开度，写入空闲状态；重复标识返回稳定冲突错误，不覆盖原记录。

### request-gate-command

运行协调员通过 `POST /commands` 提交开度调整命令。服务读取闸门当前开度和限制，校验目标开度与申请信息，创建待复核命令并冻结起始开度。

### review-safety-interlock

安全复核员通过 `POST /commands/{id}/interlock` 提交复核结论。clear 结论会原子占用闸门并签发一次执行令牌；blocked 结论会把命令置为 rejected。其他命令先占用同一闸门时，当前 clear 结论返回冲突且不改变闸门。

### execute-gate-command

闸门操作员通过 `POST /commands/{id}/execute` 提交执行令牌。服务校验命令、令牌、闸门占用关系和目标开度，在临界区内完成 approved、executing、completed 或 aborted 的转换，并同步更新闸门状态。令牌在命令结束后失效，不能复用。

### establish-region-lockdown

值班负责人通过 `POST /lockdowns` 提交区域封锁，必须显式给出闸门范围 `gate_ids` 和业务依据（`reason`、`authority`）。服务在单一临界区内原子完成：标记范围内闸门为封锁态，并把范围内所有 approved 但尚未开始执行的命令转为 withdrawn、同步释放闸门占用并作废执行令牌。pending 命令保留、executing 命令继续到终态、completed/aborted/rejected 历史保持不变。

封锁期间范围内闸门不能创建新命令，clear 复核（新占用）返回冲突；blocked 复核不占用闸门，仍可拒绝命令。范围外闸门不受影响。重复封锁（同 ID 或范围重叠）返回稳定冲突。

### release-region-lockdown

值班负责人通过 `POST /lockdowns/{id}/release` 解除封锁。解除只清除封锁标记，不改变闸门运行状态；解除后范围内按当时真实状态重新可调度（pending 可复核、可新建命令、可对同一范围再次封锁）。重复解除返回稳定冲突。`GET /lockdowns/{id}` 返回封锁记录和被撤回命令清单。

## 状态与规则

- `gate` 状态：idle、reserved、moving、faulted；另带封锁标记 `lockdown_id`（不改变运行状态，仅表示该闸门处于区域封锁内）。
- `gate_command` 状态：pending、approved、rejected、executing、completed、aborted、withdrawn。
- `region_lockdown` 状态：active、released。
- 只有 pending 命令可以复核。
- 只有 approved 命令可以执行，且执行令牌必须匹配。
- clear 复核与闸门占用必须同时成功；任一步失败时两者都不写入。
- blocked 复核使命令 rejected，不会占用闸门。
- 模拟故障会令命令 aborted、闸门 faulted，并保留最终开度。
- 已完成或已中止的命令不能再次执行。
- 只有 approved 未开始的命令可被封锁撤回；撤回与闸门占用释放在同一临界区内完成，终态与执行中历史不可改写。
- 封锁建立、重叠判定、撤回和占用释放全部在同一把仓库互斥锁下提交；失败不留部分标记。
- 解除封锁是单向转换；解除后按当时真实状态重新调度，可再次封锁。

## 模块边界

- `cmd/server`：进程入口、监听地址和退出控制。
- `internal/httpapi`：HTTP 路由、JSON 解码、状态码映射。
- `internal/coordination`：工作流编排、命令标识和执行令牌签发。
- `internal/domain`：实体、不变量和状态转换规则。
- `internal/storage`：带互斥保护的内存仓库和原子写入。
- `internal/contracts`：API 请求、响应和稳定错误结构。
- `internal/bootstrap`：运行时依赖装配。

## 接口

- `GET /health`
- `POST /gates`
- `GET /gates/{id}`
- `POST /commands`
- `GET /commands/{id}`
- `POST /commands/{id}/interlock`
- `POST /commands/{id}/execute`
- `POST /lockdowns`
- `GET /lockdowns/{id}`
- `POST /lockdowns/{id}/release`

错误响应统一为：

```json
{"code":"stable_error_code","message":"可读说明"}
```

## 验证计划

本初始化基线刻意不生成单元测试、夹具或浏览器测试文件，也不声明 `test_command`。后续任务阶段负责补充测试，并在该阶段建立红绿验证。当前每条工作流通过 `cmd/workflowcheck` 启动真实 HTTP 服务并调用公开接口，校验成功状态、持久化结果和关键失败分支。
