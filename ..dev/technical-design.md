# SWE-Bench Verified 评测技术方案

## 1. 目标

本方案用于落地 `SWE-Bench Verified 评测需求文档` 中定义的能力证明：在
`trpc-agent-go-benchmark` 仓库新增 `benchmark/swebench`，实现 baseline 复现、Go-native
SWE Agent、official local harness 验证、结果归档和中英文报告生成。

技术方案只固定对外 contract、证据链和运行口径，不把 agent 内部 runner/tool 形态写死。实现可以迭代，
但最终必须能支撑 500 case 全量评测和可复核报告。

工程实施初步拆成两个主交付：

1. baseline 复现：复现 mini-SWE-agent 链路，建立可信参照系；
2. native 实现并优化：基于 `tRPC-Agent-Go` 实现 Go-native SWE Agent，并围绕 resolved rate 持续优化。

dataset、verifier、archive、report 是两条主线共用的公共底座，不单独作为第三个能力目标。

## 2. 总体拆分与架构

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

核心设计原则：

- baseline 和 native 共用同一 dataset snapshot、case list、模型策略、预算口径和 verifier；
- official local harness 是唯一主验证标准；
- agent 只可见 `problem_statement` 和 base commit 仓库状态，不可见 gold patch、test patch 或判定测试列表；
- 运行产物先结构化归档，再从结构化归档生成报告，避免人工改报告造成口径漂移；
- smoke/subset/full batch 使用同一套命令和 schema，只是 case list 与并发不同；
- baseline 复现优先用于校准环境、数据、模型配置、harness 和报告口径；
- native full batch 原则上在 baseline 参照系可信后启动，避免把环境问题误判为 agent 能力问题。

## 3. 仓库与目录

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

## 4. 命令入口

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

### doctor

检查环境是否能进入 smoke/full run：

- Python、Go、Git、mini-SWE-agent、SWE-Bench package；
- Docker 访问方式，当前专用容器需要 `DOCKER_HOST=tcp://localhost:2375`；
- Docker daemon version、Docker Root Dir、可用磁盘；
- Hugging Face、GitHub、PyPI、Docker registry 网络访问；
- 模型 endpoint 连通性和 usage 字段可用性。

### prepare-data

固化数据集快照和 case list：

- 加载 `princeton-nlp/SWE-bench_Verified` 的 `test` split；
- 输出排序后的 500 个 `instance_id`；
- 生成 `cases.jsonl` 和 `cases.sha256`；
- 记录 dataset id、revision、loader version 和 `hints_text` 使用口径。

### run-mini

运行或导入 mini-SWE-agent baseline：

- 复用原版 mini-SWE-agent 行为；
- 只允许做模型配置、环境变量、输出格式转换等非语义适配；
- 输出 baseline predictions、trajectory、usage、duration；
- 支持 resume 和按 `instance_id` 重跑。

### run-native

运行 Go-native SWE Agent：

- 读取同一 case list；
- 为每个 case 准备隔离 workspace 或 Docker testbed；
- 调用 `tRPC-Agent-Go` agent loop；
- 记录每一步 action、observation、usage、duration；
- 提取相对 base commit 的 unified diff；
- 输出 official harness 兼容 predictions。

### verify

调用 official local harness：

```bash
DOCKER_HOST=tcp://localhost:2375 \
python -m swebench.harness.run_evaluation \
  --dataset_name princeton-nlp/SWE-bench_Verified \
  --predictions_path <predictions.jsonl> \
  --max_workers <num_workers> \
  --run_id <run_id>
```

要求：

- baseline/native 分别生成 harness report；
- 保存原始 report、logs、stdout/stderr 和命令参数；
- harness 并发与 agent 生成并发分开记录。

### import

把 runner 输出和 harness 输出导入统一 schema：

- 合并 predictions、patches、traces、usage、duration、verifier result；
- 计算 changed files 和 patch 行数；
- 按统一判定优先级生成主状态；
- 输出 `cases.jsonl` 和 `comparison.json`。

### report

从结构化结果生成报告：

- 英文：`results/REPORT.md`；
- 中文：`results/REPORT.zh_CN.md`；
- 同一份 `comparison.json` 作为数据源；
- 报告包含总体 resolved rate、五类主状态、per-repo 结果、失败分类、资源使用和复现路径。

## 5. 核心模块

模块按“公共底座 + baseline 复现 + native 实现优化”组织。公共底座保证两条主线使用同一输入、验证和归档口径；
baseline 复现建立参照系；native 主线承载 `tRPC-Agent-Go` 能力实现和指标优化。

### 5.1 dataset

职责：

- 加载 SWE-Bench Verified；
- 生成稳定 case list；
- 计算 case list hash；
- 输出 smoke/subset/full batch 的 case selection。

case list hash 采用规范化算法：

```text
sha256(join(sort(instance_id), "\n") + "\n")
```

### 5.2 baseline 复现 adapter

职责：

- 安装或定位 mini-SWE-agent；
- 注入模型 endpoint、参数和预算；
- 启动 batch run；
- 导入 mini-SWE-agent trajectory 和 predictions；
- 记录 mini-SWE-agent commit、配置和任何 wrapper 行为。

baseline adapter 不应改变以下内容：

- agent 可见输入；
- tool/action 空间；
- prompt 语义；
- patch submission 语义。

baseline 复现的完成标准：

- smoke subset 能稳定产出 predictions；
- predictions 能被 official local harness 验证；
- trajectory、usage、duration 能导入统一 schema；
- baseline-only summary 能从归档材料复算。

### 5.3 native 实现与优化 agent

第一版 native agent 建议使用最小 SWE loop：

- system/developer prompt 描述任务和输出 patch 的约束；
- bash/workspace tool 作为默认工具；
- 每轮模型输出一个 action；
- action 在隔离 workspace 或 Docker testbed 内执行；
- 达到 submit 条件后提取 patch；
- 达到 step/token/time limit 后标记 incomplete。

后续可以替换或增强 runner、tool、context management、patch strategy，但必须保持 prediction 和 trace
contract 不变。

native 优化围绕同一套证据链进行：

- 优先分析 baseline resolved 但 native unresolved 的 case；
- 对齐必要的执行环境、prompt 输入、submission protocol 和 patch extraction 语义；
- 保留每轮优化前后的 smoke/subset 结果，避免只凭单 case 观感修改；
- 优化目标优先是提升 resolved rate，其次才是降低 token、耗时和实现复杂度。

### 5.4 workspace / execution

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

### 5.5 patch

patch 提取规则：

- 以 case `base_commit` 为基准；
- 生成 unified diff；
- 排除构建产物、缓存、临时测试文件和无关未跟踪文件；
- patch apply failed 默认归为 agent 产物问题，除非证明是 harness/image/data/env 问题；
- `empty_patch` 单独归类。

### 5.6 verifier

verifier wrapper 只负责调用官方 local harness 和保存输出，不解释测试语义。

需要记录：

- swebench package version；
- harness command；
- `DOCKER_HOST`；
- `--max_workers`；
- Docker daemon version；
- Docker image tag/digest；
- raw report 和 per-case logs。

### 5.7 archive / report

archive 是报告的唯一数据源。报告生成器不得从零散日志中临时拼指标。

生成顺序：

```text
runner artifacts + harness artifacts
  -> cases.jsonl
  -> comparison.json
  -> REPORT.md / REPORT.zh_CN.md
```

## 6. 数据 schema

### 6.1 run_config.json

```json
{
  "run_id": "swebench-verified-20260706-full",
  "dataset": {
    "id": "princeton-nlp/SWE-bench_Verified",
    "split": "test",
    "revision": "<revision>",
    "case_list_hash": "<sha256>",
    "hints_text_policy": "follow-mini"
  },
  "repos": {
    "main_repo_commit": "<sha>",
    "benchmark_repo_commit": "<sha>",
    "runner_commit": "<sha>",
    "mini_swe_agent_commit": "<sha>"
  },
  "model": {
    "strategy": "single",
    "name": "<model>",
    "endpoint_id": "<endpoint-id>",
    "parameters": {}
  },
  "limits": {
    "step_limit": 250,
    "token_limit": 0,
    "instance_timeout_seconds": 3600
  },
  "concurrency": {
    "agent_generation": 15,
    "harness_max_workers": 15
  },
  "verifier": {
    "type": "official-local-harness",
    "swebench_version": "<version>",
    "docker_host": "tcp://localhost:2375",
    "docker_server_version": "19.03.15"
  }
}
```

### 6.2 cases.jsonl

每行一条 case，baseline/native 各自记录一个主状态：

```json
{
  "instance_id": "django__django-12345",
  "repo": "django/django",
  "base_commit": "<sha>",
  "baseline": {
    "main_status": "resolved",
    "failure_reason": "",
    "patch_path": "patches/baseline/django__django-12345.patch",
    "trace_path": "traces/baseline/django__django-12345.json",
    "verifier_result_ref": "local-harness-report/baseline/...",
    "duration_seconds": 1234,
    "usage": {
      "prompt_tokens": 0,
      "completion_tokens": 0,
      "total_tokens": 0,
      "api_calls": 0
    }
  },
  "native": {
    "main_status": "unresolved",
    "failure_reason": "failed official harness",
    "patch_path": "patches/native/django__django-12345.patch",
    "trace_path": "traces/native/django__django-12345.json",
    "verifier_result_ref": "local-harness-report/native/...",
    "duration_seconds": 1200,
    "usage": {
      "prompt_tokens": 0,
      "completion_tokens": 0,
      "total_tokens": 0,
      "api_calls": 0
    }
  },
  "patch_stats": {
    "baseline": {
      "changed_files": ["path/to/file.py"],
      "added_lines": 10,
      "deleted_lines": 2
    },
    "native": {
      "changed_files": ["path/to/file.py"],
      "added_lines": 8,
      "deleted_lines": 1
    }
  }
}
```

## 7. 状态判定

状态判定对象是一个 agent-case pair。baseline 和 native 对同一个 case 分别判定，主状态集合固定为：

```text
resolved | unresolved | empty_patch | infra_error | incomplete
```

判定优先级：

1. `incomplete`：agent 或 verifier 未完成；
2. `infra_error`：harness、Docker、镜像、网络、数据基线等工程原因导致无法可信判定；
3. `empty_patch`：agent 完成但 `model_patch` 为空；
4. `resolved` / `unresolved`：以 official local harness 结果为准。

最终对外报告不得包含未处置的 `infra_error` / `incomplete`。这些状态只能进入内部失败运行记录，或在修复后重跑。

## 8. 防作弊与输入隔离

runner 必须保证 agent 输入不包含：

- gold `patch`；
- `test_patch`；
- `FAIL_TO_PASS`；
- `PASS_TO_PASS`；
- harness 判定测试列表或由其派生的提示。

允许输入：

- `problem_statement`；
- repo/base commit 对应工作区；
- 按 baseline 复现链路决定是否使用的 `hints_text`。

归档 trace 时需要 scrub secrets，避免 API key、Authorization header、临时凭证进入结果目录。

## 9. 并发与资源

并发分两类：

- agent 生成并发：模型/API 密集型；
- harness 验证并发：Docker/磁盘密集型，对应 `--max_workers`。

默认档位：

| 规模 | Agent 生成并发 | Harness 验证并发 |
| --- | ---: | ---: |
| <= 10 cases | 1 | <= case count |
| 11-100 cases | <= 5 | <= 5 |
| 101-499 cases | <= 15 | <= 15 |
| 500 full batch | 15，最高 20 | 从 15 开始，按阶段零校准调整 |

进入 full run 前必须完成：

- Docker 访问方式确认；
- Docker Root Dir 容量确认；
- gold patch 或 known case harness smoke；
- 模型 endpoint smoke；
- resume/retry 策略验证。

## 10. 复查策略

需要重点复查的 case：

- baseline resolved 但 native unresolved；
- native resolved 但 baseline unresolved；
- `empty_patch`；
- patch apply failed；
- verifier infra error；
- 与公开/预期结果差异明显的 repo 或 case。

复查材料至少包括：

- problem statement；
- patch；
- trace；
- verifier log；
- changed files；
- patch stats；
- usage/duration；
- workspace 保存路径，如果该 case 被指定保留现场。

## 11. 实施阶段

### 阶段零：公共底座校准

输出：

- `doctor` 结果；
- Docker/SWE-Bench harness smoke 记录；
- dataset loader smoke；
- run_config 模板。

退出条件：数据加载、Docker/local harness、模型 endpoint、归档目录和基础 schema 都能在 smoke case 上跑通。

### 第一部分：baseline 复现

输出：

- mini-SWE-agent smoke predictions；
- baseline trajectory 导入；
- baseline harness smoke；
- baseline-only summary。

退出条件：mini-SWE-agent 链路在 smoke/subset 上稳定，baseline 结果能被 official local harness 验证并导入报告 schema。

### 第二部分：native 实现并优化

输出：

- native smoke predictions；
- trace/usage/patch 归档；
- native harness smoke；
- 与 baseline 同 schema 的 case-level result。

退出条件：native agent 能稳定产出 official local harness 可验证 predictions，并完成至少一轮基于 baseline 差异的优化。

### 最终合流：全量运行与报告

输出：

- baseline 500 case run；
- native 500 case run；
- 两组 official local harness report；
- `cases.jsonl`、`comparison.json`、`comparison.md`。

- `benchmark/swebench/README.md`；
- `results/REPORT.md`；
- `results/REPORT.zh_CN.md`；
- artifact index；
- benchmark PR；
- 主仓库 submodule 指针更新。

## 12. 当前开放点

这些问题不阻塞技术方案评审，但需要在 full run 前落定：

- 最终模型、endpoint、参数和预算；
- mini-SWE-agent commit 和配置；
- SWE-Bench dataset revision / package version；
- Docker daemon 是否继续使用 `tcp://localhost:2375`，以及 Docker Root Dir 容量是否足够；
- full run 的具体 run id、结果归档路径和长期保留策略。
