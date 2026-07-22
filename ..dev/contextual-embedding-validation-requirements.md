# Contextual Embedding 有效性验证需求

状态：I0 已关闭；I1 Retrieval-only A/B 协议待评审

日期：2026-07-20

最近更新：2026-07-22

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

历史 README 数值只用于检查复现结果的量级和趋势。README 作者已确认，历史实验实际使用
`DeepSeek-V3.2 + Gemini-3-Flash + BGE-M3`；原 README 中的 Qwen3.5 Judge 是文档笔误。
新方法的正式结论必须来自同一环境、同一代码版本和同一批次重新运行的 A/B，不得把新的
实验组直接与历史结果当作严格 A/B。

### 4.1 Baseline reproduction lane

I0 已建立并完成独立的 baseline reproduction lane，用于验证：

- 模型、Embedding、PGVector 和 RAGAS 链路可以运行；
- 运行失败具有足够的诊断信息；
- 重复运行的失败率和指标波动可以观察；
- 昂贵的 Q&A 结果可以在 evaluator 失败后复用。

I0 baseline reproduction lane 的最终标识为：

```text
run kind: baseline_reproduction
evidence scope: baseline_stability
dataset: HuggingFace Documentation（54 个 QA）
knowledge base: trpc-agent-go
evaluator: RAGAS
retrieval k: 4
vector store: PGVector
search mode: hybrid（0）
index mode: reuse existing PGVector index
chunk size / overlap: 500 / 50
embedding dimensions: 1024
answer model: GLM-5.2
judge model: Gemini-3-Flash
embedding model: BGE-M3
judge max output tokens: 65536
judge reasoning parameter: omitted
judge structured-output whole-prompt max attempts: 5
prompt declared max searches: 3
hard max tool iterations: 500
evidence status: valid
formal A/B eligible: false
```

I0 最终完成 54/54 样本、0 个 Agent 错误，七项指标均为 54/54 个有限值：

| 指标 | 当前 GLM-5.2 + Gemini-3-Flash | 历史 DeepSeek-V3.2 + Gemini-3-Flash | 差值 |
|---|---:|---:|---:|
| Faithfulness | 0.9026 | 0.9815 | -0.0789 |
| Answer Relevancy | 0.9694 | 0.8799 | +0.0895 |
| Answer Correctness | 0.6612 | 0.8104 | -0.1492 |
| Answer Similarity | 0.7850 | 0.7240 | +0.0610 |
| Context Precision | 0.6149 | 0.7098 | -0.0949 |
| Context Recall | 0.8333 | 0.9444 | -0.1111 |
| Context Entity Recall | 0.4964 | 0.4867 | +0.0097 |

两次结果使用相同 Judge 和 Embedding，但 Agent、代码版本、索引、有效检索行为和工具预算
仍可能不同，因此只作方向性比较。该结果完成的是 evidence harness 验收，不证明
Contextual Embedding 有效，也不是正式 A 组。I0 至此关闭；在最终非回归阶段前不再重复运行
HF54。

Judge 必须显式配置独立于 Agent 的 model、URL 和 API key。缺少任一显式配置、任一项
继续回退到 Agent，或者实际 model、endpoint、credential 未分离时，该轮 evidence 状态必须
为 `insufficient`。Judge 固定为 Gemini-3-Flash，并由 manifest 和运行 fingerprint 记录。
Judge reasoning 参数必须保持未提供，并在 manifest 中记录
`reasoning_parameter_supplied=false`。对于 OpenAI 兼容接口返回的纯文本 block list，允许在
进入 RAGAS 前无损归一化为字符串；若结构化输出缺字段，则只允许重新执行完整 prompt，最多
5 次，不得补默认字段或默认分数。重试耗尽后该指标保持缺失，该轮 evidence 状态必须为
`insufficient`；归一化次数、结构化重试次数和恢复次数必须写入结果产物。
若完整 evaluator 已产出且仅有少量 metric cell 缺失，允许从该结果执行缺失单元恢复，
但必须校验源结果与 samples checkpoint、保持 Judge 评分身份一致、禁止覆盖任何已有有限值，
并在新产物中记录源结果摘要、源/当前仓库版本、请求/恢复/剩余 cell 及独立恢复耗时与用量。
timeout 与 worker 数可作为执行控制调整；模型、端点、`max_tokens`、reasoning 未提供状态及
结构化输出策略不得变化。恢复后仍有任一缺失值时，evidence 继续为 `insufficient`。

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

### 4.2 Judge identity（已完成）

历史实验和当前 I0 均使用 Gemini-3-Flash Judge；当前 I0 额外确认了 Agent 与 Judge 的 model、
URL 和 credential 显式分离。后续 I2 继续冻结该 Judge，reasoning 参数保持未提供，只提高
`max_tokens` 上限，不以成本作为模型选择或实验通过条件。

如果后续替换 Judge，应将其视为新的评测配置，只重放固定 Q&A samples 进行完成率、稳定性、
受控劣化方向和聚合偏差校准，不重新调用 Agent。不同 Judge 配置的数值不得直接合并。

I1 是确定性的 retrieval-only 评测，不初始化 Agent、RAGAS 或 Gemini Judge；用于生成
chunk context 的模型属于索引构建配置，不是 Judge，并且其输出必须缓存。

### 4.3 I1 Retrieval-only contextual A/B lane

下一阶段先运行独立的 retrieval-only A/B：

```text
run kind: retrieval_contextual_ab
evidence scope: retrieval_effectiveness
primary dataset: MultiHop-RAG（609 篇文章、450 道题）
query: 原始问题，不做 rewrite
agent / RAGAS / judge: none
retrieval k: 4、10、20
index: A/B 分别使用全新且隔离的索引
loader: A/B 使用同一个实验加载路径
embedding model: A/B 均为 BGE-M3
retrieval configuration: A/B 完全相同
context cache: B 组固定并复用同一批 context
```

MultiHop-RAG 的每个评测项必须保留稳定 question ID、`question_type` 和完整原始
`evidence_list`，不能只把 `fact` 拼接到 `QAItem.context`。语料预处理还必须为每篇文章、每个
gold evidence 和每个 chunk 建立稳定 ID，并保存 evidence 到 parent document / chunk 的映射。
Document hit 以 `parent_document_id` 判断；Evidence hit 以预先生成并冻结的 evidence-to-chunk
映射判断。无法映射的 evidence 必须进入数据校验报告并使正式证据为 `insufficient`，不得在
计算分母时静默忽略。

当前代码尚未满足该 lane：`QAItem` 没有 evidence metadata，MultiHop loader 只把 gold facts
拼入 `context`，`main.py` 又固定初始化 RAGAS。I1 因此需要单独的 evidence-aware
retrieval-only runner，而不是在现有端到端 runner 中增加旁路条件。

### 4.4 I2 End-to-end contextual A/B lane

只有 I1 达到第 7.1 节的方法有效性门槛后，才运行完整 450 题的端到端 A/B：

```text
run kind: formal_contextual_ab
evidence scope: end_to_end_effectiveness
primary dataset: MultiHop-RAG
index: A/B 分别使用全新且隔离的索引
loader: A/B 使用同一个实验加载路径
answer model: A/B 均为 GLM-5.2
judge model: A/B 均为 Gemini-3-Flash
embedding model: A/B 均为 BGE-M3
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
- I1 使用相同 query 且两组都不运行 Agent 或 Judge；
- I2 使用相同 Agent、query、prompt、最大检索次数、answer model 和 evaluator；
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

HuggingFace Documentation 只用于最终一次普通文档非回归检查。

I0 已完成其基础设施验收职责。该数据集规模较小，不再用于 I1 smoke，也不单独承担新方法
有效性的结论。

#### RGB

RGB 用于检查噪声、信息整合和反事实鲁棒性。

RGB 当前的 passage 本身就是独立文档，缺少可供 contextualization 利用的父文档语境，
因此不用于证明正向收益，只用于检查新方法是否引入明显退化。

### 5.3 运行顺序

1. 扩展 MultiHop-RAG 数据契约并完成全部 gold evidence 映射预检；
2. 使用同一路径生成 A/B 完全对齐的父文档和 chunks；
3. 使用 A 组运行 MultiHop-RAG 小样本 retrieval-only smoke；
4. 生成并冻结 B 组 context cache，运行相同 smoke；
5. 分别全量构建 A/B 新索引；
6. 对完整 450 题运行 retrieval-only A/B，并生成 paired bootstrap 报告；
7. I1 不达标则停止；达标后再运行完整 450 题的 GLM-5.2 + Gemini-3-Flash 端到端 A/B；
8. 最终只运行一次 HuggingFace 和 RGB 非回归；
9. 使用同一份 lineage 生成检索、回答、成本和错误对比报告。

Smoke 失败或没有任何检索信号时，不继续扩大实验规模。

## 6. 指标与实验产物

### 6.1 质量指标

I1 retrieval-only 必须计算：

```text
Document Recall@4 / @10 / @20
Evidence Recall@4 / @10 / @20
All-evidence Recall@4 / @10 / @20
MRR
NDCG
逐样本 paired delta 和 paired bootstrap 95% CI
```

每个 query 还必须保存返回 chunk 的 ID、parent document ID、rank、score、命中的 gold
document/evidence ID 和问题类型。指标必须使用同一份冻结的 evidence mapping 计算，聚合值按
全量、comparison、inference、temporal 分别报告。

I2 端到端继续保持 benchmark 当前已有指标：

```text
Faithfulness
Answer Relevancy
Answer Correctness
Answer Similarity
Context Precision
Context Recall
Context Entity Recall
```

I1 检索指标用于先判断索引方法本身是否有效；I2 的 Answer Correctness 和 RAGAS context 指标
用于判断检索收益能否传导到最终回答。两阶段结果不得相互替代。

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

### 7.1 I1 方法有效性门槛

相对同批次 A 组，B 组在完整 MultiHop-RAG retrieval-only 结果上应同时满足：

1. 预先冻结的主指标 `All-evidence Recall@10` 绝对提升不少于 `0.05`；
2. `Evidence Recall@10` 绝对提升不少于 `0.03`；
3. 两个主指标的逐样本 paired bootstrap 95% CI 下界均大于 `0`；
4. `Document Recall@4` 和 `Evidence Recall@4` 均不得下降超过 `0.01`；
5. comparison、inference、temporal 三类中至少两类为正向，任一类不得下降超过 `0.02`；
6. A/B 样本、query、chunks、检索参数和有效分母完全一致，且没有未披露的 evidence mapping
   或 contextualization 失败。

I1 未达到以上条件时停止，不运行 I2，也不进入框架化设计。

### 7.2 I2 端到端有效性门槛

相对同批次 A 组，B 组在完整 MultiHop-RAG 上应同时满足：

1. `Answer Correctness` 绝对提升不少于 `0.02`；
2. `Context Recall` 绝对提升不少于 `0.03`；
3. `Context Precision` 下降不超过 `0.01`；
4. Answer Correctness 的逐样本 paired bootstrap 95% CI 下界大于 `0`；
5. 提升不是由减少有效样本、忽略失败或改变检索次数获得；
6. 完整实验不存在未披露的 Agent、Judge 或 contextualization 失败样本。

### 7.3 历史能力参照与声明边界

历史数值按数据集分别作为第二道能力参照，不能跨数据集使用。

MultiHop-RAG 的历史 tRPC-Agent-Go 参照为：

```text
Answer Correctness: 0.4984
Context Precision:  0.3574
Context Recall:     0.7733
```

HuggingFace 54 题的历史 tRPC-Agent-Go 参照为：

```text
Answer Correctness: 0.8104
Context Precision:  0.7098
Context Recall:     0.9444
```

`0.8104 / 0.7098 / 0.9444` 只能用于最终 HF54 非回归，不能作为 MultiHop-RAG 的通过阈值。
如果 B 只超过同批次 GLM-5.2 A、但没有达到对应数据集的历史参照，只能说明方法对当前 GLM
lane 有效，不能声称能力已超过 README 中的原实现。

即使 B 数值超过历史参照，由于 Agent、代码、索引和运行时行为仍可能不同，也只能表述为
“超过 README 同数据集历史参照”。若要严格声称“超过原 tRPC-Agent-Go 实现”，还需要在
当前 harness 下复跑 DeepSeek-V3.2 + Gemini-3-Flash 对照。

### 7.4 非回归条件

HuggingFace Documentation 和 RGB 的关键指标相对 A 组：

- `Answer Correctness` 不下降超过 `0.01`；
- `Context Precision` 不下降超过 `0.01`；
- 反事实或错误信息导致的回答退化不得明显增加；
- 不得出现系统性同文档错误 chunk 排名上升。

### 7.5 结论

实验结论只能是以下四类之一：

```text
有效且达到能力目标：
I1、I2、对应数据集历史能力参照和非回归条件均达到，可以开始评估框架化价值。

方法有效但能力目标未达到：
同批次 B 显著超过 A，但未达到对应历史参照；只证明当前 GLM lane 上的方法收益。

无效：
I1 或 I2 没有达到方法有效性门槛，停止框架化设计。

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
9. I1 完整 MultiHop-RAG 结果包含逐样本检索结果、gold evidence 命中和聚合对比。
10. 只有 I1 达标才运行 I2，且 I2 完整 450 题包含逐样本和聚合对比。
11. HuggingFace 和 RGB 只在最终阶段完成一次非回归检查。
12. 质量、成本和错误分别报告。
13. 结果可以通过保存的命令、manifest 和 context 缓存复现。
14. 结论严格依据本文有效性条件，不因单个指标上涨而宣称方案有效。

## 9. 需求边界

本需求应交付：

```text
一个最小 Contextual Embedding 实验实现
+ feature-off / feature-on 的同路径对照
+ 可审计和复用的 context 实验产物
+ evidence-aware retrieval-only A/B 与门禁报告
+ 通过门禁后的完整端到端 A/B 结果
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
