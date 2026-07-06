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

## 3. 数据 schema

### 3.1 run_config.json

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

### 3.2 cases.jsonl

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

## 4. 状态判定

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

## 5. 防作弊与输入隔离

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

## 6. 并发与资源

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

## 7. 复查材料

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
