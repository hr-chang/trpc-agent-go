# SWE-Bench Verified 评测需求文档

## 1. 背景与目标

本需求用于证明 `tRPC-Agent-Go` 能够支撑真实软件工程 Agent 场景：基于
`tRPC-Agent-Go` 构建 Go-native SWE Agent，并在 SWE-Bench Verified 全量 500 case 上
完成可复现、可审计的评测。

本需求最终服务于一次性对外宣发。整个工作只在能力证明完成、结果可核验后统一宣发；不需要设计
"先宣发、再校正" 或分阶段对外披露策略。当前阶段只关注评测正确性、完整性和证据链。

对外希望支撑的核心表述是：

> tRPC-Agent-Go 可以构建 Go-native SWE Agent，并在 SWE-Bench Verified 全量 500
> case 上完成真实评测；其结果可与同等条件下的基线 Agent 对比。

本需求不是单纯建设通用 benchmark 流水线，也不是只导入外部 baseline。评测流水线是工程底座，
真正的交付主角是基于 `tRPC-Agent-Go` 的 Go-native SWE Agent 及其全量评测结果。

## 2. 术语与范围定义

- SWE-Bench Verified：`princeton-nlp/SWE-bench_Verified`，SWE-Bench test split 中
  经过人工验证的 500 case 子集。
- case / instance：SWE-Bench Verified 中的一条任务，包含 `instance_id`、`repo`、
  `base_commit`、`problem_statement`、测试信息等字段。
- prediction：提交给官方 harness 的预测记录，至少包含 `instance_id`、
  `model_name_or_path`、`model_patch`。
- patch：Agent 生成的 unified diff；官方 harness 会将其应用到对应 base commit。
- resolved：官方 local harness 判定 patch 解决该 case。
- unresolved：patch 可验证完成，但未通过官方判定。
- empty patch：Agent 未产出有效 patch，仍计入 500 case 分母。
- error：Agent run、patch 应用、验证流程或基础设施出现错误。
- incomplete：case 未完成运行或未进入最终可判定状态。
- baseline：用于对比的既有 Agent 实现；本需求第一版使用 mini-SWE-agent。
- native agent：基于 `tRPC-Agent-Go` 实现的 Go-native SWE Agent。
- local harness：SWE-Bench 官方本地 Docker 验证流程，即
  `swebench.harness.run_evaluation`。
- sb-cli：SWE-Bench hosted evaluation CLI。当前已知不可用，不作为主 verifier 或阻塞项。

## 3. 需求约束边界

本需求约束的是能力证明、评测口径和证据链，不预先锁死具体工程实现路径。

硬约束：

- 必须基于 SWE-Bench Verified `test` split 的完整 500 case 给出最终结论；
- 必须使用官方 local harness 作为主 verifier；
- baseline 和 native agent 必须共享同一 dataset snapshot、模型策略、预算口径和验证流程；
- agent 运行输入不得包含官方答案或验证测试信息，包括 gold `patch`、`test_patch`、
  `FAIL_TO_PASS`、`PASS_TO_PASS`；official harness 判定测试仅由 verifier 持有；
- 每个 case 必须有明确状态，并保留足以复算最终指标的证据链；
- 最终报告必须中英文双语。

非硬约束：

- 不限定最终模型必须是 GLM-5；当前默认单模型，若后续改为多模型需重新确认；
- 不限定 native agent 必须采用 bash-only、固定 Go CLI 或固定 runner 形态；
- 不限定具体 workspace、sandbox、patch extraction、trace schema 的内部实现，只要求最终 artifact
  满足报告和复核需要；
- 不要求普通 PR CI 跑完整 500 case。

## 4. 业务价值与对外叙事

本项目的业务价值是为 `tRPC-Agent-Go` 提供一个行业认可、开发者容易理解、可复现核验的能力证明。
最终报告需要说明：

- `tRPC-Agent-Go` 可以承载 SWE-style coding agent 的完整运行链路；
- Go-native Agent 能读取真实 issue、操作真实仓库、生成官方可验证 patch；
- 全量 500 case 的结果、失败类型、耗时和资源使用可追踪；
- 与同一数据集、同一 verifier、同一模型策略下的 baseline 相比，差距或优势是什么。

第一版不设置 resolved rate 下限，也不承诺必须超过 baseline。验收重点是评测本身是否正确、完整、
可核验。

## 5. 最终交付物

1. Go-native SWE Agent：
   - 位置：`benchmark/swebench/trpc-agent-go-impl`；
   - 作为独立 Go module 实现；
   - 基于 `tRPC-Agent-Go` 实现 issue 读取、case workspace 操作、patch 生成和 trace 记录；
   - 具体执行形态可采用 bash/workspace、结构化工具、runner 编排或其他等价方案。

2. 全量 500 case 评测报告：
   - 英文报告：`benchmark/swebench/results/REPORT.md`；
   - 中文报告：`benchmark/swebench/results/REPORT.zh_CN.md`；
   - 报告必须基于 SWE-Bench Verified test split 的完整 500 case；
   - 报告正文索引证据链，不内嵌完整 patch。

3. 可复现文档：
   - 位置：`benchmark/swebench/README.md`；
   - 说明环境、数据集、模型配置、baseline 运行、native run、local harness 验证、报告生成。

4. 结构化归档：
   - run config、case-level 结果、predictions、patches、traces、官方 local harness report、
     comparison files。

## 6. 仓库与分支落点

主仓库 `trpc-agent-go` 只承载需求/设计文档和最终 submodule 指针更新。实际代码、报告和数据落在
`benchmark` submodule，即 `trpc-agent-go-benchmark` 仓库。

主仓库当前需求分支：

```text
bench/swe-verified
```

benchmark 仓库远端约定：

```text
origin:   https://github.com/hr-chang/trpc-agent-go-benchmark.git
upstream: https://github.com/trpc-group/trpc-agent-go-benchmark.git
```

benchmark 仓库需求分支：

```text
bench/swe-verified
```

最终合入路径：

1. 在个人 benchmark fork 的 `bench/swe-verified` 分支开发 `benchmark/swebench`；
2. 从个人 fork 向官方 `trpc-group/trpc-agent-go-benchmark` 提交 PR；
3. benchmark PR 合入官方仓库后，主仓库更新 `benchmark` submodule 指针；
4. 主仓库再完成最终 PR。

## 7. 数据集与验证标准

数据集：

- dataset：`princeton-nlp/SWE-bench_Verified`；
- split：`test`；
- denominator：500；
- 需要固化 dataset revision 或等价快照信息；
- 需要输出 500 case list 和 case list hash。
- `hints_text` 口径跟随 baseline 复现链路：如果复现原版 mini-SWE-agent 时实际使用
  `hints_text`，native agent 也按同一口径使用；如果 baseline 未使用，则整个评测都不使用
  `hints_text`。该口径必须写入 `run_config.json` 和报告。

主 verifier：

```bash
python -m swebench.harness.run_evaluation \
  --dataset_name princeton-nlp/SWE-bench_Verified \
  --predictions_path <predictions.jsonl> \
  --max_workers <num_workers> \
  --run_id <run_id>
```

验证规则：

- 官方 local harness 是唯一主验证标准；
- `sb-cli` 当前不可用，不进入主流程，不作为验收条件；
- 若未来 `sb-cli` 恢复，可作为补充交叉验证，但不能覆盖 local harness 结论；
- 没有有效 patch 的 case 也计入 500 case 分母。

官方 prediction record 至少包含：

```json
{
  "instance_id": "repo_owner__repo_name-issue_number",
  "model_name_or_path": "model-name",
  "model_patch": "unified diff patch"
}
```

## 8. 模型与对比基线

默认采用单模型评测。具体模型不在需求层面绑定 GLM-5，最终按模型可用性、成本、稳定性、endpoint
能力、工程接入难度和评测公平性综合决定。

需求层面只规定公平性原则：

- baseline 和 native agent 必须使用同一套已确认模型策略；
- 默认情况下，两组都使用同一模型和同一 endpoint；
- 如果后续因工程原因调整为多模型策略，需要重新确认，并确保两组使用同一模型集合、路由规则和预算限制；
- 模型名称、版本、endpoint、参数、usage 字段必须写入 `run_config.json`；
- 公开 leaderboard 结果只作为背景引用，不替代本项目重跑结果。

baseline 使用 mini-SWE-agent。选择它的原因是结构简单、有 SWE-Bench batch runner，且有官方结果可作
背景对比锚点。第一版不增加其他 baseline，避免扩大对比矩阵和解释成本。

baseline 目标是复现原版 mini-SWE-agent 链路，不主动改造其 agent 行为。若因本地环境、模型接入或
official harness 输入格式需要 wrapper、配置注入或 predictions 格式转换，只允许做非语义适配，并在
`run_config.json` 中记录 mini-SWE-agent commit、配置、模型参数、转换步骤和 runner commit。

native agent 不要求复刻 mini-SWE-agent。第一版可以参考 mini-SWE-agent 的最小可比行为来降低解释
成本，但允许使用 `tRPC-Agent-Go` 更自然的 runner、tool、workspace、sandbox、trace 设计，只要最终
输出满足同一 prediction 和 verifier contract。

## 9. 系统方案

标准证据链：

```text
SWE-Bench Verified dataset
  -> runner
  -> baseline/native agent
  -> per-case workspace
  -> patch
  -> predictions.jsonl
  -> official local harness
  -> case-level result
  -> report and comparison
```

上图描述的是需要被保留和审计的数据流，不限定实现时必须是单一 Go CLI、单一进程或单一 runner。
工程实现可以拆分为多个命令、脚本、服务或离线导入步骤，只要最终归档满足统一 schema，并能复算报告指标。

baseline 和 native 两条链路共享：

- 同一 dataset snapshot；
- 同一 case list；
- 同一模型策略；
- 同一资源预算口径；
- 同一 local harness；
- 同一报告生成逻辑。

## 10. 功能需求

### 10.1 数据加载

- 读取 SWE-Bench Verified `test` split；
- 固化 case list 和 hash；
- 支持按 instance id、slice、filter 运行 smoke subset；
- 全量报告必须覆盖 500 cases。

### 10.2 baseline 运行

- 支持原版 mini-SWE-agent batch run；
- 保留 trajectory、prediction、usage、duration；
- 支持导入 baseline 原始输出并转换为统一归档格式；
- baseline 适配不得改变 agent 可见输入、行动空间或 patch 语义。

### 10.3 native agent 运行

- 基于 `tRPC-Agent-Go` 实现 Go-native SWE Agent；
- 读取 problem statement；
- 准备或接入 per-case workspace；
- 具备探索仓库、修改代码、必要时自检、生成 patch 的能力；
- bash/workspace 工具是默认参考形态，不是唯一实现形态；
- 生成 unified diff patch；
- 输出与官方 harness 兼容的 predictions。

### 10.4 workspace 与执行记录

- 每个 case 需要隔离的 workspace 或等价执行环境；
- 执行过程必须有可审计 trace；
- 若使用命令执行型工具，命令应默认非交互，并记录 command、exit code、stdout/stderr 摘要和 timeout；
- 若使用非命令型工具，需要记录等价的 action、input、output、error 和耗时；
- 支持失败后标记 error/incomplete。

### 10.5 patch 与 prediction

- patch 必须是相对 case `base_commit` 生成、官方可应用的 unified diff；
- patch 应排除构建产物、缓存、无关未跟踪文件和仅用于自检的临时测试文件；
- empty patch 需要显式记录；
- predictions 文件必须覆盖所有进入评测分母的 case；
- patch 文件单独归档，不内嵌到报告正文。

### 10.6 verifier 调用与结果导入

- 调用官方 local harness；
- 保存原始 harness 输出和 per-case logs；
- 将官方结果导入统一 case-level schema；
- 每个 case 最终只能有一个主状态；
- 工程原因导致的 `infra_error` / `incomplete` 需要修复后重跑，不能作为最终对外报告的未处置状态；
- patch apply failed 默认归入 agent 产物无效类 unresolved；只有能证明是 harness、镜像、数据或环境问题时，
  才归入 infra/data baseline issue 并修复后重跑。

### 10.7 报告生成

- 生成英文和中文报告；
- 生成 comparison JSON/Markdown；
- 支持从归档结果重新计算 aggregate metrics。

## 11. 非功能需求

- 可复现：报告中的关键指标必须能由归档材料重新计算。
- 可恢复：长时间 batch run 中断后，应能识别已完成 case 并继续或重跑指定 case。
- 可审计：每个 case 都有 patch、trace、usage、duration、verifier reference。
- 可解释：失败分类和归档材料要支撑人工复查，而不是只给一个失败总数。
- 可控成本：每个 run 必须记录 token、API calls、duration、retry/error 次数。
- 可控并发：正式 batch 默认并发 15，整体并发上限 20；如需调整，需要记录原因。

## 12. 运行与归档要求

每个 admitted run 至少包含：

```text
run_config.json
cases.jsonl
predictions.jsonl
patches/
traces/
local-harness-report/
comparison.json
comparison.md
```

`run_config.json` 至少包含：

- run id；
- runner commit；
- benchmark repo commit；
- main repo commit；
- dataset id 和 revision；
- case list hash；
- `hints_text` 使用口径；
- mini-SWE-agent commit 和配置；
- SWE-Bench package / harness version；
- per-repo Docker image tag 和 digest，或可追溯到 digest 的镜像清单；
- model strategy；
- endpoint identifier；
- model parameters；
- verifier 类型和版本；
- concurrency；
- step/token/time limits；
- start/end time，使用 UTC 或显式时区。

`cases.jsonl` 至少包含：

- `instance_id`；
- baseline/native status；
- baseline/native usage；
- baseline/native duration；
- patch path；
- trace path；
- verifier result reference；
- changed files；
- patch line stats，例如 `+N/-M`；
- error 或 incomplete reason。

默认保留完整证据链，但不默认归档所有 case 的完整 workspace。考虑到本需求很可能需要多轮排查，异常
case、分歧 case、infra/data baseline issue、以及人工指定复查 case 可以显式保存 workspace 或更完整
的现场材料。报告中的 case-level 明细需要能索引到这些复查材料。

归档前必须 scrub secrets，避免模型 endpoint、API key、HTTP headers、临时凭证进入 traces、logs 或
对外 artifact。

## 13. 报告要求

报告必须中英文双语，参考 benchmark 仓库已有文档组织方式：

```text
benchmark/swebench/results/REPORT.md
benchmark/swebench/results/REPORT.zh_CN.md
```

报告至少包含：

- 总体 resolved rate；
- baseline/native 对比；
- per-repo 结果；
- failure taxonomy；
- empty patch、error、incomplete 统计；
- token、API calls、duration 统计；
- 并发配置；
- 模型策略和 endpoint 标识；
- case-level 明细索引；
- artifact 路径说明；
- 复现命令。

对外报告只给出最终 500 case 指标和必要的复现路径。smoke、calibration、失败排查过程可以作为内部
工程记录保存，但不作为对外主结论。

## 14. 成本与并发策略

成本不在需求层面绑定某个公开 API 价格。报告按实际模型策略记录资源使用：

- prompt tokens；
- completion tokens；
- total tokens；
- API calls；
- per-case duration；
- retry/error counts；
- concurrency settings；
- endpoint identifier；
- 如果 endpoint 提供 cost 字段，则原样归档。

默认规则：

- 每个 instance 都必须配置 step/action、token、time limits 或等价预算限制；
- 10 个以内 case 可默认串行；
- 当前整体并发上限为 20；
- 正式 batch 并发按 15 执行，预留 5 给 smoke/demo 或异常复查。
- 若出现明显资源争用、harness timeout 或模型服务稳定性问题，工程侧可以下调并发，并在
  `run_config.json` 和报告中记录原因。

## 15. 验收标准

最低验收：

- `benchmark/swebench` 目录结构落地；
- native agent 能完成 smoke case；
- baseline/native 都能输出官方 harness 兼容 predictions；
- local harness 能对 smoke predictions 完成验证；
- 报告生成链路能从 case-level 数据计算指标。

完整验收：

- baseline 跑完 SWE-Bench Verified 500 cases；
- native agent 跑完 SWE-Bench Verified 500 cases；
- 两组结果使用同一 dataset snapshot、同一模型策略、同一 official local harness；
- 每个 case 都有明确状态；
- 最终对外报告不得包含未处置的 `infra_error` / `incomplete`；工程原因导致的问题必须修复后重跑，
  或该 run 仅作为内部失败运行记录，不进入最终结论；
- 中英文报告、结构化结果和 artifact index 完整；
- aggregate metrics 可从归档材料重新计算。

## 16. 阶段计划

### 阶段零：环境与 verifier 校准

- 修复或启动 Docker daemon；
- 安装 Go、Python 依赖和 official SWE-Bench local harness；
- 固化 workspace/cache 路径；
- 使用 gold patch 或 known case 验证 local harness 能正确判定。

退出条件：local harness 在专用容器内可稳定运行，并能对已知输入给出可信结果。

### 阶段一：baseline 校准

- 固化 dataset revision、case list 和 hash；
- 跑通 mini-SWE-agent baseline；
- 使用 official local harness 验证；
- 导入 predictions、trajectories、harness logs、usage、duration；
- 产出 baseline-only summary。

退出条件：baseline 链路能在 smoke subset 上稳定复现，并且输出格式能被报告生成器消费。

### 阶段二：Go-native SWE Agent

- 实现 Go-native SWE Agent 的运行入口和归档链路；
- 默认可从 bash/workspace 形态起步，但不限制最终 runner/tool 设计；
- 对齐官方 prediction 和 patch submission contract；
- 跑通 smoke subset；
- 完成 trace、usage、patch、failure-status 记录。

退出条件：native agent 能稳定产出 official harness 可验证 predictions。

### 阶段三：全量评测

- baseline 跑完 500 cases；
- native agent 跑完 500 cases；
- 使用同一 official local harness 验证；
- 生成完整 case-level results 和 comparison artifacts。

退出条件：500 case 分母完整，所有 case 状态明确，aggregate metrics 可复算。

### 阶段四：交付整理

- 完成 `README.md`、`REPORT.md`、`REPORT.zh_CN.md`；
- 整理结构化结果和 artifact index；
- 准备 benchmark 仓库 PR；
- benchmark upstream 合入后，主仓库更新 submodule 指针。

退出条件：需求三件套和证据链完整，可进入一次性对外宣发准备。

## 17. 已确认决策

以下是当前已确认的大决策。实现细节默认由工程侧基于这些规则推断，仅在影响方向、成本或对外解释时再
向需求方确认。

1. 模型策略边界：
   - 默认单模型测试；
   - 具体模型落地阶段再定，不在需求层绑定 GLM-5；
   - baseline/native 必须使用同一模型、endpoint、参数和预算口径。

2. baseline 范围：
   - baseline 使用 mini-SWE-agent；
   - 原因是 mini-SWE-agent 有官方结果可作背景对比，且对比解释成本较低；
   - 第一版不增加其他 baseline。

3. 资源预算优先级：
   - 优先追求 resolved rate；
   - 成本、耗时和稳定性需要记录并可控，但不以牺牲基本能力上限为第一目标。

4. 全量运行并发：
   - 整体并发上限为 20；
   - 正式 baseline/native batch 默认并发 15；
   - 预留 5 给 smoke/demo 或异常复查。

5. 证据保留力度：
   - 有复查要求；
   - 默认保留完整证据链；
   - 不默认保存所有完整 workspace，但异常、分歧、infra/data baseline issue 和人工指定复查 case
     需要可保存更完整现场。

6. 对外报告口径：
   - 对外报告只给出最终 500 case 指标；
   - 同时提供一条可复现路径；
   - 该路径存在即可，不要求缩短或简化路径。

7. `hints_text` 口径：
   - 跟随 baseline 复现链路；
   - 如果原版 mini-SWE-agent 复现中使用，则 native agent 也使用；
   - 如果 baseline 未使用，则整个评测都不使用。

## 18. 当前环境核查记录

专用容器访问方式：

```bash
ssh root@21.214.124.53.devcloud.woa.com -p 36000
```

初步核查结果：

- OS：Linux x86_64，kernel `5.4.241-1-tlinux4-0023.4`；
- user：`root`；
- Python：`Python 3.11.6`；
- Git：`git version 2.41.0`；
- Docker CLI：`Docker version 29.3.1`；
- Go：未安装；
- `/root` 挂载约 700GB，可用约 699GB；
- `/codev` 挂载约 2TB，可用约 975GB；
- Docker daemon 当前不可用，`/var/run/docker.sock` 不存在。

后续环境动作：

- 安装 Go；
- 修复或启动 Docker daemon；
- 将 SWE-Bench workspace/cache 放到大盘路径；
- 安装并验证 official SWE-Bench local harness；
- 用 gold patch 或单个 known case 验证 harness。

## 19. 风险与开放问题

- Docker daemon 当前不可用，必须在进入 full run 前修复。
- SWE-Bench local harness 对磁盘、Docker、镜像缓存要求高，需要明确 cache 路径。
- 具体模型未定，可能影响 baseline/native 的公平性解释，需要在 run config 和报告中明确记录。
- mini-SWE-agent 输出可能需要格式转换后才能稳定喂给 local harness。
- 旧实验中出现过 internal GLM-5 结果与公开结果差异显著的情况，后续必须优先核查模型版本、
  provider 参数、dataset/image/harness 版本和 case 分母口径。
- Verified dataset 或 SWE-Bench harness 版本漂移会影响复现，需要 pin revision。
- full run 时间长，必须支持断点恢复或明确重跑策略。

## 20. 参考资料

- SWE-Bench Verified dataset：https://huggingface.co/datasets/princeton-nlp/SWE-bench_Verified
- SWE-Bench leaderboard：https://www.swebench.com/index.html
- SWE-Bench evaluation guide：https://www.swebench.com/SWE-bench/guides/evaluation/
- SWE-Bench harness reference：https://www.swebench.com/SWE-bench/reference/harness/
- mini-SWE-agent：https://github.com/SWE-agent/mini-swe-agent
- mini-SWE-agent SWE-Bench docs：https://mini-swe-agent.com/latest/usage/swebench/
- benchmark upstream：https://github.com/trpc-group/trpc-agent-go-benchmark
