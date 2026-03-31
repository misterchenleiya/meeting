# 20260331 媒体可靠性与身份体系实施计划

- Status: accepted
- Date: 2026-03-31

## 背景与问题定义

当前 `Meeting` 已经具备可演示的主链路：会议创建 / 加入 / 结束、1v1 WebRTC P2P、白板、聊天、就位确认、临时纪要和基础审计都已经实现。但如果目标是继续往“稳定可用的产品级会议系统”推进，当前仍有两条主线没有补齐：

1. **媒体可靠性不足**
   - `web/src/rtc.ts` 目前只配置了公共 STUN，没有 TURN/coturn 兜底。
   - 多人 Mesh 还处在基础框架阶段，缺少带宽自适应、断线恢复和弱网治理。
   - 当前只能证明“能连上”，还不能证明“在复杂网络里也能稳定连上”。

2. **身份体系仍是测试态**
   - `web/src/App.tsx` 已经拆分出独立的注册 / 登录入口，并支持验证码式的注册后回登录与登录后进入主页；但真实邮件投递、风控和生产级会话策略仍未接入。
   - `internal/storage/sqlite/schema.sql` 已经预留了 `users`、`password_hash`、`wechat_openid`、`user_preferences`，并补上了验证码、会话等配套存储，但还没有真实邮件通道。
   - 主持人创建会议仍然依赖前端传入的 `hostUserId`，缺少真实注册准入与后端鉴权边界。

本计划的目标，是先把这两条线拆开做稳，再合流到一条可持续演进的主干上。

## 目标与非目标

### 目标

- 让会议媒体链路在 NAT、弱网和多 peer 场景下可稳定工作。
- 让用户可以自助注册，并用邮箱验证码完成登录。
- 让主持人创建会议必须绑定真实身份，避免前端伪造身份。
- 保留匿名参会能力，不强制所有 join 流量都变成实名登录。
- 在不接入真实邮件服务的前提下，先把身份模块的接口、数据模型和前端状态机搭起来。

### 非目标

- 暂不处理生产部署、域名反代、HTTPS/WSS 证书和多环境运维。
- 暂不接入真实邮件发送通道；本阶段只做可替换的 `Mailer` 接口和开发态 stub。
- 暂不做微信登录、扫码登录和完整密码登录。
- 暂不把会议运行态从内存直接迁移到外部存储。

## 本轮执行顺序

本轮开发按下面的优先级推进，便于把双人会议可用性尽快拉起来，同时避免在身份和安全收口上返工：

- `1. 媒体可靠性`
   - 先把 `TURN / coturn` 真正落地，并把直连失败时的退化路径补全。
   - 同步收紧多 peer Mesh 的协商、恢复和质量治理。

- `2. 最小身份闭环`
   - 再补注册、邮箱验证码登录、会话和主持人准入。
   - 先保证“谁能创建会议、谁能登录”这条边界清晰可控。

- `4. 生产级安全收口`
   - 最后补 `Origin` 白名单、认证边界、接口访问控制和生产暴露前的安全收尾。
   - 这部分会和前两项联动，但不抢在主链路之前先做。

说明：

- 这里按业务优先级直接使用 `1 -> 2 -> 4` 的顺序推进，不再按文档章节编号理解。
- `Phase 0` 的方案冻结与 `Phase 3` 的联调验收仍然保留，但它们属于贯穿式工作，不作为本轮主开发顺序的主轴。

## 方案概述与核心决策

### A. 媒体可靠性优先补齐

1. **把 ICE 配置从硬编码改为可配置**
   - 当前 `web/src/rtc.ts` 只写了 `stun:stun.l.google.com:19302`。
   - 需要支持环境变量或服务端下发的 `iceServers` 配置。
   - 为后续接入私有 STUN / TURN 留出入口。

2. **接入 TURN / coturn 兜底**
   - 直连可用时继续优先 P2P。
   - 直连失败、NAT 严格或 UDP 不可达时，自动退化到 TURN relay。
   - TURN 不应由会议业务后端承担，而应作为独立网络基础设施。

3. **把现有统计能力真正用于连接治理**
   - 复用 `RTCPeerConnection.getStats()` 采集的 RTT、丢包、帧率、码率和 candidate 类型。
   - 根据网络状态调整视频和屏幕共享的码率、分辨率、优先级。
   - 在连接失败、重协商失败和 ICE 状态变化时触发恢复流程。

4. **把多人 Mesh 从“能连”推进到“能用”**
   - 目前的基础是 full mesh 的每对 peer 一条连接。
   - 需要补 multi-peer 协商、房间压力测试、CPU / 带宽治理和断线重建。
   - 先把 2 人、4 人、6 人、10 人的稳定性差异测出来，再决定默认策略。

### B. 身份体系以“注册 + 验证码登录”为主线

1. **建立真实用户域模型**
   - 在现有 `users` 表基础上补齐唯一邮箱、验证状态、登录状态和必要的审计字段。
   - 增加验证码存储、验证码过期策略、会话存储和可选的密码重置令牌。

2. **实现注册与邮箱验证码登录**
   - 注册流程：邮箱 -> 昵称 -> 验证码 -> 创建用户 -> 返回登录页。
   - 登录流程：邮箱 -> 验证码 -> 验证 -> 登录态建立。
   - 先做可替换的接口和 stub，不阻塞真实邮件通道接入。

3. **把后端鉴权真正接上会议业务**
   - 主持人创建会议必须来自已认证用户。
   - `hostUserId` 不再由前端自由拼接，而由登录态映射得到。
   - 用户偏好仍沿用现有 `user_preferences`，但要绑定真实用户身份。

4. **前端登录页从测试态切换为真实状态机**
   - `login / home / schedule / join` 这套卡片切换保留。
   - 登录卡片里的“任意非空邮箱密码可登录”已经替换为真实注册 / 验证码流程，后续重点转为真实邮件投递、验证码风控和会话强化。
   - 预留后续密码登录入口，但不阻塞本阶段主线。

## 涉及模块 / 数据结构 / 接口 / 配置 / 存储影响

### 主要代码模块

- `web/src/rtc.ts`
  - ICE servers 配置化
  - TURN fallback
  - 连接质量监测和恢复

- `web/src/App.tsx`
  - 登录 / 注册 / 验证码流程
  - 主持人身份绑定
  - 会前入口状态机

- `web/src/api.ts`
  - 新增注册、发码、验码、登录、退出、当前用户等 API 包装

- `web/src/signaling.ts`
  - 让信令地址从硬编码端口切换为可配置目标

- `internal/httpapi/server.go`
  - 新增身份相关 HTTP 接口
  - 接入认证中间件

- `internal/meeting/service.go`
  - 创建会议、授权、偏好写入等业务改成依赖真实身份

- `internal/storage/sqlite/schema.sql`
  - 扩展用户、验证码、会话等表结构

- `internal/storage/sqlite/store.go`
  - 新增用户、验证码、会话的 CRUD 和过期清理

### 建议新增或扩展的数据结构

- `users`
  - `email` 唯一化
  - `email_verified_at`
  - `status`
  - `password_hash`
  - `wechat_openid` 先保留，后续再接

- `verification_codes`
  - `email`
  - `code_hash`
  - `purpose`
  - `expires_at`
  - `attempt_count`
  - `sent_at`

- `auth_sessions` 或 `refresh_tokens`
  - `user_id`
  - `token_hash`
  - `expires_at`
  - `revoked_at`
  - `user_agent`
  - `ip_address`

### 建议新增的接口

- `POST /api/auth/register/code`
- `POST /api/auth/register/verify`
- `POST /api/auth/login/code`
- `POST /api/auth/logout`
- `GET /api/auth/me`
- `POST /api/auth/password/reset/code`
- `POST /api/auth/password/reset/confirm`

### 建议新增的配置项

- `VITE_MEETING_ICE_SERVERS`
- `VITE_MEETING_STUN_URLS`
- `VITE_MEETING_TURN_URLS`
- `VITE_MEETING_TURN_USERNAME`
- `VITE_MEETING_TURN_CREDENTIAL`
- `MEETING_VERIFICATION_CODE_TTL`
- `MEETING_VERIFICATION_CODE_RESEND_INTERVAL`
- `MEETING_SESSION_TTL`
- `MEETING_AUTH_RATE_LIMIT`

## 兼容性、迁移方案与风险

### 媒体可靠性风险

- TURN 接入后会带来额外带宽成本。
- 多 peer Mesh 在 10 人房间里会放大上行和 CPU 压力。
- 自适应逻辑如果过激，可能造成清晰度抖动。

### 身份体系风险

- 从“测试登录”切换到真实登录，会改变当前前端的默认体验。
- 验证码流程如果没有限流和过期控制，会带来滥用和刷接口风险。
- 登录态一旦引入，会要求后端所有“创建会议 / 角色授权 / 偏好写入”都改成真实身份校验。

### 迁移策略

- 媒体侧先保留现有 STUN-only 的开发配置，TURN 通过开关逐步打开。
- 身份侧先让开发态 `Mailer` 返回可观测的假验证码，等流程稳定后再接真实邮件通道。
- 认证改造期间，匿名 join 先保留，不强制历史用户立即切换到实名。

## 验证与回滚思路

### 验证

- 单元测试
  - 验证码生成、过期、重发间隔、错误次数限制
  - 用户注册、登录、会话失效
  - ICE 配置解析、候选选择、连接状态机

- 集成测试
  - 2 个浏览器互连
  - 4 个及以上浏览器的 Mesh 行为
  - TURN 可达 / 不可达两种网络路径
  - 邮箱验证码注册 / 登录的完整闭环

- E2E 测试
  - 登录 -> 注册 -> 创建会议 -> 加入会议
  - 匿名 join 与实名 host 的混合场景

### 回滚

- 媒体侧保留 STUN-only 作为临时回退开关，但不作为最终方案。
- 身份侧保留开发态 stub，实现可切换的 `Mailer` 和 session backend，方便替换和回滚。
- 新身份链路上线后，旧测试登录只在本地开发模式保留，不进生产构建。

## 推荐执行顺序

本轮建议按 **1 -> 2 -> 4** 推进，`Phase 0` 与 `Phase 3` 作为前置/后置的贯穿式工作同步推进：

1. 先做媒体可靠性，把 `TURN / coturn`、弱网恢复和 Mesh 稳定性补齐。
2. 再做身份体系，把注册、验证码登录、会话和主持人准入打通。
3. 最后做生产级安全收口，把 `Origin` 白名单、认证边界和接口访问控制补齐。
4. 在上述主线完成后，再做双人会议的最终联调验收。

## 相关链接

- `README.md`
- `README.zh-CN.md`
- `docs/issues/README.md`
- `docs/deploy/coturn.md`
- `docs/adr/ADR-0001-20260325-meeting-architecture.md`
- `web/src/rtc.ts`
- `web/src/App.tsx`
- `internal/storage/sqlite/schema.sql`
- `internal/httpapi/server.go`
- `internal/meeting/service.go`
- `internal/storage/sqlite/store.go`
