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

## 3. 总体实现思路

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

实现时按“先能跑、再可信、再优化”的顺序推进：

1. 先把公共底座做薄：能加载 500 case、写 run config、调用 local harness、导入结果；
2. 再把 mini-SWE-agent 包起来：不改其 agent 行为，只负责配置、启动、收集和转换输出；
3. 然后实现 native 最小闭环：一个 case 进 Docker testbed，模型循环执行命令，最后提取 patch；
4. 等 baseline/native 都能被同一 importer 消费后，再进入差异分析和 native 优化；
5. full run 只复用 smoke/subset 已验证的命令，不另写一套批处理逻辑。

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

CLI 内部建议按以下包边界实现：

```text
cmd/                 子命令、flag、退出码
internal/config/     run_config 读写、默认值、路径解析
internal/dataset/    SWE-Bench Verified loader 和 case list hash
internal/baseline/   mini-SWE-agent wrapper 与输出转换
internal/native/     tRPC-Agent-Go agent loop
internal/workspace/  Docker testbed / local clone workspace
internal/patch/      git diff、patch 过滤、patch stats
internal/verifier/   official local harness wrapper
internal/archive/    run artifact 写入、resume index、secret scrub
internal/report/     comparison 和中英文报告生成
```

每个命令只组合这些包，不在 command handler 里堆业务逻辑。这样 baseline 和 native 可以共用
dataset、patch、verifier、archive、report。

## 7. 实施顺序

1. 公共底座校准：
   - 实现 `doctor`，实际执行 Python/Go/Git/Docker/model endpoint 探测；
   - 实现 `prepare-data`，拉取 dataset revision，生成 500 case list 和 hash；
   - 实现 `verify`，用一条 known prediction 调通 official local harness；
   - 实现 `import` 的最小版本，把 harness 输出映射为主状态；
   - 实现 `report` 的最小版本，从 `comparison.json` 生成表格。

2. baseline 复现：
   - 固定 mini-SWE-agent commit；
   - 生成 mini 配置；
   - 串行跑 1-10 case；
   - 转换 predictions 和 trajectory；
   - local harness 验证；
   - 导入 baseline-only summary。

3. native 实现并优化：
   - 实现 Docker testbed workspace；
   - 实现 bash tool；
   - 实现 agent loop 和 submit/patch extraction；
   - 串行跑 1-10 case；
   - local harness 验证；
   - 用 baseline resolved/native unresolved case 做定点优化。

4. 最终合流：
   - 用同一 case list 跑 baseline 500；
   - 用同一 case list 跑 native 500；
   - 分别调用 local harness；
   - 用同一 importer 生成 `cases.jsonl` 和 `comparison.json`；
   - 用同一 report generator 生成中英文报告；
   - benchmark PR 合入后更新主仓库 submodule 指针。

## 8. 当前开放点

这些问题不阻塞技术方案评审，但需要在 full run 前落定：

- 最终模型、endpoint、参数和预算；
- mini-SWE-agent commit 和配置；
- SWE-Bench dataset revision / package version；
- Docker daemon 是否继续使用 `tcp://localhost:2375`，以及 Docker Root Dir 容量是否足够；
- full run 的具体 run id、结果归档路径和长期保留策略。
