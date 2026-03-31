# Meeting

[English](README.md) | 简体中文

`Meeting` 是一个面向 PC 浏览器和移动 H5 的多人视频会议系统，支持“`P2P` 优先、`TURN` 兜底、服务端仅保留基础审计数据”。

当前能力包括视频会议、白板、共享屏幕、录屏/录音、文字聊天、就位确认、会议纪要、主持人/助理/参会者权限体系、匿名参会者昵称、会后删除会议过程数据，以及基于浏览器可落地实现的 `WSS + WebRTC` 通信模型。

![Meeting 登录界面预览](docs/meeting_login.png)

## 架构概览

- 媒体面：`WebRTC`
- 控制面与信令：`WSS`
- 后端：`Golang`
- 数据库：`SQLite3`
- 前端：`TypeScript + React + Vite`
- 数据清理策略：会议运行态只保留在内存，会议结束后立即删除
- 审计策略：仅持久化基础审计事件和注册用户默认媒体偏好

## 核心约束

- `participant` 默认只有文字聊天权限
- `participant` 开启麦克风、摄像头、白板、共享屏幕、录制、就位确认都需要主持人授权
- 主持人可将参会者设为助理，助理拥有已授予的主持权限
- 录屏/录音默认本地录制，不上传服务器
- 临时聊天记录、白板、就位确认结果、临时会议纪要仅保留到会议结束
- 主持人在仍有其他参会者时不能直接离会，必须显式结束会议，避免房间进入无主持人状态

## 模块划分

下表按开源项目常见的“路径 / 职责 / 当前状态”格式整理了当前仓库的模块职责。

| 模块 | 路径 | 职责 | 当前实现情况 |
| --- | --- | --- | --- |
| 服务入口 | `cmd/server` | 组装配置、日志、存储、会议服务、HTTP API 和信令 Hub | 已实现，支持本地启动 |
| 架构决策 | `docs/adr` | 记录项目的重要架构决策、约束和取舍 | 已实现，包含初版架构 ADR |
| 设计资产 | `docs/design` | 存放可复用的 HTML/CSS 设计稿和渲染脚本 | 已实现 |
| 配置模块 | `internal/config` | 加载服务地址、SQLite 路径、日志目录等运行配置 | 已实现 |
| 日志模块 | `internal/logging` | 初始化 JSON 日志、按天轮转和保留策略 | 已实现 |
| 存储模块 | `internal/storage/sqlite` | 持久化审计事件和注册用户默认媒体偏好 | 已实现，未承载会议运行态 |
| 会议领域模块 | `internal/meeting` | 房间、参与者、权限、白板、临时聊天、就位确认、纪要等运行态管理 | 已实现核心能力，仍待补完整登录体系和多人 Mesh 优化 |
| HTTP API 模块 | `internal/httpapi` | 提供创建/加入/离会/结束会议、昵称修改、纪要查询、审计上报等接口 | 已实现基础 API |
| 信令模块 | `internal/signaling` | WebSocket 会话管理、房间广播、能力申请/授权、SDP/ICE 转发、协作事件广播 | 已实现 |
| 前端 API 层 | `web/src/api.ts` | 封装 REST 请求 | 已实现 |
| 前端信令层 | `web/src/signaling.ts` | 封装 WebSocket 连接和消息收发 | 已实现 |
| 前端 RTC 层 | `web/src/rtc.ts` | 管理 `RTCPeerConnection`、轨道同步和基础统计采集 | 已实现 1v1 主链路，待继续做多人 Mesh 稳定化 |
| 前端录制层 | `web/src/recording.ts` | 本地录制缓存、下载保存、丢弃缓存 | 已实现 |
| 前端白板模块 | `web/src/whiteboard.tsx` | 白板绘制与显示 | 已实现 |
| 前端会议控制台 | `web/src/App.tsx` | 产品化登录壳层、入会流程、主舞台、右侧抽屉和会中辅助协作面板 | 已实现 |

## 需求实现状态

### 已实现

- [x] 会议创建、加入、离开、主持人结束会议
- [x] 主持人 / 助理 / 参会者基础角色模型
- [x] `participant` 默认仅聊天，其他权限需主持人授权
- [x] 1v1 `WebRTC` P2P 建链
- [x] 本地媒体采集、本地/远端视频预览
- [x] 共享屏幕
- [x] 本地录屏/录音缓存、下载、丢弃
- [x] 文字聊天
- [x] 白板协作
- [x] 就位确认
- [x] 临时会议纪要运行态
- [x] 临时会议纪要本地导出
- [x] 基础审计统计上报
- [x] 匿名/注册参会者昵称输入
- [x] 昵称修改并写入聊天留痕
- [x] 公开 `9` 位数字会议号、会议号复制和会中分享二维码
- [x] 会议结束后清理运行态数据

### 部分实现

- [~] 多人视频会议
  当前已具备 1v1 主链路和基础 Mesh 结构，仍需继续做多人 Mesh 稳定性、弱网和退化策略优化。
- [~] 产品化登录与预定会议流程
  当前前端已经落地黑色风格登录壳层、快速会议、预定会议表单和带密码弹窗的加入会议流程，但真实登录、验证码和真正的预定会议持久化仍未实现。
- [~] 会议纪要
  当前支持会中临时纪要、聊天记录、白板数量和就位确认摘要导出；“会议结束时提示主持人保存纪要”尚未补齐。
- [~] 审计日志
  当前已上报延迟、丢包、帧率、码率和连接摘要；仍可继续丰富设备指纹和更细粒度网络信息。

### 未实现

- [ ] `TURN` / coturn 部署与穿透失败自动退化链路的生产验证
- [ ] 多人 Mesh 的动态管理和性能优化
- [ ] 微信注册、扫码登录、邮箱验证码登录
- [ ] 打开邀请链接后自动回填会议号与密码
- [ ] 会议结束时主持人的纪要保存提示流

## 当前 UI 流程

- 入会前的登录页已经切换为全屏单卡布局：顶部是大号 `meeting` 字标，下方是聚焦光斑，再往下是居中的登录卡片，整体与会中页保持同一套 macOS dark 风格。
- 登录流程已经拆成两个独立入口：`注册` 和 `登录`。注册时先填邮箱、昵称和验证码，验证成功后会自动返回登录页；登录时使用邮箱验证码完成登录，成功后进入登录后的入口卡片。开发模式下验证码会自动回填，便于本地联调。
- 主持人流程：先登录，再回到黑色产品壳层中选择快速会议或预定会议；其中预定会议表单当前仍复用现有创建会议接口，提交后会立即进入会议。
- 加入会议流程：先输入公开 `9` 位会议号并做预检，只有会议需要密码时才会弹出密码悬浮窗继续加入；带空格的 `3-3-3` 会议号也会自动规范化。
- 会中流程：会议房间已经切换为单屏全舞台布局，顶部是标题栏，底部是 dock 工具栏，主持人工具 / 会议工具 / 设置 / 应用 / 结束会议通过贴附式子窗口展开，成员和聊天默认收纳到右侧抽屉。无人开视频 / 共享时显示头像墙；存在活动媒体时切为主画面 + 右侧缩略窗。
- 分享窗口会显示公开 `9` 位会议号、分享二维码和复制入口；会议号按 `3-3-3` 形式分组显示，内部 room id 不再直接暴露给用户。
- 白板、就位确认、临时纪要、审计摘要和权限管理仍然保留，通过菜单、抽屉和浮动窗口围绕主舞台提供。

## API 与运行态说明

当前已实现的关键接口包括：

- `POST /api/meetings`：创建会议
- `GET /api/meetings/{meetingID}`：获取会议快照
- `GET /api/meetings/{meetingID}/minutes`：获取会中临时纪要快照
- `POST /api/meetings/{meetingID}/join`：加入会议
- `POST /api/meetings/{meetingID}/participants/{participantID}/leave`：离开会议
- `POST /api/meetings/{meetingID}/participants/{participantID}/nickname`：修改昵称
- `POST /api/meetings/{meetingID}/participants/{participantID}/capabilities/{capability}/grant`：主持人授权
- `POST /api/meetings/{meetingID}/participants/{participantID}/audit`：上报基础审计数据
- `POST /api/meetings/{meetingID}/end`：主持人结束会议
- `PUT /api/users/{userID}/preferences`：保存注册用户默认媒体偏好
- `GET /ws/meetings/{meetingID}`：WebSocket 信令入口

更完整的接口契约文档见 [docs/api/README.md](docs/api/README.md)。

说明：

- `POST /api/auth/register/code`、`POST /api/auth/register/verify`、`POST /api/auth/login/code`、`POST /api/auth/login/verify`、`GET /api/auth/me`、`POST /api/auth/logout`：注册 / 登录 / 当前用户 / 退出登录接口
- `POST /api/meetings` 返回的会议对象现在同时包含内部 `id` 和公开 `meetingNumber`。
- `GET /api/meetings/{meetingID}` 与 `POST /api/meetings/{meetingID}/join` 等会议级接口现在同时接受内部运行态 ID 和公开 `9` 位会议号。
- `GET /ws/meetings/{meetingID}` 仍继续使用内部运行态 ID，以减少对现有信令链路的影响。

## 本地运行

### 后端

```bash
go run ./cmd/server
```

可选环境变量：

- `MEETING_HTTP_ADDR`，默认 `:5180`
- `MEETING_SQLITE_PATH`，默认 `./data/meeting.db`
- `MEETING_LOG_DIR`，默认 `./logs`

### 前端

```bash
cd web
npm install
npm run dev
```

前端开发服务器默认监听 `0.0.0.0:5188`。

### 使用 Makefile

```bash
make build
make run-backend
make run-frontend
make clean
```

说明：

- `make build`：构建后端二进制和前端静态资源，产物写入 `build/`
- 后端构建输出：`build/backend/meeting`
- 前端构建输出：`build/frontend/`
- `make run-backend`：启动后端服务，并将运行期日志和 SQLite 数据写入 `build/run/`
- `make run-frontend`：启动前端开发服务器
- 前端运行期日志默认输出到浏览器控制台；`warn`/`error` 和关键 `info` 事件会批量上报到后端 `POST /api/client-logs`，并进入后端 JSON 日志；浏览器本地不再持久化保存这些日志
- `make clean`：删除 `build/` 目录

## 验证命令

```bash
go test ./...
go build ./cmd/server
cd web && npm run build
make build
```

## 数据生命周期

- 房间、参会人员、权限状态、临时聊天记录、白板、就位确认、临时会议纪要：仅运行时内存态
- 会议结束后：立即从内存清理
- 服务器持久化：仅审计事件和注册用户默认媒体偏好

## 许可协议

本项目采用 MIT 许可开源，详见 [LICENSE](LICENSE)。

## 设计资源

- 架构决策：`docs/adr/ADR-0001-20260325-meeting-architecture.md`
- Issue 清单：`docs/issues/README.md`
- TURN 部署说明：`docs/deploy/coturn.md`
- 前端设计资产：`docs/design/`
- UI 落地记录：`docs/design/20260325-product-ui-rollout.md`
