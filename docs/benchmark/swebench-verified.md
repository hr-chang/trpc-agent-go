# SWE-Bench Verified Capability Proof

## Purpose

This branch introduces the planning and integration track for proving that
`tRPC-Agent-Go` can build a Go-native software engineering agent and evaluate it
on the full SWE-Bench Verified benchmark.

The external message to support is:

> tRPC-Agent-Go can build a Go-native SWE Agent and complete a reproducible full
> 500-case SWE-Bench Verified evaluation, with results comparable to a
> mini-SWE-agent + GLM-5 baseline.

This is not primarily a generic benchmark pipeline project. The main deliverable
is the Go-native SWE Agent built on `tRPC-Agent-Go`, plus a complete and
auditable evaluation result.

## Researched Context

### SWE-Bench Verified

- Dataset: `princeton-nlp/SWE-bench_Verified`.
- Split: `test`.
- Size: 500 rows.
- Scope: a human-validated subset of SWE-Bench test instances from popular
  Python repositories.
- Task shape: given `problem_statement` and `base_commit`, an agent edits a
  checked-out repository and returns a patch.
- Important dataset fields include `repo`, `instance_id`, `base_commit`,
  `patch`, `test_patch`, `problem_statement`, `hints_text`, `version`,
  `environment_setup_commit`, `FAIL_TO_PASS`, and `PASS_TO_PASS`.
- The official leaderboard metric is `% Resolved`, i.e. solved instances divided
  by benchmark instances. For Verified, the denominator is 500.

Sources:

- https://huggingface.co/datasets/princeton-nlp/SWE-bench_Verified
- https://www.swebench.com/index.html
- https://openai.com/index/introducing-swe-bench-verified/

### Official Verification

The primary verifier must be the official local SWE-Bench harness:

```bash
python -m swebench.harness.run_evaluation \
  --dataset_name princeton-nlp/SWE-bench_Verified \
  --predictions_path <predictions.jsonl> \
  --max_workers <num_workers> \
  --run_id <run_id>
```

The harness applies generated patches inside Docker-based task environments,
runs the benchmark tests, and writes aggregate results, per-instance results, and
logs. Prediction records must include:

```json
{
  "instance_id": "repo_owner__repo_name-issue_number",
  "model_name_or_path": "model-name",
  "model_patch": "unified diff patch"
}
```

Resource assumptions from the official docs:

- Docker-based evaluation.
- x86_64 is recommended; arm64 support is experimental.
- At least 120 GB free storage, 16 GB RAM, and 8 CPU cores are recommended.
- Worker count should be chosen conservatively, below
  `min(0.75 * os.cpu_count(), 24)`.

Sources:

- https://www.swebench.com/SWE-bench/guides/evaluation/
- https://www.swebench.com/SWE-bench/reference/harness/

### mini-SWE-agent Baseline

mini-SWE-agent is a strong baseline because it intentionally uses a small
bash-only scaffold:

- no custom tools other than bash;
- linear message history;
- independent command execution via subprocess-like calls;
- SWE-Bench batch support with `--subset verified --split test`;
- predictions output that can be fed into official SWE-Bench evaluation.

Representative baseline commands:

```bash
mini-extra swebench \
  --model <glm5-litellm-or-openai-compatible-model-name> \
  --subset verified \
  --split test \
  --workers <num_workers> \
  --output <baseline-run-dir>

python -m swebench.harness.run_evaluation \
  --dataset_name princeton-nlp/SWE-bench_Verified \
  --predictions_path <baseline-run-dir>/preds.jsonl \
  --max_workers <num_workers> \
  --run_id <baseline-run-id>
```

Sources:

- https://github.com/SWE-agent/mini-swe-agent
- https://mini-swe-agent.com/latest/
- https://mini-swe-agent.com/latest/usage/swebench/

### Current Repository Placement

The main repository has a `benchmark` submodule configured as:

```text
path: benchmark
url:  https://github.com/trpc-group/trpc-agent-go-benchmark
branch: main
```

The benchmark repository currently organizes benchmark suites as top-level
directories such as `gaia`, `knowledge`, `memory`, `summary`, and `toolsearch`.
The SWE-Bench work should therefore land under:

```text
benchmark/swebench/
  README.md
  data/
  trpc-agent-go-impl/
  results/
```

Development should use the personal fork
`https://github.com/hr-chang/trpc-agent-go-benchmark.git` for the SWE-Bench
implementation branch. The final merge path should be:

1. develop `benchmark/swebench` in the benchmark fork;
2. PR from the fork into `trpc-group/trpc-agent-go-benchmark`;
3. update this main repository's `benchmark` submodule pointer after the
   benchmark PR is merged upstream.

Source:

- https://github.com/trpc-group/trpc-agent-go-benchmark

## Final Deliverables

1. Go-native SWE Agent:
   - location: `benchmark/swebench/trpc-agent-go-impl`;
   - implemented as an independent Go module based on `tRPC-Agent-Go`;
   - reads SWE-Bench Verified instances, prepares per-case workspaces, drives a
     bash/workspace action loop, and emits official unified diff patches.

2. Full 500-case evaluation report:
   - location: `benchmark/swebench/results/REPORT.md`;
   - includes structured result files and artifact indexes;
   - reports the full SWE-Bench Verified test split denominator of 500.

3. Reproducibility documentation:
   - location: `benchmark/swebench/README.md`;
   - covers dataset version, model configuration, baseline run, native agent run,
     official local harness verification, optional `sb-cli` cross-check, and
     metric calculation.

## Evaluation Requirements

Both compared runs must use:

- the same SWE-Bench Verified dataset version;
- the same 500-case test split;
- the same default model, GLM-5;
- the same self-hosted OpenAI-compatible GLM-5 endpoint where possible;
- the same official local SWE-Bench harness;
- the same result denominator of 500, including empty patch, error, and
  incomplete cases.

The mini-SWE-agent + GLM-5 result must be rerun by this project. Public GLM-5
leaderboard results may be cited as background, but the report's core comparison
must come from our rerun baseline and native-agent runs.

`sb-cli` hosted evaluation is optional cross-validation only. It must not block
the main result path while hosted evaluation is unstable or differs from local
gold-patch behavior.

## Native Agent Scope

The first Go-native agent should intentionally align with mini-SWE-agent's
bash-only behavior before adding more complex tool surfaces:

- prompt/action loop;
- independent bash command execution;
- workspace checkout and command execution rooted in the case repository;
- non-interactive command discipline;
- per-instance step, token, and time limits;
- patch generation and validation before submission;
- prediction output compatible with official SWE-Bench harness;
- trace and usage capture sufficient for case-level audit.

## CLI Shape

The first version should provide a unified Go CLI in
`benchmark/swebench/trpc-agent-go-impl`:

```text
swebench run-mini
swebench run-native
swebench verify
swebench report
swebench import
```

Scripts may wrap environment setup, but orchestration logic should live in the
Go CLI.

## Artifact Contract

Each admitted run must archive enough evidence to recompute and audit the final
metrics:

```text
run_config.json
cases.jsonl
predictions.jsonl
patches/
traces/
local-harness-report/
sb-cli-report/        # optional
comparison.json
comparison.md
```

`cases.jsonl` should include, at minimum:

- `instance_id`;
- baseline and native statuses;
- baseline and native usage;
- baseline and native duration;
- patch path;
- trace path;
- verifier result reference;
- changed files;
- patch line stats such as `+N/-M`;
- error or incomplete reason where applicable.

Full workspaces are not archived by default. They may be retained temporarily for
debugging or explicitly saved for exceptional cases.

## Cost And Concurrency Rules

Cost is tracked as internal model-service resource usage rather than public API
USD cost unless the GLM-5 endpoint provides reliable cost fields.

Every run must record:

- prompt tokens;
- completion tokens;
- total tokens;
- API calls;
- per-case duration;
- retry and error counts;
- concurrency settings;
- model endpoint identifier.

Rules:

- default model is GLM-5;
- every instance has step, token, and time limits;
- runs of 10 or fewer cases may be serial by default;
- runs above 10 cases require confirmation of baseline/native concurrency;
- current total concurrency ceiling is 20;
- suggested formal batch concurrency is 15, leaving 5 slots for smoke/demo use;
- final report must be based on all 500 cases.

## Initial Milestones

### Phase 1: Baseline Calibration

- Pin dataset version and materialize the 500-case list plus hash.
- Run original mini-SWE-agent + GLM-5 on all 500 cases.
- Verify with official local harness.
- Import predictions, trajectories, verifier logs, usage, and duration.
- Produce baseline-only report slices to validate parsing and reporting fields.

### Phase 2: Go-native SWE Agent

- Implement the Go CLI and native bash-only SWE Agent.
- Match mini-SWE-agent's core execution contract and prediction output.
- Run smoke cases and then a small calibration subset.
- Confirm trace, usage, patch, and failure-status recording.

### Phase 3: Full Native Evaluation

- Run the native agent on all 500 cases with GLM-5.
- Verify with the same official local harness.
- Generate full case-level results, failure taxonomy, and comparison artifacts.

### Phase 4: Delivery Cleanup

- Finalize `README.md`, `REPORT.md`, `REPORT.zh_CN.md`, structured files, and
  artifact indexes.
- Ensure every aggregate metric can be recomputed from archived case-level data.
- Prepare benchmark repository PR, then update this repository's submodule
  pointer after benchmark upstream merge.

## Open Items To Resolve

- Pin the exact GLM-5 model identifier, endpoint label, and model version string
  for reports.
- Capture a dated snapshot of the SWE-Bench leaderboard row used as the
  background GLM-5 reference.
- Decide the exact benchmark fork branch name; default proposal:
  `bench/swe-verified`.
- Decide whether native execution uses official SWE-Bench Docker images directly
  or a workspace-preparation adapter that mirrors their checkout semantics.
- Define the failure taxonomy precisely enough to map official harness failures,
  empty patches, agent exceptions, timeouts, and incomplete runs without double
  counting.
- Confirm whether `preds.json` from mini-SWE-agent must be converted to JSONL
  before local harness execution in our final scripts.

