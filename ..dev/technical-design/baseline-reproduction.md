# Baseline 复现方案

## 1. 目标

baseline 复现用于建立可信参照系。第一版只复现 mini-SWE-agent，不增加其他 baseline。

baseline 复现要回答：

- mini-SWE-agent 在本项目固定的数据集、模型策略和自建 official local harness 环境下能否稳定运行；
- mini-SWE-agent 产出的 predictions、trajectory、usage、duration 是否能进入统一归档；
- 本地复现结果是否能作为后续 native agent 的公平参照；与公开背景结果的差异只做审计解释。

## 2. 边界

baseline adapter 只做非语义适配，不主动改造 mini-SWE-agent 的 agent 行为。

允许：

- 注入模型 endpoint、API key、模型参数；
- 配置 step/token/time budget；
- 设置环境变量；
- 适配输出路径；
- 将原始输出转换为统一 `predictions.jsonl` 和归档 schema；
- 记录 wrapper、转换脚本和 runner commit。

不允许：

- 改写 prompt 语义；
- 改变 agent 可见输入；
- 扩大 tool/action 空间；
- 修改 patch submission 语义；
- 给 agent 注入 gold patch、test patch、`FAIL_TO_PASS`、`PASS_TO_PASS`。

## 3. 输入与输出

输入：

- 固定的 SWE-Bench Verified 500 case list；
- baseline 复现链路决定的 `hints_text` 口径；
- 已确认模型策略；
- mini-SWE-agent commit 和配置；
- 当前冻结的自建 verifier、Docker/testbed image 集合和 harness patch；
- per-instance step/token/time limit；
- agent 生成并发配置。

输出：

```text
results/runs/<run_id>/
  predictions/baseline.jsonl
  patches/baseline/
  traces/baseline/
  local-harness-report/baseline/
  run_config.json
  cases.jsonl
```

## 4. 命令入口

```bash
go run ./trpc-agent-go-impl run-mini \
  --run-id <run_id> \
  --cases ../data/cases.jsonl \
  --output ../results/runs/<run_id> \
  --model <model> \
  --agent-workers <n>
```

具体 flags 可以在实现中调整，但必须覆盖：

- run id；
- case list；
- model name / endpoint id / model parameters；
- mini-SWE-agent checkout 或 executable；
- output dir；
- step/token/time limit；
- agent generation concurrency；
- resume / rerun instance id。

## 5. 运行流程

```text
prepare-data
  -> load case list
  -> build mini-SWE-agent config
  -> run mini-SWE-agent batch
  -> collect predictions / trajectories / usage / duration
  -> normalize predictions
  -> verify with official local harness
  -> import baseline result
```

具体实现分三层：

1. 配置层：根据本项目 run config 生成 mini-SWE-agent 可识别的配置；
2. 启动层：按 case list 调用 mini-SWE-agent batch runner；
3. 转换层：把 mini 原始输出转换为本项目统一 predictions、trace、usage、duration。

### 5.1 配置生成

实现 `baseline.BuildMiniConfig(runConfig, cases)`：

- 写入模型名、endpoint、temperature、max tokens 等参数；
- 写入 step/time/token limit；
- 写入 case list 或 instance filter；
- 写入 `hints_text` 使用口径；
- 写入输出目录；
- 记录 mini-SWE-agent commit 和配置文件 hash。

生成后的配置必须归档，不能只存在临时目录。

### 5.2 启动 mini-SWE-agent

实现 `baseline.RunMini(config)`：

- 使用外部进程调用 mini-SWE-agent，不把 mini 代码嵌入 Go；
- stdout/stderr 同时写日志文件；
- 每个 case 完成后落盘中间结果，支持 resume；
- 进程级失败保留 exit code 和命令行；
- agent 生成并发由 run config 显式指定；baseline/native 应复用同一并发策略，实际值按 endpoint
  健康度校准。

### 5.3 输出转换

实现 `baseline.ImportMiniOutput(rawDir)`：

- 找到 mini 原始 trajectory；
- 提取或转换 `model_patch`；
- 生成 official harness 需要的 `predictions/baseline.jsonl`；
- 提取 usage 和 duration；
- 将每个 case 的 trace 写入 `traces/baseline/<instance_id>.json`；
- 将 patch 写入 `patches/baseline/<instance_id>.patch`。

转换器必须保留转换前原始文件路径，方便复查。

## 6. 归档要求

必须归档：

- mini-SWE-agent commit；
- mini-SWE-agent config；
- 模型策略和参数；
- wrapper / converter commit；
- predictions；
- trajectory；
- patch；
- usage；
- duration；
- official local harness raw report；
- per-case verifier log。

如 baseline 复现需要格式转换，必须记录转换前后的文件路径和转换逻辑版本。

## 7. 错误处理

baseline 复现中的错误按来源处理：

- mini 进程失败：记录 process error，case 暂记 `incomplete`，修复后重跑；
- 单 case 无 trajectory：记录 missing trajectory，优先检查 mini resume/index；
- patch 为空：进入 `empty_patch` 候选，仍交由 importer 统一判定；
- predictions 格式非法：converter 失败，不进入 verifier；
- harness 失败：由公共底座判断是 `infra_error` 还是 patch/result 问题。

## 8. 校准与验收

smoke 验收：

- 10 个以内 case smoke 可跑通；
- 每个 case 都有 prediction 或明确失败原因；
- official local harness 可验证 baseline predictions；
- result importer 可生成 baseline case-level result；
- baseline-only summary 可从归档材料重算。

full 验收：

- baseline 跑完 500 case；
- 500 个 case 都有 baseline 主状态；
- 未处置的 `infra_error` / `incomplete` 不进入最终结论；
- baseline 结果、用量、耗时和失败分类可由归档材料复核。

## 9. 风险与复查

重点风险：

- mini-SWE-agent 版本或配置与公开背景结果不一致；
- 模型 endpoint 参数不同导致结果不可解释；
- `hints_text` 口径与 native 不一致；
- predictions 格式转换引入 patch 语义变化；
- local harness / Docker 环境问题被误算为 agent 失败。

复查优先级：

1. baseline 与公开背景结果差异明显的 repo 或 case；
2. baseline `empty_patch`；
3. baseline patch apply failed；
4. baseline verifier infra error；
5. baseline usage 或 duration 异常 case。

baseline 复现完成后，native 优化应优先使用 baseline resolved 但 native unresolved 的 case 做差异分析。
