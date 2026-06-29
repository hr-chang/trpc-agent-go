# 核心问题讨论：Artifact 命名与 Hydrate 时机

## 1. 文档定位
本文记录需求包 A 中两个核心问题的讨论结论：
- artifact 命名：标准 `ContentParts` 中的 inline 多模态内容外存到 `artifact.Service` 时，artifact filename 如何生成。
- hydrate 时机：session 中保存 internal artifact ref 后，什么时候恢复成模型、回放、调试或评测可消费的内容。

阅读本文前只需知道：A 包会在写入 `session.Events` 前，把标准消息结构里的 image/audio/file inline bytes 或 data URL 替换为轻量 internal ref/metadata，避免 session backend 长期保存大块多模态内容。

## 2. 当前结论
### 2.1 Artifact 命名
已确认：
- 命名格式：
    ```text
    sessionpart_<unix-ms>_<sha256-16>_<uuid>.<ext>
    ```
- `unix-ms`：治理层生成 artifact name 时的 Unix millisecond timestamp，用于排序和排查。
- `sha256-16`：内容 sha256 的前 16 位，用于调试、弱校验和离线过滤，不承担唯一性主责。
- `uuid`：由治理层通过 `uuid.NewString()` 生成，作为 artifact object id。
- `ext`：只用于可读性，真实恢复依赖 metadata 中的 mime type / format。
- owner 信息：event ID、request ID、message index、part index 等放 metadata，不进入 filename。

设计取舍：
> filename 负责安全唯一和基本可读；metadata 负责定位、恢复和排查。

### 2.2 Hydrate 时机
已确认：
- A 包首期 `GetSession` 默认 hydrate，保持业务可见行为与历史 inline session 一致。
- hydrate 路径采用 copy-on-write，只影响返回给调用方的 hydrated view，不污染 persisted view。
- session persisted view 中可以保存 internal artifact ref。
- provider adapter 前不允许存在 unresolved internal artifact ref。
- 模型请求构造层必须 hydrate 或显式转换 internal ref。

后续优化：
- without-hydrate / persisted view / lazy hydrate 读取入口。
- hydrate API 是否对框架外公开。
- 前端回放、调试、评测按需 hydrate。
- provider file upload 和 provider file id 缓存。

## 3. 已确认边界
治理边界：
- 治理发生在 `AppendEvent` 进入具体 session backend 之前。
- 治理动作构造 persisted event，不修改 runtime event。
- A 包只治理进入 `session.Events` 的标准 `model.Message.ContentParts`。
- A 包不递归扫描 tool result JSON、业务自定义 JSON/string/metadata、`StateMap`。

默认策略：
- 多模态外存默认关闭，业务需要显式开启并配置 artifact 能力。
- artifact 保存失败默认 fail closed。
- 首期不提供 fail open 兼容开关。
- 首期 `ContentRef` 不强制加 `schema_version`，缺省版本即 v1。

## 4. Artifact 命名讨论
### 4.1 现有 artifact 模型
仓库中的 `artifact.Service` 已经是 `scope + filename + version` 模型。

scope 由 `artifact.SessionInfo` 提供：
- `AppName`
- `UserID`
- `SessionID`

保存与读取：
- 保存时调用方传入 `filename`。
- `SaveArtifact` 返回 version。
- 同一 scope 和 filename 下多次保存会形成多个 version。
- 读取时通过 `SessionInfo + filename + version` 精确读取。
- version 为 nil 时读取 latest。

结论：
- A 包 persisted event 必须 pin version。
- 不能依赖 latest，否则同名 artifact 后续新增版本会影响历史回放。

### 4.2 参考模型
#### Google ADK
ADK artifact 采用类似模型：
- artifact 不直接存入 session/state。
- ArtifactService 按 `app_name / user_id / session_id / filename` 保存。
- save 返回 version。
- load 可指定 version，不指定则取 latest。

启发：
- `filename + scope + version` 是合理抽象。
- A 包是框架自动外存，需要比业务传入 filename 更谨慎地生成 filename。

#### LangGraph
LangGraph 更强调 checkpoint/store 分层：
- checkpoint 保存 graph state。
- 大对象不建议直接放入 checkpoint。
- 通常把大对象外存，只在 state 中保存轻量 URL/ID/metadata。

启发：
- persisted state/session 中保存 ref 是合理方向。
- LangGraph 不提供可直接复用的 artifact filename 规则。

#### OpenAI file refs
OpenAI 使用 provider 侧 opaque id：
- 上传文件后返回 `file-xxx`。
- 请求中使用 `file_id`、`file_data`、`file_url`、`image_url`。
- `file_id` 是 provider 侧资源 ID，不是框架 artifact ref。
- `filename` 更多是输入文件的语义/展示信息，不承担框架内部存储主键语义。

启发：
- 最稳的外部引用通常是存储系统生成的 opaque id。
- 当前仓库 artifact 接口仍以调用方传入 filename 为主，因此 A 包需要生成安全 filename。

### 4.3 候选方案对比
#### 固定 filename + version
示例：
```text
session_multimodal
```

判断：
- 不采用。
- 不同 message/part 会被表达成“同一个文件的多个版本”，语义不准确。
- latest 语义危险。
- 删除 filename 会影响该 filename 下全部 version。
- S3 实现当前对同 filename 并发保存不安全。

#### 原始文件名 + version
示例：
```text
<original-filename>
```

判断：
- 不作为唯一键。
- 原始文件名可能重复、过长、包含隐私或非法字符。
- 原始文件名可能缺失，例如纯 bytes、data URL、模型返回图片。
- 原始文件名应保留在 metadata 中，用于展示和调试。

#### 结构化 filename
示例：
```text
sessionevt_<event-key>_msg_<msg-index>_part_<part-index>_<sha256-prefix>.<ext>
```

判断：
- 不作为首期默认方案。
- 优点是可读性强，能定位到 event/message/part。
- 缺点是暴露结构信息，并且依赖 event key、message index、part index 的稳定语义。
- 当前已改为 owner 信息进 metadata，filename 使用 opaque UUID。

#### opaque generated id
示例：
```text
sessionpart_<unix-ms>_<sha256-16>_<uuid>.<ext>
```

判断：
- 采用。
- `uuid.NewString()` 是仓库内已有的通用 ID 生成方式。
- 每个 content part 独立生成 filename，避免同 filename 多 version 承担 part 区分职责。
- 排查依赖 metadata 反查，这是可接受的工程取舍。

### 4.4 当前命名规则
最终格式：
```text
sessionpart_<unix-ms>_<sha256-16>_<uuid>.<ext>
```

配套规则：
- timestamp 使用治理层生成 artifact name 时的 Unix millisecond timestamp。
- filename 不包含 `/`。
- 不使用用户原始文件名作为唯一键。
- 原始文件名只放 metadata。
- 完整 sha256 放 metadata。
- name 中 sha256 前缀采用 16 hex chars。
- hash 放在 uuid 前面，利于人眼扫描和离线过滤。
- 当前 artifact API 主要按完整 filename 读取，hash 前移不会直接带来在线查询能力。
- 首期更看重按时间排序和排查，因此采用 `unix-ms` 在前、hash 在中、uuid 在后的顺序。
- ext 只用于可读性，恢复以 metadata 为准。
- persisted ref 必须 pin version：`artifact://<name>@<version>`。
- event ID、request ID、message index、part index 等 owner 信息放 metadata。

## 5. Hydrate 时机讨论
### 5.1 背景
A 包写入 session backend 的是 persisted view：
- inline bytes/data URL 被移除。
- persisted event 保存 internal artifact ref 和 metadata。
- runtime event 不被修改，当前轮模型调用不受持久化减重影响。

hydrate 指：
- 读取 `artifact://<name>@<version>`。
- 恢复 bytes。
- 或进一步转换为特定 consumer 可接受的格式，例如 provider request 的 base64/data URL/file_data。

### 5.2 参考模型
#### Google ADK
ADK artifact 由 ArtifactService 管理，不默认塞回 session/state。使用时通过 context 显式 `load_artifact(filename, version)`。

启发：
- JIT load 会增加真正消费 artifact 时的时延。
- ADK 把成本放在真正需要 artifact 的调用点。

#### LangGraph
LangGraph 推荐不要把大对象直接放入 checkpoint。大对象通常外存，state/checkpoint 里放引用和 metadata。node 需要时再按需读取。

启发：
- 不应让所有 checkpoint/session load 都承担大对象恢复成本。
- 具体消费点 JIT 读取是常见性能取舍。

#### OpenAI file refs
OpenAI provider request 接受 provider 可消费字段：
- `file_id`
- `file_data`
- `file_url`
- `image_url`

启发：
- OpenAI 不认识框架内部 `artifact://`。
- provider adapter 前必须 resolve internal ref。
- resolve 后是直接带 bytes/base64，还是上传 provider 获得 file_id，属于后续性能优化问题。

### 5.3 候选方案对比
#### `GetSession` 默认 hydrate
判断：
- 采用为 A 包首期默认方案。
- 优点是业务读到的 session 接近旧版本 inline 行为。
- 缺点是读取性能收益有限，且 artifact backend 故障可能影响完整 session 读取。
- 当前取舍是兼容优先，without-hydrate / lazy hydrate 后续再做优化。

#### `GetSession` 默认不 hydrate
判断：
- 作为后续性能优化方向保留。
- 优点是默认读路径轻量。
- 缺点是业务直接读 session 时会看到 ref/metadata，而不是 bytes，升级成本较高。

#### 模型请求构造前 hydrate / convert
判断：
- A 包闭环必须具备。
- provider adapter 前不允许 unresolved internal ref。
- 首期不做 provider 上传/file id 缓存，但要保留后续优化空间。

#### message/event 粒度 hydrate helper
判断：
- 推荐作为内部基础能力。
- `GetSession` 默认 hydrate 可以复用该能力。
- 首期不对框架外公开，但应放在可测试的内部 helper 中，供 `GetSession` 和模型请求构造链路复用；后续业务明确有需求时再评估公开 API。

### 5.4 当前 hydrate 规则
首期规则：
- `GetSession` 默认 hydrate，保持业务可见行为与历史 inline session 一致。
- hydrate 路径采用 copy-on-write，不把 bytes 写回 persisted event。
- 模型请求构造前必须 hydrate 或显式转换 internal ref。
- provider adapter 前发现 unresolved internal ref 时返回明确错误。
- hydrate 失败必须返回明确错误，不能静默丢内容。

后续优化：
- without-hydrate session 读取入口。
- persisted view / lazy hydrate。
- 前端回放、调试、评测按需 hydrate。
- 同一次 invocation 内短生命周期 cache。
- provider file upload 和 provider file id 缓存。

## 6. 最终结论
建议初版收敛为：
- artifact 命名采用 `sessionpart_<unix-ms>_<sha256-16>_<uuid>.<ext>`。
- artifact ref 使用 `artifact://<name>@<version>`，必须 pin version。
- `ContentRef` 在 `model.ContentPart` 上以明确统一字段承载，不分散写入 `Image`、`Audio`、`File` 各自结构。
- `ContentRef` 首期不强制加 `schema_version`，缺省版本即 v1。
- `GetSession` 默认 hydrate，保持业务可见行为与历史 inline session 一致。
- hydrate 路径采用 copy-on-write，不把 bytes 写回 persisted event。
- 模型请求构造前必须 hydrate 或显式转换 internal ref。
- hydrate helper 首期仅作为框架内部可测试能力，不对框架外公开。
- 首期不做 provider 上传和 provider file id 缓存。

建议后续优化：
- without-hydrate / persisted view / lazy hydrate 读取入口。
- hydrate helper 的 public/internal 边界，如业务明确需要手动 hydrate 再评估公开。
- orphan artifact 的反引用索引和自动清理。
