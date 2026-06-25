# 多模态存储路径盘点

## 1. 盘点目的
本盘点不是列出仓库中所有存储，而是从多模态内容第一次进入框架或由框架产生开始，追踪它在框架内的流转、复制、持久化、外发和回放路径，找出哪些环节会把大块非文本内容放进不合适的存储。

治理的落脚点通常发生在“存储时或存储前”，因此本盘点采用如下路径视角：
```text
入口/来源
  -> 框架内标准表示
  -> 运行时消费链路
  -> 存储/外发/回放点
  -> 链路终止点
  -> 治理判断
```

## 2. 标注约定
- `[存储点]`：会落 DB、Redis、对象存储、本地文件、evalset 等。
- `[外发点]`：会发往 provider、OTLP collector、Langfuse、外部 gateway 等。
- `[回放点]`：从已有存储读取并返回给用户或前端。
- `[临时态]`：仅运行时内存或临时 workspace 文件，不一定长期保存。
- `P0/P1/P2`：治理优先级，不等同于版本拆分。

## 3. 入口总览
| 入口/来源 | 第一手多模态形态 | 标准表示 | 主要存储/外发点 | 风险 | 规划判断 |
|---|---|---|---|---|---|
| 用户消息 | `ContentParts`、bytes、base64、path | `model.Message` | `session.Events`、telemetry、debuglog、eval recorder、checkpoint | 高 | P0 |
| `RunOptions.Messages` / seed history | 历史 `model.Message` | `model.Message` | `session.Events`、model request、telemetry | 高 | P0 |
| `UserMessageRewriter` 输出 | 改写后的多条消息 | `model.Message` | `session.Events`、model request、telemetry | 高 | P0 |
| AG-UI 输入 | `InputContent.Data` / URL | `model.ContentPart` + AG-UI event | `session.Events`、`session.Tracks`、MessagesSnapshot | 高 | P0/P1 |
| OpenAI-compatible API 输入 | `image_url.url`，可能是 URL 或 data URL | `model.ContentPart.Image.URL` | `session.Events`、telemetry、debuglog | 中-高 | P1 |
| A2A Server 入站 | `FileWithBytes` / URI | `model.ContentPart` | `session.Events`、telemetry、provider API | 高 | P1 |
| A2A Agent 远端响应 | remote `Message` / `Task.Artifacts` | `event.Event` / `model.Message` | `session.Events`、telemetry、debuglog | 中-高 | P1 |
| OpenClaw Gateway 输入 | 上传文件、图片、音频、host ref | uploads file / `model.ContentPart` | uploads、`session.Events`、debug recorder | 高 | P1 |
| Tool Result | 文本、大 JSON、未来多模态 | tool result message | `session.Events`、next model request、telemetry、debuglog | 中-高 | P1 |
| MCP Tool Image Result | MCP image item base64 | `model.Message.Image.Data` | `session.Events`、model request、debug recorder | 高 | P1 |
| CodeExecutor / Skill 输出 | workspace 文件、artifact ref | file / artifact / tool result | artifact、`session.Events`、workspace metadata | 中 | P1 |
| Model / Provider 响应 | 文本、tool call、未来文件/图片 | `model.Response` / `model.Message` | `session.Events`、telemetry、debuglog | 中 | P1/P2 |
| Graph State / Interrupt 输入 | graph state、resume payload | `graph.State` / checkpoint payload | checkpoint、session state | 中-高 | P2 |
| Evaluation Recorder | 录制 invocation/message/result | eval case/result | evalset local/mysql、eval result | 高 | P2 |

## 4. 分入口存储路径
### 4.1 用户消息入口
入口形态：
- 业务直接传入 `model.Message`
- `model.ContentPart`
- `AddImageData`、`AddAudioData`、`AddFileData` 等 bytes helper
- `AddImageFilePath`、`AddAudioFilePath`、`AddFilePath` 等 path helper
- base64 输入在业务侧或协议转换层被转换成 bytes

标准表示：
- `model.Message`
- `ContentParts`
- `Image.Data`
- `Audio.Data`
- `File.Data`
- 或已外部化的 `URL` / `FileID`

链路：
```text
用户消息
  -> model.Message.ContentParts
  -> runner.resolveCurrentTurnMessages
  -> invocation.Message
  -> runner.persistCurrentTurnMessages
      [存储点] session.Events
  -> ContentRequestProcessor 构造 model.Request
      [外发点] provider API
  -> telemetry/debuglog/model callbacks
      [外发点] OTLP / Langfuse
      [存储点] debuglog / trace backend
  -> evaluation recorder（启用时）
      [存储点] evalset / eval result
  -> graph agent 场景（如果消息进入 graph state）
      [存储点] graph checkpoint
```

主要存储点：
| 存储点 | 风险 | 原因 | 治理判断 |
|---|---|---|---|
| `session.Events` | 高 | event JSON 会保存完整 `ContentParts`，`[]byte` 变 base64 | P0，主治理点 |
| telemetry / Langfuse | 高 | request/messages/response span 属性可能外发 base64 | P1，观测治理 |
| debuglog / ExecutionTrace | 高 | snapshot 可能直接 marshal request/response/event | P1，调试治理 |
| graph checkpoint | 中-高 | graph state 若含 messages，会复制多模态 | P2，checkpoint 治理 |
| eval recorder / evalset | 高 | 录制完整 `model.Message` | P2，评测资产治理 |

链路终止点：
- 当前轮 provider API 调用。
- session 历史持久化。
- 可选观测、调试、评测、graph checkpoint 存储。

治理重点：
- 当前运行时消息保持可用。
- 持久化视图中将 inline bytes 转为引用。
- 后续从 session 恢复时按需 hydrate。

### 4.2 `RunOptions.Messages` / Seed History 入口
入口形态：
- 业务在 run option 中传入历史消息。
- 历史消息可能包含 `ContentParts` 和 inline bytes。

标准表示：
- `[]model.Message`

链路：
```text
RunOptions.Messages
  -> runner.seedSessionHistory（空 session 时）
      [存储点] session.Events
  -> ContentRequestProcessor 历史消息投影
      [外发点] provider API
  -> telemetry/debuglog（启用时）
      [外发点/存储点] trace/debug snapshot
```

主要存储点：
| 存储点 | 风险 | 原因 | 治理判断 |
|---|---|---|---|
| `session.Events` | 高 | seed history 会被包装成 event 持久化 | P0 |
| telemetry/debuglog | 高 | model request 可能带完整历史 | P1 |

链路终止点：
- seed history 成为 canonical session transcript。
- 后续模型请求使用该历史。

治理重点：
- 不应只处理当前 user message，也要处理 seed history。
- 历史消息外存后仍需可恢复。

### 4.3 `UserMessageRewriter` 输出入口
入口形态：
- rewriter 将原始用户消息改写为一组当前轮消息。
- 改写结果可能仍包含多模态内容。

标准表示：
- `[]model.Message`

链路：
```text
原始 user message
  -> UserMessageRewriter
  -> currentTurnMessages
  -> runner.persistCurrentTurnMessages
      [存储点] session.Events
  -> invocation.Message 取最后一条
  -> model request
      [外发点] provider API
```

主要存储点：
| 存储点 | 风险 | 原因 | 治理判断 |
|---|---|---|---|
| `session.Events` | 高 | rewriter 输出会以消息事件形式进入 session | P0 |
| telemetry/debuglog | 中-高 | 当前轮请求可能携带改写结果 | P1 |

链路终止点：
- 改写消息进入 session 和当前模型请求。

治理重点：
- 治理入口不能只看原始 message，应覆盖 rewriter 后的最终持久化消息。

### 4.4 AG-UI 输入入口
入口形态：
- AG-UI `InputContent`
- `InputContent.Data` 中的 base64
- `InputContent.URL`

标准表示：
- `server/agui/internal/multimodal` 将 AG-UI 输入转换成 `model.ContentPart`
- AG-UI track 中可能保留 user message custom event

链路：
```text
AG-UI InputContent
  -> multimodal.UserMessageFromInputContents
  -> model.Message.ContentParts
  -> runner user message
      [存储点] session.Events
  -> AG-UI recordUserMessage / tracker.AppendEvent
      [存储点] session.Tracks
  -> MessagesSnapshot
      [回放点] 前端 replay
  -> model request
      [外发点] provider API
  -> telemetry/debuglog
      [外发点/存储点] trace/debug snapshot
```

主要存储点：
| 存储点 | 风险 | 原因 | 治理判断 |
|---|---|---|---|
| `session.Events` | 高 | AG-UI 输入转为 `model.Message` 后进入普通 session event | P0 |
| `session.Tracks` | 高 | AG-UI track custom event 可能保存 `InputContent.Data` | P0/P1 |
| MessagesSnapshot | 中-高 | 读回 track 并返回前端，可能再次暴露 base64 | P1 |
| telemetry/debuglog | 高 | 继承 model request/event 风险 | P1 |

链路终止点：
- 模型调用。
- session events。
- AG-UI track。
- 前端 replay。

治理重点：
- AG-UI 是框架重点路径，应纳入整体规划。
- 只治理 `session.Events` 不够，track 是独立存储路径。

### 4.5 OpenAI-compatible API 输入入口
入口形态：
- OpenAI chat request 中的 `messages[].content[]`
- `image_url.url`
- 普通 URL
- data URL，例如 `data:image/png;base64,...`

标准表示：
- `server/openai/converter.go` 将 `image_url.url` 转成 `model.ContentPart.Image.URL`
- 当前转换不会解码 data URL 成 `Image.Data`

链路：
```text
OpenAI-compatible request
  -> server/openai converter
  -> model.Message.ContentParts
  -> runner / model request
      [存储点] session.Events
  -> telemetry/debuglog
      [外发点/存储点] trace/debug snapshot
```

主要存储点：
| 存储点 | 风险 | 原因 | 治理判断 |
|---|---|---|---|
| `session.Events` | 中-高 | 普通 URL 风险低；data URL 本质是 inline base64，可能作为字符串进入 event | P1 |
| telemetry/debuglog | 中-高 | request/event snapshot 会复制 data URL 字符串 | P1 |

链路终止点：
- runner/session。
- provider API 或兼容接口响应。

治理重点：
- 普通 URL 不强制重托管。
- data URL 不应视为普通外部引用，应作为 inline 大对象纳入治理。

### 4.6 A2A Server 入站入口
入口形态：
- A2A file part
- `FileWithBytes`
- `FileWithURI`

标准表示：
- `server/a2a` converter 转换成 `model.ContentPart`

链路：
```text
A2A FilePart
  -> server/a2a converter
  -> model.Message.ContentParts
  -> runner
      [存储点] session.Events
  -> model request
      [外发点] provider API
  -> telemetry/debuglog
      [外发点/存储点] trace/debug snapshot
```

主要存储点：
| 存储点 | 风险 | 原因 | 治理判断 |
|---|---|---|---|
| `session.Events` | 高 | `FileWithBytes` 转成 inline data 后随 event 落库 | P1 |
| telemetry/debuglog | 高 | request/event 快照可能携带 bytes/base64 | P1 |

链路终止点：
- runner/session。
- provider API。

治理重点：
- 优先鼓励 `FileWithURI` 或 artifact/业务引用。
- `FileWithBytes` 应纳入 inline bytes 治理。

### 4.7 A2A Agent 远端响应入口
入口形态：
- 远端 A2A agent 返回 `protocol.Message`
- 远端 A2A task history
- 远端 A2A `Task.Artifacts`
- streaming artifact update

标准表示：
- `agent/a2aagent` converter 将 remote A2A response 转成 `event.Event`
- response parts 可能进一步映射为 `model.Message` / `ContentParts`

链路：
```text
Remote A2A response
  -> agent/a2aagent converter
  -> event.Event / model.Message
      [存储点] session.Events
  -> telemetry/debuglog
      [外发点/存储点] trace/debug snapshot
  -> AG-UI translator（如前端展示）
      [回放点] UI stream / track（视事件类型）
```

主要存储点：
| 存储点 | 风险 | 原因 | 治理判断 |
|---|---|---|---|
| `session.Events` | 中-高 | 远端 artifacts/parts 可能包含 bytes 或 file refs | P1 |
| telemetry/debuglog | 中-高 | event/response snapshot 可能复制内容 | P1 |

链路终止点：
- session transcript。
- 前端展示。
- 观测/调试面。

治理重点：
- A2A 需要区分本框架 server 入站和本框架 agent 调远端后的响应。
- 远端返回 bytes 时也应视为第一手多模态来源。

### 4.8 OpenClaw Gateway 输入入口
入口形态：
- Telegram/HTTP 等渠道上传文件、图片、音频
- data URL
- URL 文件
- 业务 host ref

标准表示：
- OpenClaw uploads store 中的文件
- `host://` ref
- `model.ContentPart`

链路：
```text
OpenClaw inbound media
  -> gateway normalize
      [存储点] OpenClaw uploads（文件类）
  -> model.Message.ContentParts 或 host ref
  -> runner
      [存储点] session.Events
  -> debug recorder（启用时）
      [存储点] debug events / attachments
  -> model request
      [外发点] provider API
```

主要存储点：
| 存储点 | 风险 | 原因 | 治理判断 |
|---|---|---|---|
| OpenClaw uploads | 高 | 原始文件 bytes 落本地 state dir | P1，应用侧大对象存储 |
| `session.Events` | 高 | inline image/audio 仍可能进入 session | P1 |
| debug recorder | 高 | full mode 会保存附件和请求快照 | P1 |

链路终止点：
- OpenClaw uploads。
- session。
- provider API。
- debug trace。

治理重点：
- OpenClaw 已有独立文件承载面，应与 artifact/session 引用策略对齐。
- debug recorder 生产默认应安全化。

### 4.9 Tool Result 入口
入口形态：
- 工具返回文本
- 大 JSON
- 文件路径
- 未来多模态 tool result
- workspace artifact refs

标准表示：
- tool result message
- `model.Message{Role: tool}`
- `event.Event`

链路：
```text
Tool execution output
  -> FunctionCallResponseProcessor
  -> tool result message
  -> event.Event
      [存储点] session.Events
  -> next model request
      [外发点] provider API
  -> telemetry/debuglog
      [外发点/存储点] trace/debug snapshot
  -> evaluation recorder（启用时）
      [存储点] evalset/eval result
```

主要存储点：
| 存储点 | 风险 | 原因 | 治理判断 |
|---|---|---|---|
| `session.Events` | 中-高 | 大 tool result 或未来多模态 result 会进入 event | P1 |
| telemetry/debuglog | 高 | tool args/result/request snapshot 可能很大 | P1 |
| eval recorder | 中-高 | 可能录制 tool 中间结果 | P2 |

链路终止点：
- session transcript。
- next model request。
- 观测/调试/评测面。

治理重点：
- 多模态 tool result 应优先产出 artifact ref。
- 大文本 tool result 属于相邻的 context/offload/summary 问题，但如果是非文本 bytes，应纳入本规划。

### 4.10 MCP Tool Image Result 入口
入口形态：
- MCP tool result 中的 image content item
- image data base64
- mime type，例如 `image/png`、`image/jpeg`

标准表示：
- OpenClaw MCP image adapter 解码 base64
- `model.Message.AddImageData`
- `model.ContentPart.Image.Data`

链路：
```text
MCP tool image result
  -> extractMCPImages
  -> model.Message.AddImageData
  -> tool result messages / user-visible message
      [存储点] session.Events
  -> model request
      [外发点] provider API
  -> OpenClaw debug recorder（启用时）
      [存储点] debug events / attachments
```

主要存储点：
| 存储点 | 风险 | 原因 | 治理判断 |
|---|---|---|---|
| `session.Events` | 高 | MCP image 被转成 `Image.Data` 后进入消息链路 | P1 |
| debug recorder | 高 | OpenClaw full debug 可能保存结果图片 | P1 |
| telemetry/debuglog | 中-高 | 继承 event/request snapshot 风险 | P1 |

链路终止点：
- session transcript。
- provider API。
- OpenClaw debug trace。

治理重点：
- MCP image result 是明确的第一手多模态 bytes 来源。
- 虽然属于 tool result 范畴，但建议单独点名，避免被文本 tool result 掩盖。

### 4.11 CodeExecutor / Skill 输出入口
入口形态：
- workspace 输入文件
- code executor 输出文件
- skill 输出文件
- tool 保存的 artifact

标准表示：
- workspace 文件
- `artifact://` ref
- tool result / skill result message

链路：
```text
Workspace / CodeExecutor / Skill output
  -> workspace out file
      [临时态] workspace filesystem
  -> workspace_save_artifact / skill artifact save
      [存储点] artifact.Service
  -> tool result / skill result
      [存储点] session.Events（如果写入消息）
  -> next model request
      [外发点] provider API（通常通过引用或文本说明）
```

主要存储点：
| 存储点 | 风险 | 原因 | 治理判断 |
|---|---|---|---|
| workspace filesystem | 中 | 临时或 per-session 文件，可能保存大对象 | P2，生命周期治理 |
| artifact.Service | 中 | 目标承载层，保存 bytes | P0，定义契约 |
| `session.Events` | 中 | 如果 result 中内联文件内容，会进入 event | P1 |

链路终止点：
- workspace 临时目录。
- artifact。
- session/tool result。

治理重点：
- 鼓励输出文件进入 artifact。
- session 中只保留 artifact ref 和元信息。

### 4.12 Model / Provider Response 入口
入口形态：
- provider 返回文本
- provider 返回 tool call
- 未来可能返回文件、图片、音频或 file ref

标准表示：
- `model.Response`
- `model.Message`
- `event.Event`

链路：
```text
Provider response
  -> model.Response
  -> event.Event
      [存储点] session.Events
  -> telemetry/debuglog
      [外发点/存储点] trace/debug snapshot
  -> AG-UI translator（如果前端输出）
      [回放点] UI stream / track（视事件类型）
```

主要存储点：
| 存储点 | 风险 | 原因 | 治理判断 |
|---|---|---|---|
| `session.Events` | 中 | 当前主要文本，未来多模态响应风险升高 | P1/P2 |
| telemetry/debuglog | 中-高 | response snapshot 可能复制完整输出 | P1 |

链路终止点：
- session transcript。
- 前端 stream。
- 观测/调试面。

治理重点：
- 结构上应兼容未来 provider 多模态输出。
- 已是 provider file ref 的内容不强制重托管。

### 4.13 Graph State / Interrupt 入口
入口形态：
- graph state 中的 `messages`
- node output
- interrupt/resume payload
- pending writes

标准表示：
- `graph.State`
- checkpoint channel values
- `CheckpointTuple`

链路：
```text
Graph runtime state
  -> graph.State / channel values
  -> checkpoint saver
      [存储点] graph checkpoint
  -> resume / replay
      [回放点] graph execution restore
  -> session events（部分节点输出）
      [存储点] session.Events
```

主要存储点：
| 存储点 | 风险 | 原因 | 治理判断 |
|---|---|---|---|
| graph checkpoint | 中-高 | checkpoint JSON 可能保存完整 messages/state | P2 |
| `session.Events` | 中-高 | graph output 仍可能进入主事件 | P0/P1 |

链路终止点：
- graph checkpoint。
- graph resume。
- session transcript。

治理重点：
- checkpoint 不应无约束保存完整 inline bytes。
- 需要明确哪些 state 必须可恢复，哪些可以保存引用或摘要。

### 4.14 Evaluation Recorder 入口
入口形态：
- 录制当前 invocation
- 录制 user content、final response、intermediate responses、context messages

标准表示：
- eval case
- eval result
- `model.Message`

链路：
```text
Runner invocation / events
  -> evalset recorder
  -> EvalCase
      [存储点] evalset local/mysql
  -> EvalResult
      [存储点] eval result local/mysql
  -> evaluation report
      [存储点] benchmark/examples output
```

主要存储点：
| 存储点 | 风险 | 原因 | 治理判断 |
|---|---|---|---|
| evalset local/mysql | 高 | eval case 可保存完整 `model.Message` 和 ContentParts | P2 |
| eval result | 中-高 | 可能间接保存 actual messages/traces | P2 |
| benchmark output | 低-中 | 多数为文本/分数，取决于 case | P2 |

链路终止点：
- eval assets。
- reports。
- benchmark output。

治理重点：
- eval recorder 不能无治理地录制线上多模态 payload。
- evalset 更像长期资产，应比普通 debug 更谨慎。

## 5. 存储点汇总矩阵
| 存储点 | 被哪些入口触达 | 是否可能存 inline 多模态 | 存储性质 | 治理方向 | 优先级 |
|---|---|---|---|---|---|
| `session.Events` | 用户消息、seed history、rewriter、AG-UI、OpenAI-compatible、A2A、OpenClaw、tool result、MCP image、model response、graph output | 是 | 长期/半长期对话状态 | artifact ref + metadata + hydrate | P0 |
| `session.Tracks` | AG-UI input、AG-UI translator | 是 | 协议回放存储 | track payload 引用化，MessagesSnapshot 返回引用 | P0/P1 |
| `session.State` / `StateDelta` | graph、tool、skill、A2A state delta、业务扩展 | 可能 | 会话 KV 状态 | 大 bytes 约束，state 中只存 ref | P2 |
| `app/user state` | 业务扩展、memory/skill 游标 | 可能 | app/user KV 状态 | 文档约束和大小限制 | P2 |
| graph checkpoint | graph state、interrupt、pending writes | 可能 | 图执行恢复存储 | checkpoint 前引用化/摘要化 | P2 |
| telemetry / OTLP | user/model/tool request-response | 是 | 外部观测导出 | drop/omit/truncate/ref，默认策略收紧 | P1 |
| Langfuse | OTEL messages/observation | 是 | 外部观测平台 | leaf 截断，blob 禁止或引用化 | P1 |
| debuglog | request/response/event snapshot | 是 | 调试日志 | 默认关闭，snapshot 引用化或截断 | P1 |
| ExecutionTrace | message/response JSON snapshot | 可能 | 内存 + 可导出调试轨迹 | 仅摘要/引用，谨慎导出 | P1/P2 |
| evalset | evaluation recorder | 是 | 评测资产 | recorder 前剥离 bytes 或保存引用 | P2 |
| eval result / benchmark output | evaluation result | 可能 | 结果文件/DB | 不保存 raw message，保存 hash/ref | P2 |
| OpenClaw uploads | OpenClaw gateway | 是 | 应用侧文件库 | 生命周期、权限、与 session ref 对齐 | P1 |
| OpenClaw debug recorder | OpenClaw gateway/model/runner/MCP result | 是 | 调试文件/attachments | safe mode、短 retention、权限隔离 | P1 |
| workspace filesystem | codeexecutor/skill | 是 | 临时或 per-session 文件系统 | cleanup、产物转 artifact | P2 |
| artifact.Service | 多入口治理结果 | 是 | 目标大对象层 | 内容本体、版本、元信息、生命周期 | P0 |
| memory store | memory extractor/offload | 通常否，可能间接 | 文本 memory / external offload | extractor text-only，offload 契约 ref-only | P2 |
| pgvector/text index | session pgvector | event 列是，索引列通常否 | event JSON + 文本索引 | 治理 event 列，保持索引 text-only | P1/P2 |

## 6. 完整性检查
### 6.1 按入口覆盖
- 用户消息：已覆盖
- `RunOptions.Messages` / seed history：已覆盖
- `UserMessageRewriter`：已覆盖
- AG-UI input：已覆盖
- OpenAI-compatible API input：已覆盖
- A2A Server 入站：已覆盖
- A2A Agent 远端响应：已覆盖
- OpenClaw gateway input：已覆盖
- Tool result：已覆盖
- MCP tool image result：已覆盖
- CodeExecutor / Skill output：已覆盖
- Model / provider response：已覆盖
- Graph state / interrupt：已覆盖
- Evaluation recorder：已覆盖

### 6.2 按标准数据结构覆盖
- `model.Message`：已覆盖
- `model.ContentPart`：已覆盖
- `Image.Data` / `Audio.Data` / `File.Data`：已覆盖
- `Image.URL` 中的 data URL：已覆盖
- `model.Request` / `model.Response`：已覆盖
- `event.Event`：已覆盖
- `session.TrackEvent`：已覆盖
- `session.StateMap` / `StateDelta`：已覆盖
- `graph.State` / checkpoint：已覆盖
- `artifact.Artifact`：已覆盖
- eval case/result：已覆盖
- OpenClaw uploads/debug blobs：已覆盖

### 6.3 按存储性质覆盖
- DB / Redis / ClickHouse / MongoDB session backend：已覆盖
- 对象存储 / artifact backend：已覆盖
- 本地文件：OpenClaw uploads、debug recorder、evalset、workspace 已覆盖
- 内存缓存：inmemory session、AG-UI aggregator、workspace registry 作为边界项已覆盖
- 观测外发：OTLP、Langfuse 已覆盖
- 日志 / debug snapshot：已覆盖
- 前端回放：AG-UI MessagesSnapshot 已覆盖
- 评测资产：evalset/eval result/benchmark output 已覆盖

### 6.4 低风险或非第一手来源说明
- Dify / n8n 默认适配器主要消费已有 `invocation.Message`，默认不产生新的多模态 bytes；不作为独立第一层入口。
- telemetry、debuglog、checkpoint、evalset 是派生存储或外发点，不是第一手入口。
- memory extractor 是消费链路，不是多模态来源；但需要确保它不要把 base64 当作文本长期保存。
- workspace staging 通常是消费和临时承载，真正来源仍是 user/tool/skill/codeexecutor output。
- knowledge document readers / OCR 属于知识库文档处理，是否进入对话存储取决于上层使用方式；当前作为边界项，不列为主入口。
