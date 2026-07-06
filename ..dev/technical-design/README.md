# SWE-Bench Verified 技术方案总览

## 1. 目标

本方案用于落地 `SWE-Bench Verified 评测需求文档` 中定义的能力证明：在
`trpc-agent-go-benchmark` 仓库新增 `benchmark/swebench`，实现 baseline 复现、Go-native
SWE Agent、official local harness 验证、结果归档和中英文报告生成。

技术方案只固定对外 contract、证据链和运行口径，不把 agent 内部 runner/tool 形态写死。实现可以迭代，
但最终必须能支撑 500 case 全量评测和可复核报告。

## 2. 工作拆分

工程实施拆成两个主交付：

1. baseline 复现：复现 mini-SWE-agent 链路，建立可信参照系；
2. native 实现并优化：基于 `tRPC-Agent-Go` 实现 Go-native SWE Agent，并围绕 resolved rate 持续优化。

dataset、verifier、archive、report 是两条主线共用的公共底座，不单独作为第三个能力目标。

## 3. 总体架构

```text
SWE-Bench Verified dataset
  -> case list / snapshot
  -> run config

公共底座：
  doctor / prepare-data / official local harness / archive / report

主线 A：baseline 复现
  mini-SWE-agent runner
  -> baseline predictions / traces / usage
  -> official local harness
  -> baseline case-level results

主线 B：native 实现并优化
  tRPC-Agent-Go native runner
  -> native predictions / traces / usage
  -> official local harness
  -> native case-level results

最终合流：
  baseline results + native results
  -> comparison.json / cases.jsonl
  -> REPORT.md / REPORT.zh_CN.md
```

## 4. 设计原则

- baseline 和 native 共用同一 dataset snapshot、case list、模型策略、预算口径和 verifier；
- official local harness 是唯一主验证标准；
- agent 只可见 `problem_statement` 和 base commit 仓库状态，不可见 gold patch、test patch 或判定测试列表；
- 运行产物先结构化归档，再从结构化归档生成报告，避免人工改报告造成口径漂移；
- smoke/subset/full batch 使用同一套命令和 schema，只是 case list 与并发不同；
- baseline 复现优先用于校准环境、数据、模型配置、harness 和报告口径；
- native full batch 原则上在 baseline 参照系可信后启动，避免把环境问题误判为 agent 能力问题。

## 5. 仓库与目录

主仓库只保留需求/方案文档和最终 submodule 指针。实际实现放在 benchmark submodule：

```text
benchmark/swebench/
  README.md
  data/
    README.md
    cases.jsonl
    cases.sha256
  trpc-agent-go-impl/
    go.mod
    main.go
    cmd/
    internal/
      dataset/
      baseline/
      native/
      model/
      workspace/
      patch/
      verifier/
      archive/
      report/
  results/
    README.md
    REPORT.md
    REPORT.zh_CN.md
    runs/
      <run_id>/
        run_config.json
        cases.jsonl
        predictions/
          baseline.jsonl
          native.jsonl
        patches/
          baseline/
          native/
        traces/
          baseline/
          native/
        local-harness-report/
          baseline/
          native/
        comparison.json
        comparison.md
```

说明：

- `data/` 只保存可复核的 case list、hash 和数据准备说明，不默认提交完整第三方数据集；
- `trpc-agent-go-impl/` 是 Go-native agent 和评测编排入口；
- `results/runs/<run_id>/` 保存每次有效运行的完整证据链；
- 顶层 `results/REPORT.md` 和 `REPORT.zh_CN.md` 只引用最终有效 run，不内嵌完整 patch。

## 6. 命令入口

建议实现一个 Go CLI 作为主编排入口，脚本只做环境封装。命令名称可以在实现时调整，但功能边界保持稳定。

公共底座命令：

```bash
go run ./trpc-agent-go-impl doctor
go run ./trpc-agent-go-impl prepare-data
go run ./trpc-agent-go-impl verify
go run ./trpc-agent-go-impl import
go run ./trpc-agent-go-impl report
```

baseline 复现命令：

```bash
go run ./trpc-agent-go-impl run-mini
```

native 实现与优化命令：

```bash
go run ./trpc-agent-go-impl run-native
```

## 7. 实施顺序

1. 公共底座校准：
   - `doctor` 结果；
   - Docker/SWE-Bench harness smoke 记录；
   - dataset loader smoke；
   - run config 模板。

2. baseline 复现：
   - mini-SWE-agent smoke predictions；
   - baseline trajectory 导入；
   - baseline harness smoke；
   - baseline-only summary。

3. native 实现并优化：
   - native smoke predictions；
   - trace/usage/patch 归档；
   - native harness smoke；
   - 与 baseline 同 schema 的 case-level result；
   - 基于 baseline/native 差异做定点优化。

4. 最终合流：
   - baseline 500 case run；
   - native 500 case run；
   - 两组 official local harness report；
   - `cases.jsonl`、`comparison.json`、`comparison.md`；
   - `README.md`、`REPORT.md`、`REPORT.zh_CN.md`；
   - benchmark PR 和主仓库 submodule 指针更新。

## 8. 当前开放点

这些问题不阻塞技术方案评审，但需要在 full run 前落定：

- 最终模型、endpoint、参数和预算；
- mini-SWE-agent commit 和配置；
- SWE-Bench dataset revision / package version；
- Docker daemon 是否继续使用 `tcp://localhost:2375`，以及 Docker Root Dir 容量是否足够；
- full run 的具体 run id、结果归档路径和长期保留策略。
