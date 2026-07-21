# Contextual Embedding 有效性验证需求

状态：已评审（I0 evidence harness 已实现；独立 Judge 与正式运行待完成）

日期：2026-07-20

## 1. 验证目标

业务使用 tRPC-Agent-Go（下文简称 TAG）把长文档切分为 chunk，并将每个 chunk 独立生成
向量后写入知识库。

独立 chunk 可能丢失其在父文档中的必要语境。例如：

- chunk 中只出现“该公司”“这一版本”等指代；
- 关键实体、时间或产品名称只出现在文档标题和其他章节；
- 多篇文档包含相似表述，必须结合所属文档才能区分；
- 回答一个问题需要从多篇文档中召回多个相关 chunks。

本需求验证以下假设：

```text
在其他条件保持一致时，
为每个 chunk 补充其在完整父文档中的必要语境后再生成向量，
能够提高 TAG 的检索质量和最终回答质量。
```

本需求不预设该方法最终会纳入 TAG，也不预设其公共 API、扩展点或生产形态。只有验证结果
达到本文定义的有效性门槛后，才评估是否值得进行框架化设计。

## 2. 待验证方法

### 2.1 方法定义

实验方法固定为 Document-aware Contextual Embedding：

```text
完整父文档 + 当前 chunk
    ↓
生成当前 chunk 的简短检索语境
    ↓
检索语境 + 当前原有 embedding text
    ↓
使用与基线相同的 embedder 生成向量
```

生成的检索语境应说明当前 chunk 在父文档中的主题、实体、时间、章节或指代关系，并满足：

- 只使用父文档中存在的信息；
- 不回答用户问题；
- 不添加文档外事实；
- 不改写或摘要整个 chunk；
- 不改变当前 chunk 的业务含义；
- 输出为空时视为失败。

实验使用固定模型、固定 prompt 和确定性生成配置。影响输出的模型或 prompt 发生变化时，
视为不同实验配置，不得合并结果。

### 2.2 Embedding 输入

对照组使用 TAG 当前原本会发送给 embedder 的文本：

```text
BaseEmbeddingText
```

实验组使用：

```text
Context + 明确分隔符 + BaseEmbeddingText
```

如果 reader 已提供自定义 `EmbeddingText`，它属于 `BaseEmbeddingText`，不得被 context
覆盖或丢弃。

Context 只用于生成向量。实验不得改变：

- 原始 `Document.Content`；
- 检索结果返回的 chunk 文本；
- 回答模型看到的知识内容；
- query；
- chunk 边界和顺序；
- sparse/keyword 索引文本；
- reranker 输入。

## 3. 实验实现

### 3.1 实现原则

实验实现只需方便验证本文假设，不要求形成正式框架能力。

允许采用：

- 内部或实验性 package；
- benchmark 专用加载器或 source wrapper；
- 非导出的 option、配置或辅助类型；
- 固定的一种模型和 prompt；
- 只支持本次 benchmark 使用的文档类型；
- 每组实验使用全新索引并全量重建；
- 实验目录内的 context 缓存和运行 manifest。

不应仅为了实验提前设计公共 Contextualizer、通用 source 协议或跨 vector store 抽象。

### 3.2 相同的数据路径

基线组和实验组必须使用同一个实验加载路径：

1. 读取相同的完整父文档；
2. 使用相同 chunker 和参数；
3. 产生内容、顺序和 metadata 相同的 chunks；
4. 基线组直接使用 `BaseEmbeddingText`；
5. 实验组生成 context 后使用 `Context + BaseEmbeddingText`；
6. 调用相同 embedder 和 vector store 写入独立索引。

不能让基线继续走旧 loader、实验组走新 loader，否则无法排除文档读取、chunking 或 metadata
变化带来的影响。

### 3.3 父文档与 chunk

父文档必须是切分前的完整文档内容。实验不得通过拼接带 overlap 的 chunks 反推父文档，
也不得把当前 chunk 自身当作父文档。

每个 chunk 至少保留以下实验标识：

```text
parent document ID
chunk ID
chunk index
parent content hash
chunk content hash
```

这些标识只用于确保 A/B 对齐和结果审计，不要求形成 TAG 的正式文档模型。

### 3.4 Context 生成与缓存

Context 生成发生在 chunking 之后、embedding 之前。

每个生成结果应保存为可复用的实验产物，至少包含：

```text
parent document ID
chunk ID
model
prompt identifier
generated context
input / output / cache token（提供方支持时）
elapsed time
error
```

同一实验配置的检索和回答评测必须复用已经生成的 context，不得在每次评测时重新生成，
避免生成波动和额外费用污染 A/B 结果。

缓存 key 可以由父文档、chunk、模型和 prompt 的 hash 组成，但不要求形成生产级缓存协议。

### 3.5 失败处理

Context 生成失败时：

- 记录明确错误和对应 parent/chunk；
- 不静默使用原始 `BaseEmbeddingText`；
- 不把空 context 当作成功；
- 不把失败样本排除后继续宣称完整实验成功；
- 不在同一实验索引中混合无法识别的 contextual 和 non-contextual vectors。

父文档超过实验模型输入限制时，不得静默截断。可以将该文档记为不支持并终止该次完整
实验，或采用固定且写入 manifest 的实验处理规则。

## 4. Benchmark 前置条件

Benchmark 复现基础已由独立任务交付，当前任务已接管服务器、运行目录和后续实验链路。
本需求负责把交付环境整理为 clean checkout、版本化 runner 和可审计证据；不再重新追求
旧环境或历史数值的严格复现。

复现任务应向本实验提供：

```text
可执行的运行命令
完整环境和模型配置
使用的数据版本
当前 trpc-agent-go-impl 的结果文件
运行日志和有效配置
```

历史 README 数值只用于检查复现结果的量级和趋势。新方法的正式结论必须来自同一环境、
同一代码版本和同一批次重新运行的 A/B，不得把新的实验组直接与历史结果比较。

### 4.1 Baseline reproduction lane

Contextual Embedding 实现前，允许先建立独立的 baseline reproduction lane，用于验证：

- 模型、Embedding、PGVector 和 RAGAS 链路可以运行；
- 运行失败具有足够的诊断信息；
- 重复运行的失败率和指标波动可以观察；
- 昂贵的 Q&A 结果可以在 evaluator 失败后复用。

当前 baseline reproduction lane 固定标识为：

```text
run kind: baseline_reproduction
evidence scope: baseline_stability
dataset: HuggingFace Documentation（54 个 QA）
knowledge base: trpc-agent-go
evaluator: RAGAS
retrieval k: 4
vector store: PGVector
search mode: hybrid（0）
chunk size / overlap: 500 / 50
embedding dimensions: 1024
answer model: GLM-5.2
judge model: 独立低成本模型（正式运行前冻结）
embedding model: BGE-M3
judge max output tokens / per-job timeout: 65536 / 1800 seconds
judge structured-output whole-prompt max attempts: 5
prompt declared max searches: 3
hard max tool iterations: 500
formal A/B eligible: false
```

Judge 必须显式配置独立于 Agent 的 model、URL 和 API key。缺少任一显式配置、任一项
继续回退到 Agent，或者实际 model、endpoint、credential 未分离时，该轮 evidence 状态必须
为 `insufficient`。具体 Judge 型号在校准完成后冻结，并由 manifest 和运行 fingerprint 记录。
Judge reasoning 参数必须保持未提供，并在 manifest 中记录
`reasoning_parameter_supplied=false`。对于 OpenAI 兼容接口返回的纯文本 block list，允许在
进入 RAGAS 前无损归一化为字符串；若结构化输出缺字段，则只允许重新执行完整 prompt，最多
5 次，不得补默认字段或默认分数。重试耗尽后该指标保持缺失，该轮 evidence 状态必须为
`insufficient`；归一化次数、结构化重试次数和恢复次数必须写入结果产物。

其中，`hard max tool iterations = 500` 是 modified-harness watchdog，用于避免近似无界的
工具循环永久挂起，不代表正式实验的科学检索预算。Prompt 声明次数、硬上限和每个样本的
实际工具调用次数必须分别记录。

Baseline reproduction lane 可以为观察稳定性而使用 `--skip-load` 复用已存在的索引，但
必须披露实际 PGVector table、文档行数、索引是否复用以及当前解析到的 TAG module 版本。
该 lane 的结果只能说明当前 GLM-5.2 Agent、冻结的独立 Judge 和 BGE-M3 配置的
可运行性、失败率和波动，不能：

- 作为正式 Contextual Embedding A 组；
- 与 benchmark README 中其他默认模型配置的历史结果直接合并；
- 用于判断 Contextual Embedding 有效或无效；
- 在存在 Agent、Judge 或指标缺失时描述为干净的质量基线。

Agent 失败样本仍应以明确占位值保留在固定问题分母中，并单独报告失败率。如果任一 Agent
或 Judge 失败、任一指标缺失或运行配置不可追溯，该轮结果的证据状态必须为
`insufficient`，但仍可作为排障产物保存。

### 4.2 Judge selection gate

正式 baseline 和 Contextual A/B 前，必须先完成独立低成本 Judge 的校准。校准只重放固定的
Q&A samples，不重新调用 Agent，也不改变检索结果。

Judge calibration 至少检查：

- 固定样本上的完成率、错误类型和指标完整性；
- 重复运行稳定性；
- 对受控劣化 answer/context 的方向判别；
- 与参考评测的逐样本相关性和聚合偏差；
- 实际 token 用量和延迟；如有权威内部单价则附费用，但价格信息不作为阻塞门槛。

校准结果只用于选择 Judge，不属于 baseline stability 或 Contextual effectiveness 证据。旧的
GLM-5.2 Agent + GLM-5.2 Judge 结果仅作为历史运行参考，不与新 Judge 指标直接合并。

### 4.3 Formal contextual A/B lane

正式 Contextual Embedding 验证必须使用独立的 formal contextual A/B lane：

```text
run kind: formal_contextual_ab
evidence scope: contextual_effectiveness
primary dataset: MultiHop-RAG
index: A/B 分别使用全新且隔离的索引
loader: A/B 使用同一个实验加载路径
models: A/B 使用相同 answer、独立 judge 和 embedding 模型
tool budget: A/B 使用相同且由代码硬执行的明确上限
context cache: B 组固定并复用同一批 context
```

Baseline reproduction lane 中的旧索引、历史样本或聚合指标不得直接复用为正式 A。正式
A/B 的具体硬检索上限在进入该 lane 前冻结，并写入 manifest；不能只依靠 Prompt 约束。

## 5. 对照实验

### 5.1 必须运行的实验组

只设置两个必要实验组：

```text
A. Baseline
   BaseEmbeddingText → Embedder

B. Contextual Embedding
   Context + BaseEmbeddingText → 同一个 Embedder
```

两组必须使用：

- 相同 TAG commit；
- 相同 corpus 和问题集；
- 相同文档顺序；
- 相同 chunker、chunk size 和 overlap；
- 相同 embedding 模型和维度；
- 相同 vector store；
- 相同 search mode、top-k 和过滤条件；
- 相同 Agent、query、prompt 和最大检索次数；
- 相同 answer model 和 evaluator；
- 相同错误样本处理规则；
- 相互隔离且全新构建的索引。

本次不通过增加 BM25、reranker、query rewrite 或其他检索方法来提高实验组指标。只有
`B > A` 才能证明 Contextual Embedding 本身有效。

### 5.2 数据集职责

#### MultiHop-RAG

MultiHop-RAG 是主要有效性数据集。它包含完整新闻文章和需要组合多篇文章证据的问题，
用于判断 Contextual Embedding 是否改善多文档证据召回并提高最终回答质量。

其结果承担是否继续投入的主要决策。

#### HuggingFace Documentation

HuggingFace Documentation 用于 smoke test 和普通文档非回归检查。

该数据集规模较小，且当前结果已接近较高召回水平，不单独承担新方法有效性的结论。

#### RGB

RGB 用于检查噪声、信息整合和反事实鲁棒性。

RGB 当前的 passage 本身就是独立文档，缺少可供 contextualization 利用的父文档语境，
因此不用于证明正向收益，只用于检查新方法是否引入明显退化。

### 5.3 运行顺序

1. 使用 A 组跑通小规模 smoke，确认与复现结果量级一致；
2. 使用 B 组跑通相同 smoke，检查 context、向量和检索结果；
3. 固定并缓存 B 组全部 contexts；
4. 在完整 MultiHop-RAG 上依次运行 A 和 B；
5. 在 HuggingFace 和 RGB 上运行非回归检查；
6. 使用相同结果文件生成对比报告。

Smoke 失败或没有任何检索信号时，不继续扩大实验规模。

## 6. 指标与实验产物

### 6.1 质量指标

必须保持 benchmark 当前已有指标：

```text
Faithfulness
Answer Relevancy
Answer Correctness
Answer Similarity
Context Precision
Context Recall
Context Entity Recall
```

如果复现链路可以稳定获得逐查询检索结果，还应记录：

```text
每个 query 的检索文本
返回 chunk 的 ID、rank 和 score
gold evidence coverage
all-evidence-hit
Recall@k
MRR 或 nDCG
```

检索指标用于解释收益来源；最终是否有业务价值仍需同时观察 Answer Correctness。

### 6.2 成本与运行指标

质量与成本分别下结论。至少记录：

```text
父文档数和 chunk 数
context 生成成功数和失败数
context 生成总耗时
contextualization input / output / cache token
contextualization 费用
embedding 耗时
索引总耗时
索引存储变化
query 平均延迟和 p95（可以获得时）
```

本需求不预设成本上限，但不得只报告质量提升而省略索引成本。

### 6.3 必须保存的产物

每次可用于结论的实验必须保存：

```text
运行 manifest
父文档与 chunk 对齐清单
context 缓存
A / B 逐样本结果
A / B 聚合指标
错误清单
成本和耗时
最终对比报告
复现实验的准确命令
```

聚合结果不能替代逐样本结果。

## 7. 有效性标准

### 7.1 主要通过条件

相对同批次 A 组，B 组在完整 MultiHop-RAG 上应同时满足：

1. `Answer Correctness` 绝对提升不少于 `0.02`；
2. `Context Recall` 绝对提升不少于 `0.03`，或可计算的 all-evidence Recall
   绝对提升不少于 `0.05`；
3. `Context Precision` 下降不超过 `0.01`；
4. 提升不是由减少有效样本、忽略失败或改变检索次数获得；
5. 逐样本 paired comparison 的 95% bootstrap confidence interval 不跨越零；
6. 完整实验不存在未披露的 contextualization 失败样本。

历史 MultiHop-RAG 指标可以作为方向性参照，但不作为正式门槛：

```text
Answer Correctness: 0.4984
Context Precision:  0.3574
Context Recall:     0.7733
```

正式判断只比较同批次 A 和 B。

### 7.2 非回归条件

HuggingFace Documentation 和 RGB 的关键指标相对 A 组：

- `Answer Correctness` 不下降超过 `0.01`；
- `Context Precision` 不下降超过 `0.01`；
- 反事实或错误信息导致的回答退化不得明显增加；
- 不得出现系统性同文档错误 chunk 排名上升。

### 7.3 结论

实验结论只能是以下三类之一：

```text
有效：
达到主要通过条件和非回归条件，可以开始评估框架化价值。

无效：
没有达到主要通过条件，停止框架化设计。

证据不足：
存在运行失败、样本不一致、指标不可比或显著性不足，
修复证据问题后再判断，不将其描述为有效。
```

达到有效性标准不自动意味着该方法应成为 TAG 默认能力，也不自动确定其公共 API。

## 8. 验收标准

1. A、B 使用相同的父文档、chunks、embedding 模型和检索配置。
2. A、B 的每个 chunk 可以通过稳定实验 ID 一一对应。
3. B 的唯一检索表示变化是增加由父文档和当前 chunk 生成的 context。
4. B 保持原始 `Document.Content`、query 和回答上下文不变。
5. 已有自定义 `EmbeddingText` 在 B 中得到保留。
6. Context 只使用父文档信息，空输出和生成失败均可观察。
7. 同一组已生成 contexts 被检索和回答评测重复使用。
8. A、B 使用相互隔离的全新索引，不复用不兼容向量。
9. 完整 MultiHop-RAG 结果包含逐样本和聚合对比。
10. HuggingFace 和 RGB 完成非回归检查。
11. 质量、成本和错误分别报告。
12. 结果可以通过保存的命令、manifest 和 context 缓存复现。
13. 结论严格依据本文有效性条件，不因单个指标上涨而宣称方案有效。

## 9. 需求边界

本需求应交付：

```text
一个最小 Contextual Embedding 实验实现
+ feature-off / feature-on 的同路径对照
+ 可审计和复用的 context 实验产物
+ 完整 benchmark A/B 结果
+ 质量、成本和停止结论
```

本需求不包含：

- TAG 公共 Contextualizer API；
- 正式 `WithContextualizer` option；
- 通用 Custom Contextualizer；
- 通用父文档或 GroupedSource 抽象；
- 所有 source 和 vector store 的支持；
- 生产级增量同步、fingerprint和索引迁移；
- 生产级缓存、重试、fallback和监控；
- Contextual BM25 或 sparse 索引改造；
- reranker 输入改造；
- query rewrite、HyDE 或其他查询阶段方法；
- parent/neighbor expansion；
- Late Chunking、token hidden state 或 span pooling；
- GraphRAG 或知识图谱构建；
- 默认启用或对外发布该方法；
- 旧 benchmark 环境和历史数值的再次复现。

如果实验未达到有效性标准，上述框架化事项不再继续。若实验有效，后续需求应基于实验代码
和结果中已经确认的真实约束重新制定，而不是直接沿用本实验的临时代码形态。
