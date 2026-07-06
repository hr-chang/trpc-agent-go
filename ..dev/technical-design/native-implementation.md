# Native 实现与优化方案

## 1. 目标

native 主线用于基于 `tRPC-Agent-Go` 实现 Go-native SWE Agent，并围绕 resolved rate 持续优化。

native 主线要回答：

- `tRPC-Agent-Go` 能否承载 SWE-style coding agent 的完整运行链路；
- native agent 能否读取真实 issue、操作真实仓库、生成 official harness 可验证 patch；
- native 结果与 mini-SWE-agent baseline 的差异来自能力、环境、prompt/tool 语义还是工程链路。

## 2. 边界

native agent 不要求复刻 mini-SWE-agent 的内部实现。它可以使用 `tRPC-Agent-Go` 更自然的 runner、tool、
workspace、sandbox、trace 设计。

必须保持一致：

- dataset snapshot；
- case list；
- 模型策略；
- 预算口径；
- official local harness；
- prediction 格式；
- 状态判定；
- 防作弊约束。

允许不同但需记录：

- prompt/scaffold；
- step/action 语义；
- tool 封装形态；
- workspace 管理方式；
- trace schema 细节；
- context management 策略。

## 3. 输入与输出

输入：

- 固定的 SWE-Bench Verified 500 case list；
- 与 baseline 相同的模型策略；
- 与 baseline 一致的 `hints_text` 口径；
- per-instance step/token/time limit；
- workspace / Docker testbed 配置；
- agent 生成并发配置。

输出：

```text
results/runs/<run_id>/
  predictions/native.jsonl
  patches/native/
  traces/native/
  local-harness-report/native/
  run_config.json
  cases.jsonl
```

## 4. 命令入口

```bash
go run ./trpc-agent-go-impl run-native \
  --run-id <run_id> \
  --cases ../data/cases.jsonl \
  --output ../results/runs/<run_id> \
  --model <model> \
  --agent-workers <n> \
  --workspace-mode docker-testbed
```

具体 flags 可以在实现中调整，但必须覆盖：

- run id；
- case list；
- model name / endpoint id / model parameters；
- output dir；
- step/token/time limit；
- workspace mode；
- agent generation concurrency；
- resume / rerun instance id。

## 5. 第一版 agent loop

第一版 native agent 建议使用最小 SWE loop：

- system/developer prompt 描述任务和输出 patch 的约束；
- bash/workspace tool 作为默认工具；
- 每轮模型输出一个 action；
- action 在隔离 workspace 或 Docker testbed 内执行；
- 达到 submit 条件后提取 patch；
- 达到 step/token/time limit 后标记 `incomplete`。

后续可以替换或增强 runner、tool、context management、patch strategy，但必须保持 prediction 和 trace
contract 不变。

## 6. workspace 与执行环境

支持两种执行环境：

1. `docker-testbed`：优先路径，使用 SWE-Bench testbed image 或 local harness 对齐环境；
2. `local-clone`：调试路径，用于快速定位 loader、patch、trace 等问题，不作为最终 verifier。

执行记录至少包含：

- command/action；
- working directory；
- exit code；
- stdout/stderr 摘要；
- timeout；
- start/end time；
- error reason。

## 7. patch 生成

patch 提取规则：

- 以 case `base_commit` 为基准；
- 生成 unified diff；
- 排除构建产物、缓存、临时测试文件和无关未跟踪文件；
- patch apply failed 默认归为 agent 产物问题，除非证明是 harness/image/data/env 问题；
- `empty_patch` 单独归类。

## 8. 优化方法

native 优化围绕同一套证据链进行：

- 优先分析 baseline resolved 但 native unresolved 的 case；
- 对齐必要的执行环境、prompt 输入、submission protocol 和 patch extraction 语义；
- 保留每轮优化前后的 smoke/subset 结果；
- 使用 official local harness 验证优化效果；
- 不用单 case 观感替代 subset 指标；
- 优化目标优先是提升 resolved rate，其次才是降低 token、耗时和实现复杂度。

建议迭代顺序：

1. 跑通 1 case smoke，验证 workspace、tool、patch、trace；
2. 跑通 3-10 case smoke，验证稳定性；
3. 与 baseline 同 case 对比 trajectory 和 patch；
4. 修复明显工程差异；
5. 扩到 11-100 case subset；
6. 进入 full batch。

## 9. 归档要求

必须归档：

- native runner commit；
- prompt/scaffold 版本；
- tool/workspace 配置；
- 模型策略和参数；
- predictions；
- trace；
- patch；
- usage；
- duration；
- official local harness raw report；
- per-case verifier log。

trace 需要足够支撑复查，但归档前必须 scrub secrets。

## 10. 验收

smoke 验收：

- native agent 能完成 10 个以内 case 的串行运行；
- 每个 case 都有 patch 或明确失败原因；
- official local harness 可验证 native predictions；
- trace、usage、duration、patch stats 能导入统一 schema。

优化验收：

- 至少完成一轮 baseline/native 差异分析；
- 优化前后结果可由归档材料对比；
- 不引入防作弊风险；
- 不改变 verifier 口径。

full 验收：

- native 跑完 500 case；
- 500 个 case 都有 native 主状态；
- 未处置的 `infra_error` / `incomplete` 不进入最终结论；
- native 结果、用量、耗时和失败分类可由归档材料复核。

## 11. 风险与复查

重点风险：

- native prompt/tool 语义与 baseline 差异过大，导致结果解释困难；
- workspace 环境与 official harness 环境不一致；
- patch extraction 误收构建产物或临时测试；
- context management 导致模型过早提交或无法定位关键文件；
- trace 中泄露 endpoint、API key 或 header。

复查优先级：

1. baseline resolved 但 native unresolved；
2. native `empty_patch`；
3. native patch apply failed；
4. native token 或 duration 异常；
5. native resolved 但 baseline unresolved，用于分析 native 优势 case。
