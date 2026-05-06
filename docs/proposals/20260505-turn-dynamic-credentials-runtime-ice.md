# 20260505 TURN 动态凭据与运行时 ICE 配置改造方案

- Status: implemented
- Date: 2026-05-05

## 背景与问题定义

当前项目的 TURN 接入方式仍然是“构建期静态注入”：

- 前端在构建时通过 `VITE_MEETING_TURN_URLS`、`VITE_MEETING_TURN_USERNAME`、`VITE_MEETING_TURN_CREDENTIAL` 把 ICE / TURN 信息直接打进浏览器产物。
- coturn 默认按 `lt-cred-mech + user=meeting:固定密码` 工作。
- `Makefile` 发布链路会把同一份固定用户名 / 密码同时注入前端产物和 coturn 配置。

这套方案在内网演示或私有项目里勉强可用，但对当前仓库来说已经不合适，主要原因有三个：

1. **仓库是开源项目**
   - 真实 TURN 密码不能写进 `Makefile`、`env.example` 或任何仓库文件。

2. **即使不写进仓库，只在发布时通过环境变量传入，也仍然会泄露**
   - 当前前端运行时只读构建产物里的 `VITE_MEETING_TURN_*`。
   - 这意味着任何访问会议页面的人，都可以从浏览器里读出固定 TURN 密码。

3. **固定 TURN 密码不利于长期运维**
   - 密码一旦泄露，只能整体轮换。
   - 难以限制凭据有效期。
   - 难以区分不同会议、不同参会者的使用边界。

与此同时，最近线上排查已经暴露出另一条现实问题：

- 生产环境的 `coturn`、证书、前端构建注入、容器运行方式都存在多处耦合，一旦继续沿用静态密码模式，后续每一次发布都容易引入新的配置漂移。

因此，本轮需要把 TURN 从“固定密码 + 构建期注入”升级为“服务端共享密钥 + 运行时签发短期凭据”的模式。

## 目标与非目标

### 目标

- 不再把真实 TURN 密码写入仓库、`Makefile` 或前端构建产物。
- 将 coturn 切换为 `use-auth-secret` 动态凭据模式。
- 由后端在运行时为当前参会者签发短期 TURN 凭据。
- 前端在运行时从后端获取 `iceServers`，而不是只读 `VITE_MEETING_TURN_*`。
- 保持当前会议业务、信令转发、`WebRTC Mesh` 架构不变，只替换 TURN 凭据分发方式。
- 为后续 `Web`、`H5`、微信小程序等多端统一媒体接入方式打基础。

### 非目标

- 本轮不引入 `SFU` / `MCU`。
- 本轮不重构 `WebRTC Mesh` 协商流程。
- 本轮不改变会议权限模型。
- 本轮不处理所有 coturn 运维细节，例如防火墙模板、证书自动部署脚本、日志聚合等。
- 本轮不强制把本地开发环境也切成动态凭据；本地开发可保留静态 fallback，但生产发布链路必须去掉固定密码。

## 方案概述与核心决策

### 1. coturn 从固定密码模式切换到共享密钥模式

当前示例配置是：

```conf
lt-cred-mech
user=meeting:CHANGE_ME_STRONG_PASSWORD
```

本轮改成：

```conf
use-auth-secret
static-auth-secret=从服务端环境变量注入的共享密钥
realm=turn.meeting.07c2.com.cn
fingerprint
```

说明：

- `static-auth-secret` 只存在于服务端运行环境，不进仓库，不进前端。
- coturn 将根据客户端提交的 `username / credential` 动态校验 HMAC。
- 证书仍按现有 `turn.meeting.07c2.com.cn` 域名证书加载，不改变 TLS 部署边界。

### 2. TURN 凭据由后端运行时签发

后端负责根据共享密钥为当前参会者生成短期 TURN 凭据。

推荐的用户名格式：

```text
<expires_unix_timestamp>:<participant_id>
```

示例：

```text
1746451200:7f3a2c9f1e0ab6d4c5e2a7b8
```

密码生成方式：

- 算法：`HMAC-SHA1`
- key：`MEETING_TURN_SHARED_SECRET`
- message：完整 `username`
- 输出：`base64(HMAC-SHA1(...))`

这样做的原因：

- 与 coturn `use-auth-secret` 的常见接入方式一致。
- 前端不需要知道共享密钥。
- 每次签发的 `credential` 都与过期时间绑定，不再是长期固定密码。

### 3. ICE 配置改成运行时下发，而不是构建期写死

前端当前在 [web/src/runtime-config.ts](/Users/chenlei/Codes/www/github.com/misterchenleiya/meeting/web/src/runtime-config.ts) 中完全依赖构建期环境变量。

本轮改造后：

- 生产环境的 TURN 配置由后端接口运行时返回。
- 前端创建 `RTCPeerConnection` 之前，先拿到本次会议可用的 `iceServers`。
- 构建期的 `VITE_MEETING_TURN_*` 只保留为本地开发或测试 fallback，不再作为生产发布的主路径。

### 4. 运行时 ICE 配置通过独立接口下发

建议新增接口：

```text
POST /api/meetings/{meetingNumber}/participants/{participantID}/ice-servers
```

推荐返回结构：

```json
{
  "iceServers": [
    {
      "urls": [
        "stun:stun.l.google.com:19302"
      ]
    },
    {
      "urls": [
        "turn:turn.meeting.07c2.com.cn:3478?transport=udp",
        "turn:turn.meeting.07c2.com.cn:3478?transport=tcp",
        "turns:turn.meeting.07c2.com.cn:5349?transport=tcp"
      ],
      "username": "1746451200:7f3a2c9f1e0ab6d4c5e2a7b8",
      "credential": "<base64-hmac>"
    }
  ],
  "expiresAt": "2026-05-05T18:00:00Z"
}
```

建议这个接口只做一件事：

- 返回当前参会者可用的 `iceServers`

不把 TURN 动态凭据混入 `GET /api/meetings/{meetingNumber}` 快照中，原因是：

- 会议快照偏静态事实；
- TURN 凭据是短期、会过期的运行时信息；
- 把短期凭据混进会议快照，会让缓存语义和安全边界变得模糊。

### 5. 创建会议 / 加入会议响应中可附带首份 ICE 配置

为减少一次额外请求，可以考虑在以下响应中直接附带首份 `iceServers`：

- `POST /api/meetings`
- `POST /api/meetings/{meetingNumber}/join`

这样前端在首次进入会议时不需要额外再打一跳。

但为了兼容以下场景，仍然保留独立刷新接口：

- 页面刷新后恢复会议
- 本地状态已恢复，但之前拿到的 TURN 凭据已经过期
- 后续如果需要会中主动刷新凭据

本轮建议采用“双路径”：

1. `create / join` 响应里附带首份 `iceServers`
2. 独立 `ice-servers` 接口用于恢复和刷新

### 6. 前端不再把 TURN 凭据持久化到本地恢复状态

当前前端有本地恢复会议态。TURN 动态凭据不应直接跟随会议快照长期持久化，否则会引入两类问题：

- 已过期凭据被错误复用
- 本地存储里长期残留媒体接入凭据

因此本轮建议：

- `iceServers` 只保存在内存态
- 页面恢复会议时，如果需要重新创建 `PeerMesh`，先重新请求 `ice-servers`

### 7. 本阶段不做复杂的会中 TURN 凭据热刷新

如果把 TURN 凭据有效期设置得过短，就需要前端在会中定时刷新并对既有 `RTCPeerConnection` 执行 `setConfiguration` / `restartIce`，复杂度会明显上升。

为了把首轮改造收敛在可控范围内，本阶段建议：

- 默认 TTL 设为 12 小时
- 覆盖绝大多数单场会议时长
- 先不做中途自动刷新

这仍然比“固定密码永久有效”安全得多，同时不会把本轮改造成大范围的 `WebRTC` 生命周期重写。

后续如果产品出现超长时长会议需求，再补：

- 凭据接近过期提醒
- 后台刷新
- 对既有 peer 执行 `restartIce`

## 涉及模块 / 数据结构 / 接口 / 配置 / 存储影响

### 1. coturn 配置

- [deploy/coturn/turnserver.conf](/Users/chenlei/Codes/www/github.com/misterchenleiya/meeting/deploy/coturn/turnserver.conf)
  - 去掉 `lt-cred-mech`
  - 去掉 `user=meeting:...`
  - 改成 `use-auth-secret`
  - `static-auth-secret` 改为由环境变量或发布模板注入

### 2. 后端配置

- [internal/config/config.go](/Users/chenlei/Codes/www/github.com/misterchenleiya/meeting/internal/config/config.go)

建议新增：

- `MEETING_STUN_URLS`
- `MEETING_TURN_URLS`
- `MEETING_TURN_SHARED_SECRET`
- `MEETING_TURN_TTL_SECONDS`

建议默认值：

- `MEETING_STUN_URLS=stun:stun.l.google.com:19302`
- `MEETING_TURN_TTL_SECONDS=43200`

其中：

- `MEETING_TURN_SHARED_SECRET` 必须只存在于服务端环境变量
- 不进入仓库
- 不进入前端构建

### 3. 后端 HTTP API

- [internal/httpapi/server.go](/Users/chenlei/Codes/www/github.com/misterchenleiya/meeting/internal/httpapi/server.go)
  - 新增 `POST /api/meetings/{meetingNumber}/participants/{participantID}/ice-servers`
  - `POST /api/meetings`、`POST /api/meetings/{meetingNumber}/join` 的响应结构可扩展 `iceServers` 和 `iceCredentialExpiresAt`

建议新增一个专门的内部模块来做签发逻辑，例如：

- `internal/turnauth`
- 或 `internal/rtcconfig`

它负责：

- 解析 STUN / TURN URL 列表
- 根据共享密钥生成动态凭据
- 组装最终 `[]RTCIceServer` 等价结构

### 4. 会议域与鉴权边界

- [internal/meeting/service.go](/Users/chenlei/Codes/www/github.com/misterchenleiya/meeting/internal/meeting/service.go)
  - 当前 `participantID` 使用 12 字节随机值生成，最终是 24 位十六进制字符串，随机性足够高。

本轮建议的接口校验策略：

- 校验 `meetingID` 存在
- 校验 `participantID` 存在且仍在会议中
- 若请求方已登录且有 cookie，会额外校验当前用户与参会者是否匹配
- 对匿名参会者，先接受“`meetingID + participantID` 即可领取本参会者动态 TURN 凭据”的模式

原因：

- 当前匿名参会者本来就没有独立账号态
- `participantID` 已经是高随机值
- 这是在现有架构下代价最低的收口方式

后续如果需要更强隔离，再引入单独的参会者会话 token。

### 5. 前端运行时媒体配置

- [web/src/runtime-config.ts](/Users/chenlei/Codes/www/github.com/misterchenleiya/meeting/web/src/runtime-config.ts)
  - `resolveIceServers()` 不再作为生产主路径
  - 改成“本地 fallback / 调试 fallback”

- [web/src/rtc.ts](/Users/chenlei/Codes/www/github.com/misterchenleiya/meeting/web/src/rtc.ts)
  - `PeerMesh` 不再依赖固定的 `defaultRTCConfiguration`
  - 改成由上层在构造时注入运行时 `RTCConfiguration`

- [web/src/App.tsx](/Users/chenlei/Codes/www/github.com/misterchenleiya/meeting/web/src/App.tsx)
  - 在创建会议、加入会议、恢复会议、重新连信令前，确保已有当前可用的 `iceServers`
  - 不把动态凭据长时间写入本地持久化状态

### 6. 发布链路

- `Makefile`
  - 生产发布流程不再把真实 `TURN_CREDENTIAL` 注入前端构建产物
  - `TURN_HOST` / `TURN_USERNAME` / `TURN_CREDENTIAL` 这套静态注入链路应从生产路径中降级或移除

- `docs/deploy/coturn.md`
  - 需要从“固定用户名 / 密码 + 前端构建注入”改写为“共享密钥 + 运行时签发”

## 兼容性、迁移方案与风险

### 兼容性

- 会议业务接口主体不变
- WebSocket 信令协议不变
- `WebRTC Mesh` 模型不变
- 浏览器端仍然只消费标准 `RTCIceServer[]`

### 迁移策略

建议按下面顺序落地：

1. 后端先支持运行时 `ice-servers` 接口，但前端暂不切换
2. 前端支持优先使用运行时 `iceServers`，保留构建期 fallback
3. 生产环境 coturn 切到 `use-auth-secret`
4. 生产发布链路停止注入 `VITE_MEETING_TURN_CREDENTIAL`
5. 验证通过后，逐步废弃静态 TURN 凭据文档和默认值

这样做的好处是：

- 不需要前后端、coturn 三方同一时刻硬切
- 可以先在测试环境双栈兼容
- 即使某一轮回滚，也不会把系统整体打断

### 风险

1. **前端恢复会议流程会变复杂**
   - 现在恢复态只恢复会议信息。
   - 改造后需要在恢复时再打一跳 `ice-servers` 接口。

2. **匿名参会者的接口校验边界仍然偏轻**
   - 本轮依赖高随机 `participantID` 收口。
   - 若后续安全要求提高，需追加匿名参会 token。

3. **TTL 过短会放大会中刷新复杂度**
   - 本轮通过把 TTL 设为 12 小时来规避。

4. **服务端时间漂移会影响动态凭据**
   - 应确保应用服务器与 coturn 所在宿主机都启用 NTP 同步。

5. **生产环境若仍保留旧静态变量，容易出现双配置漂移**
   - 必须在发布文档里明确“生产以运行时接口为准”。

## 验证与回滚思路

### 验证

#### 配置层验证

- coturn 实际配置中应出现：

```conf
use-auth-secret
static-auth-secret=***
```

- 不再出现：

```conf
user=meeting:固定密码
```

#### 后端验证

- `ice-servers` 接口能返回：
  - STUN 列表
  - TURN URL 列表
  - 动态 `username`
  - 动态 `credential`
  - `expiresAt`

- 同一参会者在不同时间请求，`username / credential` 应随过期时间变化

#### 前端验证

- 生产打包产物中不再出现：
  - `CHANGE_ME_STRONG_PASSWORD`
  - 真实 `TURN_CREDENTIAL`

- 浏览器实际建连时能拿到 `relay` candidate
- `H5` 与桌面端在复杂网络下能互相看到远端流

#### 线上验证

- `turns:turn.meeting.07c2.com.cn:5349` 能完成 TLS 握手
- coturn 日志中不再出现“固定用户名 / 密码认证”的相关排查噪音

### 回滚

若动态凭据模式上线后出现问题，回滚顺序建议如下：

1. 前端先退回到静态 fallback 版本
2. 后端保留 `ice-servers` 接口但暂不使用
3. coturn 再退回 `lt-cred-mech + user=...`

注意：

- 回滚只适合作为短期止血手段
- 固定密码模式不应长期保留在开源公网项目中

## 备选方案与放弃原因

### 方案 A：继续沿用固定 TURN 用户名 / 密码

放弃原因：

- 真实密码无法安全地存在开源仓库
- 即使不在仓库里，仍会泄露到前端 bundle
- 运维上只能整体轮换，安全边界差

### 方案 B：把真实 TURN 密码放进 `Makefile` 或 `.env`

放弃原因：

- 仓库泄密风险直接不可接受
- 不符合当前项目“敏感信息只保留在本地环境”的基本约束

### 方案 C：只改 coturn，不改前后端

放弃原因：

- `use-auth-secret` 必须有上游来生成动态凭据
- 只改 coturn，前端仍拿不到合法的 `username / credential`

### 方案 D：立即做会中自动刷新与 `restartIce`

放弃原因：

- 复杂度明显超出本轮目标
- 当前优先级是先去掉固定密码暴露问题

## 落地后需要同步更新的文档

- [docs/deploy/coturn.md](/Users/chenlei/Codes/www/github.com/misterchenleiya/meeting/docs/deploy/coturn.md)
- `env.example`
- `meeting-backend.env` 示例
- 前端运行配置说明
- 生产发布说明

## 相关链接

- [web/src/runtime-config.ts](/Users/chenlei/Codes/www/github.com/misterchenleiya/meeting/web/src/runtime-config.ts)
- [web/src/rtc.ts](/Users/chenlei/Codes/www/github.com/misterchenleiya/meeting/web/src/rtc.ts)
- [internal/httpapi/server.go](/Users/chenlei/Codes/www/github.com/misterchenleiya/meeting/internal/httpapi/server.go)
- [internal/config/config.go](/Users/chenlei/Codes/www/github.com/misterchenleiya/meeting/internal/config/config.go)
- [internal/meeting/service.go](/Users/chenlei/Codes/www/github.com/misterchenleiya/meeting/internal/meeting/service.go)
- [deploy/coturn/turnserver.conf](/Users/chenlei/Codes/www/github.com/misterchenleiya/meeting/deploy/coturn/turnserver.conf)
- [docs/deploy/coturn.md](/Users/chenlei/Codes/www/github.com/misterchenleiya/meeting/docs/deploy/coturn.md)
