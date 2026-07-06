# 公共底座方案

公共底座为 baseline 复现和 native 实现提供同一套 dataset、verifier、archive、report、状态判定和资源口径。
它的目标不是单独证明能力，而是确保两条主线可比、可复核、可重跑。

## 1. 职责边界

公共底座负责：

- 环境检查；
- 数据集加载和 case list 固化；
- official local harness 调用；
- 运行结果导入；
- 证据链归档；
- 报告生成；
- 主状态判定；
- 防作弊和 secret scrub 基础规则。

公共底座不负责：

- 改造 mini-SWE-agent 行为；
- 决定 native agent 的内部 runner/tool 设计；
- 用自造 verifier 替代 official local harness；
- 对 full run 指标做人工修正。

## 2. 命令入口

### doctor

实现为一组实际探测命令，而不是只打印配置。每个探测项输出 `ok/warn/fail`、原始值和修复建议。

需要执行：

- `python --version`、`go version`、`git --version`；
- `python -c "import swebench"`，读取 SWE-Bench package version；
- `DOCKER_HOST=tcp://localhost:2375 docker info`，读取 server version、Docker Root Dir、CPU、memory；
- `df -h` 检查 workspace/cache 目标路径；
- 对 Hugging Face、GitHub、PyPI、Docker registry 做轻量连通性检查；
- 对模型 endpoint 发一个最小 completion 请求，确认认证、模型名、usage 字段；
- 检查 `results/runs/<run_id>` 是否可写，避免 full run 中途才发现路径问题。

输出写入：

```text
results/runs/<run_id>/doctor.json
results/runs/<run_id>/doctor.log
```

### prepare-data

实现方式：

1. 通过 Hugging Face datasets 或等价官方加载方式读取
   `princeton-nlp/SWE-bench_Verified` 的 `test` split；
2. 只把 agent 运行需要的安全字段写入内部 case manifest；
3. gold `patch`、`test_patch`、`FAIL_TO_PASS`、`PASS_TO_PASS` 不进入 agent 输入文件；
4. 按 `instance_id` 排序输出 500 case；
5. 计算 case list hash；
6. 把 dataset revision、loader version、`hints_text` 使用口径写入 `run_config.json`。

输出：

```text
benchmark/swebench/data/cases.jsonl
benchmark/swebench/data/cases.sha256
results/runs/<run_id>/run_config.json
```

case list hash 采用规范化算法：

```text
sha256(join(sort(instance_id), "\n") + "\n")
```

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

- wrapper 不解释测试语义，只负责启动 official local harness；
- baseline/native 分别生成 harness report；
- 保存原始 report、logs、stdout/stderr 和命令参数；
- harness 并发与 agent 生成并发分开记录；
- 失败时保留完整命令、exit code 和日志路径，供 import 判断 `infra_error` 或 `incomplete`。

实现细节：

1. 根据 `--target baseline|native` 选择 predictions 文件；
2. 为 harness 生成独立 `run_id`，避免 baseline/native report 混写；
3. 设置 `DOCKER_HOST=tcp://localhost:2375`；
4. 执行 `python -m swebench.harness.run_evaluation`；
5. 将 harness 输出目录复制或索引到 `local-harness-report/<target>/`；
6. 写入 `verifier_manifest.json`，记录 swebench version、Docker version、`--max_workers`、命令行和时间。

### import

把 runner 输出和 harness 输出导入统一 schema：

- 合并 predictions、patches、traces、usage、duration、verifier result；
- 计算 changed files 和 patch 行数；
- 按统一判定优先级生成主状态；
- 输出 `cases.jsonl` 和 `comparison.json`。

实现步骤：

1. 读取 `data/cases.jsonl` 作为分母；
2. 读取 baseline/native predictions，按 `instance_id` 建索引；
3. 读取 patches/traces/usage/duration；
4. 解析 harness report，得到每个 prediction 的 resolved/unresolved/error 原始结果；
5. 对 baseline/native 分别执行主状态判定；
6. 计算 patch stats；
7. 输出规范化 `cases.jsonl`；
8. 从 `cases.jsonl` 聚合生成 `comparison.json` 和 `comparison.md`。

importer 必须把“缺记录”当作错误处理，不能静默跳过 case。500 case 分母必须完整。

### report

从结构化结果生成报告：

- 英文：`results/REPORT.md`；
- 中文：`results/REPORT.zh_CN.md`；
- 同一份 `comparison.json` 作为数据源；
- 报告包含总体 resolved rate、五类主状态、per-repo 结果、失败分类、资源使用和复现路径。

实现方式：

- report generator 只读取 `comparison.json`、`cases.jsonl` 和 artifact index；
- 中英文报告共用同一份结构化数据；
- 报告正文不内嵌完整 patch，只链接 patch artifact；
- smoke/subset/internal failed run 不写入最终对外报告，只作为内部记录。

## 3. 公共模块实现

### 3.1 config

负责：

- 合并 CLI flags、环境变量和默认值；
- 生成 `run_config.json`；
- 校验 baseline/native 是否使用同一 dataset、模型策略和 verifier；
- 在 full run 前阻止缺失关键字段的配置进入运行。

### 3.2 archive

负责：

- 创建 `results/runs/<run_id>`；
- 为每个 case 生成标准 artifact 路径；
- 写入 run manifest；
- 支持 resume：已完成 case 不重复运行，除非显式 `--rerun`；
- 执行 secret scrub。

### 3.3 patch

负责：

- 读取 patch 文件；
- 计算 changed files、added/deleted lines；
- 检查 patch 是否为空；
- 检查是否包含明显不应提交的构建产物、缓存、临时测试；
- 只做结构检查，不替代 official harness。

## 4. 数据 schema

### 4.1 run_config.json

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

### 4.2 cases.jsonl

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

## 5. 状态判定

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

## 6. 防作弊与输入隔离

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

## 7. 并发与资源

并发分两类：

- agent 生成并发：模型/API 密集型；
- harness 验证并发：Docker/磁盘密集型，对应 `--max_workers`。

默认档位：

| 规模 | Agent 生成并发 | Harness 验证并发 |
| --- | ---: | ---: |
| <= 10 cases | 15，实际活跃任务不超过 case 数 | <= case count |
| 11-100 cases | 15 | <= 5 |
| 101-499 cases | 15 | <= 15 |
| 500 full batch | 15 | 从 15 开始，按阶段零校准调整 |

进入 full run 前必须完成：

- Docker 访问方式确认；
- Docker Root Dir 容量确认；
- gold patch 或 known case harness smoke；
- 模型 endpoint smoke；
- resume/retry 策略验证。

## 8. 复查材料

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
