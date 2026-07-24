# Contextual Embedding 有效性验证需求

状态：I0 已关闭；I1（GLM Context）已完成且为有效负结果；I2（DeepSeek Agentic A/B）实现已完成，待远端探针与实验

日期：2026-07-20

最近更新：2026-07-25

## 1. 需求目标

本需求只验证以下方法假设：

```text
在相同语料、chunk、Embedding、检索模式、Agent 和 Judge 下，
为每个 chunk 增加由完整父文档生成的短 Context 后再进行 embedding，
能否提高 MultiHop-RAG 上的 Agentic RAG 最终回答质量。
```

本轮正式结论只比较同批次 A/B，不与 README 的历史聚合数值作严格横向比较，也不预设该
方法最终会纳入 tRPC-Agent-Go（下文简称 TAG）。只有 I2 达到本文门槛后，才讨论框架化和
非回归验证。

## 2. 方法与对照组

### 2.1 A/B 唯一处理差异

```text
A / Baseline:
BaseEmbeddingText → BGE-M3 → A index

B / Contextual:
DeepSeek Context + delimiter + BaseEmbeddingText → BGE-M3 → B index
```

`BaseEmbeddingText` 是相同实验加载路径为 chunk 生成的原始 embedding 输入。B 不得覆盖或
丢弃它。

### 2.2 Context 约束

Context 由 DeepSeek-V3.2 根据“完整父文档 + 当前 chunk”离线生成，并满足：

- 使用现有 `anthropic-contextual-retrieval-v1` prompt，不根据 I1/I2 结果调参；
- query-independent，不读取 benchmark question 或 gold evidence；
- `temperature=0`，不传 `reasoning`；
- 只补充 chunk 的主题、实体、时间、章节或指代语境；
- 不回答问题，不添加父文档外信息，不重写整个 chunk；
- 空输出、截断、父文档不完整或生成失败均为可见失败；
- 全量生成后冻结并复用同一 cache，不在检索或回答阶段重新生成。

Context 只进入 `Document.EmbeddingText`。A/B 的 `Document.Content`、metadata、chunk ID、顺序
和 Agent 可见知识文本必须完全相同。

## 3. 已冻结的正式实验配置

```text
primary dataset:       MultiHop-RAG
parents:               609
chunks:                13,086
questions:             450
question types:        comparison / inference / temporal，各 150
contextualizer:        DeepSeek-V3.2（仅 B 的离线索引构建）
answer model:          DeepSeek-V3.2（A/B 相同）
judge model:           GLM-5.2（provider model ID: glm52，A/B 相同）
embedding model:       BGE-M3，1024 dimensions（A/B 相同）
vector store:          PGVector（A/B 独立新表）
effective search mode: pure Vector（A/B 相同）
retrieval k per call:  4
agent repeats:         3 × 450 / arm
judge max tokens:      65,536
judge reasoning:       omitted
Judge client retry:    0
RAGAS request attempt: 1 attempt total
Judge runtime:         Python 3.11.6 / RAGAS 0.2.15 /
                       langchain-openai 0.2.14 / openai 1.109.1 /
                       datasets 2.21.0
```

I2 共运行 `3 × 450 × 2 = 2,700` 个 Agent case。这里的三次是完整实验重复，不是把同一个
Judge 调用三次，也不是要求 Agent 固定检索三次。

## 4. 代码边界

### 4.1 必须沿用的 legacy 行为

除实验必需差异外，A/B 必须沿用原 benchmark 的：

- Agent Prompt 原文；
- `temperature=0`；
- tool 名称、描述和输入协议；
- fresh session；
- Agent 自主生成和改写 search query 的行为；
- 原工具循环终止语义；
- 错误返回语义。

Prompt 中“最多搜索 3 次”仍是原始指令，不新增代码级三次限制。不得增加
`WithMaxToolIterations(500)` 或其他框架内硬限制。运行保护由实验 controller 的单请求
timeout 提供；超时记录为该 case 失败，不通过重新运行 Agent 隐藏。

### 4.2 Pure Vector 的隔离方式

README 声称历史实验使用纯 Vector，但 legacy `/answer` 的 Agent tool 实际没有应用
`--search-mode`。本轮已经选择纯 Vector，因此必须让它在 A/B 中真实生效，但不得改变 legacy
服务行为。

实现要求：

```text
index_variant=legacy:
    沿用原 Agent knowledge tool 路径

index_variant=baseline/contextual:
    仅实验服务使用配置的 search_mode
    正式运行要求 search_mode=1
```

controller 必须从 `/config` 验证 Agent 的有效检索模式，而不能只记录启动参数。

### 4.3 实验代码位置

I2 runner、controller、Judge 适配、统计和产物应放在
`benchmark/knowledge/contextual_retrieval/` 下。不得继续扩展 legacy `knowledge/main.py` 来承载
I2 语义。

独立的 Agent/Judge/Embedding URL、key 和 gateway header 路由可以保留；没有设置实验变量时，
legacy fallback 必须保持原行为。任何 I0/HF54 专用 calibration、resume、validation 或 runner
不得成为 I2 的启动门禁。

## 5. 数据、Context 与索引身份

### 5.1 Sealed 数据

沿用已经冻结的数据与 exact evidence mapping：

```text
parents manifest: 609 parents
chunks manifest:  13,086 chunks
cases manifest:   450 questions / 1,209 gold evidence records
mapping:          1,209 / 1,209 exact parent-span mappings
```

每个 chunk 至少保留：

```text
parent document ID
chunk ID
chunk index
parent content hash
chunk content hash
metadata
```

### 5.2 新实验身份

旧 GLM Context cache 和旧 B index 只能作为 I1 历史证据，不得用于本轮 DeepSeek A/B。

本轮必须生成：

- 新 DeepSeek Context cache；
- 新 A 表与新 B 表；
- 新 run directory；
- 包含模型、prompt、数据、代码和索引 digest 的 manifest。

Context 缺失、为空、hash 不匹配、未以正常 `stop` 结束或数量不是 13,086 时禁止
构建正式 B。必须按 chunk manifest 顺序将全部 `chunk_id + context_hash` 封装为
`context_set_digest`，并贯穿 Context summary、B 索引、runtime config 和 I2 lineage。A/B 表
相同、已有非空未知表或索引行数不等于 13,086 时禁止运行。

构建索引和运行 I2 时，根仓库与 benchmark 子仓库都必须为 clean checkout；索引
state、Agent controller 与 Judge manifest 必须绑定同一组精确 commit。

## 6. I1 与 I2

### 6.1 I1 Retrieval-only

```text
原始问题 → A/B 各直接检索一次 → Recall / MRR / NDCG
```

I1 不运行 Agent 或 Judge，只回答静态原始 query 的向量排名是否改善。

2026-07-22 的 GLM Context I1 是证据完整的负结果。它不代表 DeepSeek Context 的结果，也不
阻塞 I2。新索引完成后先跑 DeepSeek Context 的 30 题 I1 smoke，作为机制诊断：
只有 smoke 达到既有 promotion 门槛才扩大到完整 450 题 I1；如果为 `stop`，保留
smoke 证据即可。两种结果都不阻塞 I2。

### 6.2 I2 Agentic RAG

```text
问题
→ DeepSeek Agent
→ 一次或多次自主 search
→ 最终回答
→ 冻结回答
→ GLM Judge
```

I2 是本轮是否存在 Agentic RAG 方法价值的主要依据。A/B 初始问题相同、配置相同，但后续
query、实际搜索次数和轨迹允许不同；这些差异是索引结果影响 Agent 行为的下游效果。

## 7. 执行协议

### 7.1 Context compatibility probe

正式 cache 前固定选取 20 个结构差异较大的 chunks，只检查：

- API 和模型配置可用；
- 输出非空、简短且为检索语境；
- 没有回答问题、整段改写或明显文档外信息；
- 完整父文档未被静默截断。

Probe 不读取 benchmark 结果，不用于选择效果更好的 prompt。

### 7.2 Operational smoke

从三个问题类型各固定 10 题，共 30 题。Smoke 只验证：

- A/B 两个服务、索引和有效配置正确；
- Agent、Embedding、PGVector 与 GLM Judge 全链路可运行；
- 逐样本 checkpoint、trace、错误和 Judge 指标完整；
- timeout 与并发不会造成明显系统性失败。

Smoke 指标方向不作为扩量门禁。只要证据链完整且不存在基础设施故障，就进入正式 I2。

### 7.3 三次正式重复

- 每个 repeat 包含相同 450 题和两条 arm；
- 每题在同一 repeat 内配对；
- A/B 请求顺序按题平衡交错，下一 repeat 反转；
- case 顺序使用 manifest 中固定 seed；
- 两组使用相同并发和 timeout；
- 每个 Agent case 只采样一次；
- 不在看到首轮效果后决定是否补跑后两轮；
- 三轮全部结束后才生成正式结论。

## 8. 逐样本与聚合产物

每个 Agent case 至少保存：

```text
case ID / question type / repeat / arm / request order
question / ground truth
answer / contexts
每次 search query
每次返回的 chunk ID、parent ID、rank、score、metadata
每次和累计 gold evidence 命中
实际 tool call 数
完整可审计 trace
Agent status / error / elapsed time / token（可获得时）
```

Judge 产物至少保存七个现有 RAGAS 指标的逐样本值、运行配置、错误、token 和耗时：

```text
Faithfulness
Answer Relevancy
Answer Correctness
Answer Similarity
Context Precision
Context Recall
Context Entity Recall
```

还必须保存：

```text
Context cache 与 summary
A/B index state
运行 manifest
append-only Agent checkpoint 与终态 sealed answers
Judge checkpoint
paired aggregate
bootstrap CI
错误与失败率
准确命令和代码/data/model lineage
```

Controller 终态复用与 Judge 都必须验证 Agent checkpoint 实体的 SHA-256 和记录数，
并与 sealed answers、Agent report 和 controller report 相互绑定。Judge 还必须记录并
校验自身运行时的 root/benchmark clean commit，不得在 Judge 代码漂移后复用旧分数。

聚合结果不得替代逐样本产物。

## 9. 统计方法

每题先分别对 A、B 的三次 repeat 指标求均值，再计算该题 paired delta。不得把三次 repeat
当作 1,350 个独立问题。

正式 CI 使用：

```text
paired stratified bootstrap
strata: comparison / inference / temporal
resamples: 10,000
seed: 固定并写入 manifest
confidence interval: two-sided 95%
```

分题型指标用于解释异质性，不额外设置多个确认性显著性检验。

## 10. 失败处理

- A/B 使用固定 450 题分母；
- Agent 最终失败不得删题，七项指标统一按预注册的最坏值 `0` 进入 ITT，并单报失败率和
  success-only 诊断值；其中 Answer Correctness 是唯一确认性主指标，Context Precision 的
  guardrail 也使用该固定分母，避免因失败删题产生选择偏差；
- transport、timeout 和 HTTP attempt 全部保存；
- 正式 Agent case 不因失败自动重新采样；
- GLM Judge 每个冻结答案只评分一次；OpenAI client transport retry 为 `0`，RAGAS
  `RunConfig.max_retries=1`（在 RAGAS 0.2.15 中表示一次总尝试）；
- 不增加 whole-prompt 重试；RAGAS 自身既有 schema repair 可保留；
- Judge 失败只影响 Judge 阶段，不得重新运行 Agent；
- 正式主结果不使用 missing-cell resume 合并不同时间的 Judge 采样；
- 任一 repeat 的 Context、索引、样本或指标不完整时，该 repeat 为 `insufficient`；
- 不通过删除失败样本、缩小分母或事后改变 timeout 获得正向结论。

## 11. I2 有效性门槛

唯一确认性主指标为：

```text
mean(Answer Correctness_B - Answer Correctness_A)
```

方法有效必须同时满足：

1. Answer Correctness 绝对提升不少于 `0.02`；
2. 按第 9 节计算的 paired bootstrap 95% CI 下界大于 `0`；
3. Context Precision 点差不低于 `-0.01`；
4. B 的 Agent 最终失败率相对 A 不恶化超过 `1` 个百分点；
5. 三次 repeat、固定分母、Judge 指标和所有关键 lineage 完整。

Context Recall、Faithfulness、累计 gold evidence recall、实际搜索次数、延迟和 token 是支持或
机制指标，不作为额外硬门。

正式结论只有：

```text
method_effective:
    I2 满足全部确认性门槛；可以开始非回归和框架化价值评估。

method_not_effective:
    证据完整，但主指标或 guardrail 未通过；停止框架化。

insufficient:
    数据、索引、运行、Judge 或统计证据不完整；修复基础设施后重跑完整受影响 repeat。
```

使用 GLM-5.2 Judge 的本轮数值不得直接声明超过 README 的 Gemini-3-Flash 历史实现。

## 12. 非回归与框架化

只有 `method_effective` 后才运行 HuggingFace/RGB 非回归，并评估是否将实验能力沉淀为 TAG
框架 API。

本需求不包含：

- TAG 公共 Contextualizer API；
- 通用父文档/GroupedSource 抽象；
- 默认启用 Contextual Embedding；
- Contextual BM25、reranker 或 HyDE；
- query rewrite 方法变更；
- Late Chunking、GraphRAG 或知识图谱；
- 与历史 README 聚合分数的严格等价复现；
- 生产级缓存、增量同步和索引迁移。

## 13. 当前执行顺序

```text
P0  清理非 A/B legacy 行为变更，并完成 experiment-only Vector enforcement
P1  实现 I2 paired runner、Judge、统计和测试
P2  DeepSeek 20-chunk Context probe
P3  生成 13,086 Context，构建新 A/B 索引
P4  在新索引上跑 DeepSeek Context I1 smoke；仅 promote 时扩大到 450 题（诊断，不门禁）
P5  30-case Agent/Judge operational smoke，冻结 timeout / concurrency
P6  运行 3 × 450 / arm I2 Agent cases
P7  对冻结答案运行 GLM Judge并生成正式 paired 报告
P8  根据第 11 节决定停止或进入非回归/框架化评估
```
