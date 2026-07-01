# 测试方案与当前结论：Session 多模态外存最小闭环

## 1. 文档定位
本文是需求包 A 的端到端测试方案与当前执行结论，依据 `req-scope.md` 的验收口径制定，并参考 `tech-design.md` 与 `core-issues.md` 的实现边界。

视角与分工：
- 本方案聚焦端到端测试：通过 runner / `session.Service` 公开契约与真实 backend，验证可观察行为。
- 代码层校验（未导出 helper、内部 copy-on-write、data URL 解析、命名/元信息字段、artifact 清理细节等）由研发环节自行掌控，不在本方案内。
- 范围限定：只覆盖需求包 A；B/C/F 等其他治理面不在本方案内。
- 本文记录测试设计、测试落点与当前执行结果，不承载实现设计细节。

## 2. 被测对象与契约
端到端契约（对外可观察行为）：
- 装配入口：
    - `session/externalization.Wrap(inner, artifactService, externalization.Config{Enabled: true})`。
    - wrapped service 通过 `runner.WithSessionService` 注入 runner；直接 session 写入也应使用同一份 wrapped service。
- 写入治理：
    - 开启后，标准 `ContentParts` 的 inline 多模态在落入 session backend 前被外存为引用。
- 读取还原：
    - `GetSession` 默认 hydrate，把引用还原为标准 `ContentParts`。
- 引用表达：
    - persisted 仅保留 `model.ContentPart.ContentRef`，不污染 provider 字段。

不在被测范围：
- tool result JSON 内部 inline blob（需求包 C）。
- AG-UI track 独立 payload（需求包 B）。
- telemetry/debuglog/checkpoint/evalset 复制面（后续需求包）。
- `StateMap` / `StateDelta` 大对象（需求包 F 或业务约束）。

## 3. 测试目标
对齐 `req-scope.md` 第 2 节四个核心目标：
- 写入减重：
    - 开启治理后，标准 `ContentParts` 的 inline bytes/data URL 不再落入 session backend。
- 运行不变：
    - 当前轮 runtime event 与模型调用不受 persisted 减重影响。
- 读取兼容：
    - 历史 inline、新引用化、混合 session 都能读取并继续对话。
- 资损可控：
    - artifact 不可用等故障场景显式失败，不产生损坏写入或静默丢内容。

## 4. 测试分层与方法
端到端分层：
- 主链路端到端：
    - 通过 `session/externalization.Wrap` 装配 + fake model，验证写入减重、默认 hydrate、当前轮不变、继续对话。
- 跨后端一致性：
    - 同一组行为在多个 session backend 上重放，验证行为一致。
- 写入面回归：
    - 覆盖各写入入口与“治理关闭”默认行为不变。
- 历史兼容：
    - 历史 inline、新引用化、混合 session 的读取与继续对话。
- 故障与边界：
    - artifact 不可用、hydrate 失败等场景的可观察失败语义。

测试原则：
> 用例围绕验收口径，断言可观察结果，不绑定内部实现细节。
- 每条验收口径至少一条用例。
- 故障用例必须断言“无损坏写入、不静默丢内容”。
- 断言聚焦行为结果（persisted 形态、hydrated 形态、错误返回、provider 收到的内容）。

## 5. 测试环境与后端矩阵
通用依赖：
- 无需外部 API key；模型侧使用记录入参的 fake model（不接真实 provider）。
- artifact 承载层：`artifact/inmemory`。
- 故障注入：在 artifact / session backend 边界注入错误（不接管内部实现）。

后端矩阵（决策：inmemory、redis、mongodb）：
| 后端 | 测试载体 | 说明 |
|---|---|---|
| inmemory | 进程内 `session/inmemory` | 基准后端，确定性最强 |
| redis | `session/redis` + `miniredis`（`WithRedisClientURL`） | 进程内假实例，无外部依赖 |
| mongodb | `session/mongodb` + 真实实例（`WithMongoClientURI`） | 决策采用真实实例；需具备可用 MongoDB 才执行 |

> mongodb 用真实实例：通过 `MONGO_TEST_URI` 注入连接串；连接串不写入代码、文档或 git diff。

## 6. 验收口径到端到端用例映射
### 6.1 写入（对应 req-scope 6.1）
| 编号 | 验收点 | 用例要点 | 期望 |
|---|---|---|---|
| W1 | 标准 `ContentParts` inline 不落库 | 开启治理写入含 `Image.Data` 的 event，读取底层 backend | persisted part 的 inline bytes 为空、`ContentRef` 非空 |
| W2 | data URL 视为 inline | 写入 `Image.URL`/`File.URL` 为 data URL 的 event | data URL 被清空并外存 |
| W3 | 治理关闭保持 inline | 未配置 option | persisted 保留原 inline，无 `ContentRef` |
| W4 | 外部引用不重托管 | 普通 URL、provider file id、已有 `artifact://` | 不外存、不新增 artifact、不改写引用 |
| W5 | 多 backend 行为一致 | 同组用例在后端矩阵重放 | 外存与读取行为一致 |
| W6 | 小对象同样外存 | 极小 inline bytes | 同样被外存（首期无阈值） |
| W7 | 不污染 provider 字段 | 写入后读取底层 backend | `artifact://` 不写入 provider `URL`/`FileID` |

### 6.2 运行时（对应 req-scope 6.2）
| 编号 | 验收点 | 用例要点 | 期望 |
|---|---|---|---|
| R1 | runtime event 不被修改 | 写入后检查调用方持有的原始 `evt` | 原 event inline bytes 不变 |
| R2 | 当前轮模型调用不受影响 | runner + fake model，当前轮 | fake model 收到原始内容，行为不变 |

### 6.3 读取与恢复（对应 req-scope 6.3）
| 编号 | 验收点 | 用例要点 | 期望 |
|---|---|---|---|
| H1 | 新引用化 session 可读取 | 外存后 `GetSession` | hydrated part 还原原始 bytes |
| H2 | hydrate 不回写 persisted | hydrate 后再读底层 backend | persisted part 仍无 inline bytes |
| H3 | 历史 inline session 可读取 | 直接以 inline event 落库后经装饰器读取 | 原样返回，可继续对话 |
| H4 | 混合 session 可读取 | 同一 session 含 inline event 与 ref event | 两类 event 都正确返回 |
| H5 | hydrate 失败显式报错 | artifact load 失败或 ref 不可解析 | `GetSession` 返回明确错误，不返回空内容 session |

### 6.4 故障与边界（对应 req-scope 6.4）
| 编号 | 验收点 | 用例要点 | 期望 |
|---|---|---|---|
| F1 | artifact service 未配置 | 开启治理但未提供 artifact service | 写入报错，不追加 event |
| F2 | artifact 后端不可用 | save 注入错误 | 报错，不写入“已清空 bytes 但 ref 不可用”的 event |
| F3 | GetSession 失败面变化 | artifact 后端故障下读取 | `GetSession` 返回错误（已确认接受，作为期望行为） |

## 7. 端到端主链路用例
装配方式：
- 通过 `session/externalization.Wrap` 包装 session service 开启治理。
- 通过 `runner.WithSessionService` 注入 wrapped service，并通过 `runner.WithArtifactService` 注入同一 artifact service。
- 使用记录入参的 fake model，不接真实 provider。

用例：
- 开启治理：
    - 新写入 session 不保存被治理的大块 inline bytes（W1/W6）。
    - `GetSession` 默认 hydrate，业务可见行为接近历史 inline session（H1）。
    - 当前轮模型调用收到原始内容，不受 persisted view 影响（R2）。
    - 新引用化 session 可继续对话：后续轮 fake model 通过 hydrate 后历史拿到还原内容。
- 默认关闭：
    - 未包装 session service，或 `externalization.Config{Enabled:false}` 时行为与既有版本一致，不产生 artifact（W3）。

## 8. provider 边界负向用例（纳入 A 包）
目标：
> 引用绝不能被当作真实内容外发给 provider。
- 构造携带 unresolved `ContentRef`（无 inline bytes）的历史 session，经正常模型请求链路触发一轮对话。
- 断言 fake model / provider 适配层收到的请求中：
    - 不包含 `artifact://` 字面量作为内容。
    - 内容要么已被还原为真实 bytes，要么链路显式报错。
- 该用例锁定“引用不外泄”这条底线，独立于具体在何处完成还原。

## 9. 跨后端一致性测试
- 用表驱动在后端矩阵（inmemory、redis、mongodb 真实实例）重放核心用例：
    - 写入减重（W1）、读取还原（H1）、hydrate 不回写（H2）、混合 session（H4）。
- 断言各后端外存与读取行为一致（W5）。

## 10. 写入面回归用例
覆盖 `tech-design.md` 第 8.2 的写入面：
- runner 当前轮用户消息写入。
- seed history 写入。
- message rewriter 输出写入。
- assistant / provider response event 写入。
- 业务直接调用 `session.Service.AppendEvent` 写入。

回归断言：
- 治理开启时各写入面都经过统一治理，无绕过分支。
- 治理关闭时各写入面默认行为不变。

## 11. 历史兼容与混合数据
- 历史 inline session：以 inline event 落库后经装饰器读取与继续对话。
- 新引用化 session：经治理写入后读取、继续对话。
- 混合 session：同一 session 含 inline event、ref event、治理关闭期写入的 inline event；读取、继续对话、基础回放均兼容。

## 12. 不在本测试范围内
- 代码层/白盒校验（未导出 helper、copy-on-write、data URL 解析、命名与元信息字段、artifact 清理细节等），由研发环节掌控。
- 阈值治理（小对象保留策略）。
- without-hydrate / lazy hydrate 读取入口。
- 完整 artifact GC、权限、审计、加密、脱敏。
- 历史数据批量迁移工具。
- provider 文件上传优化与 provider file id 缓存。
- 业务自定义 JSON / `StateMap` 深扫。

## 13. 当前测试落点
已新增端到端测试：
- 测试模块：`test/`。
- 测试文件：`test/session_multimodal_e2e_test.go`。
- 依赖接线：`test/go.mod` / `test/go.sum` 引入 `session/redis`、`session/mongodb`、`miniredis`，并通过本地 `replace` 指向仓库内子模块。

测试夹具：
- session backend：`session/inmemory`、`session/redis` + `miniredis`、`session/mongodb` + 真实实例。
- artifact：`artifact/inmemory`；故障场景使用测试包装器注入 save/load 错误。
- model：记录入参的 fake model，不接真实 provider。

## 14. 当前执行结论
结论：
- `inmemory` 后端：通过。
- `redis` 后端：通过。
- `mongodb` 真实实例：通过。
- 新增 `TestSessionMultimodal*`：通过。
- `test` 模块完整测试：通过。
- 新增测试 race 检查：通过。

已执行命令：
- `cd test && GOPROXY=https://proxy.golang.org,direct go test ./... -run 'TestSessionMultimodal'`
- `cd test && GOPROXY=https://proxy.golang.org,direct go test ./...`
- `cd test && GOPROXY=https://proxy.golang.org,direct go test -race ./... -run 'TestSessionMultimodal'`
- `cd test && MONGO_TEST_URI=... GOPROXY=https://proxy.golang.org,direct go test ./... -run 'TestSessionMultimodal' -count=1`

覆盖到的端到端行为：
- 开启治理后，标准 `ContentParts` inline 多模态写入 session backend 前被外存为 `ContentRef`。
- `GetSession` 默认 hydrate，可还原为模型调用可见的原始 bytes。
- 当前轮模型调用不受 persisted view 影响。
- 新引用化 session 可继续对话。
- 治理关闭时保持历史 inline 行为。
- provider 边界负向用例通过：unresolved `ContentRef` 不会作为 `artifact://` 内容进入 fake model；链路显式失败。
- artifact save/load 故障可见失败，不静默写入损坏数据。

未在本轮端到端覆盖的内容：
- 代码层/白盒细节（如内部 helper、精确元信息字段、artifact 清理细节），按分工由研发环节掌控。
- 后续需求包中的复制面治理（tool result JSON、AG-UI track、telemetry/debuglog/checkpoint/evalset、`StateMap` 深扫等）。
