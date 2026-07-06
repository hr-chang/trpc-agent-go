# Baseline 复现方案

## 1. 目标

baseline 复现用于建立可信参照系。第一版只复现 mini-SWE-agent，不增加其他 baseline。

baseline 复现要回答：

- mini-SWE-agent 在本项目固定的数据集、模型策略和 official local harness 下能否稳定运行；
- mini-SWE-agent 产出的 predictions、trajectory、usage、duration 是否能进入统一归档；
- 本地复现结果与公开背景结果是否存在明显差异，以及差异是否能被解释。

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

## 7. 校准与验收

smoke 验收：

- 10 个以内 case 串行可跑通；
- 每个 case 都有 prediction 或明确失败原因；
- official local harness 可验证 baseline predictions；
- result importer 可生成 baseline case-level result；
- baseline-only summary 可从归档材料重算。

full 验收：

- baseline 跑完 500 case；
- 500 个 case 都有 baseline 主状态；
- 未处置的 `infra_error` / `incomplete` 不进入最终结论；
- baseline 结果、用量、耗时和失败分类可由归档材料复核。

## 8. 风险与复查

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
