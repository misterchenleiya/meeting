# 20260401 邮箱验证码自动注册与外部发信方案

- Status: implemented
- Date: 2026-04-01

## 背景与问题定义

当前仓库已经具备一套可运行的身份基础设施：

- 后端已实现注册验证码、登录验证码、用户表、验证码表和会话表。
- 前端已实现登录卡片、注册卡片和验证码登录的基础状态机。
- 数据库已预留 `users.password_hash` 字段，可支持后续密码能力扩展。

但当前实现仍然有三个明显缺口：

1. **邮箱验证码登录只允许已注册用户使用**
   - `RequestLoginCode` 和 `CompleteLogin` 目前会对未注册邮箱直接返回 `ErrNotRegistered`。
   - 这要求用户必须先走“注册”入口，无法做到“新用户直接验证码登录，成功后自动完成注册”。

2. **验证码发送仍处于 debug 模式**
   - 后端当前直接返回 `debugCode` 和 `deliveryMode=debug`，前端会把验证码自动回填。
   - 生产环境没有真实邮件投递能力，无法让用户真正收到验证码。

3. **密码字段仅为预留，没有明确产品语义**
   - 数据库已有 `password_hash`，但当前既没有密码登录 API，也没有“未设置密码”的提示规则。
   - 如果后续补密码登录入口，必须先明确“空密码字段”的含义和返回语义。

本方案的目标，是在不一次性引入完整密码体系的前提下，先完成一条可上线的“邮箱验证码登录主线”，同时为未来密码登录和密码设置保留兼容空间。

## 目标与非目标

### 目标

- 保留现有自助注册入口。
- 支持新用户直接通过邮箱验证码登录。
- 未注册邮箱在验证码登录成功后自动完成注册并建立会话。
- 数据库用户 `password_hash` 为空时，明确表示“该用户尚未设置密码”。
- 当用户尝试密码登录且账号未设置密码时，返回明确提示，引导其改用邮箱验证码登录。
- 在生产环境中启用真实邮件验证码发送能力。
- Docker 部署继续保持简单，优先复用现有 `meeting-backend` 容器，不额外引入新的业务容器。

### 非目标

- 本轮不实现“设置密码 / 修改密码 / 忘记密码 / 重置密码”的完整闭环。
- 本轮不接入微信登录、扫码登录或企业 SSO。
- 本轮不引入独立的邮件微服务、消息队列或异步任务系统。
- 本轮不做复杂的反垃圾、设备指纹、图形验证码或地域风控。
- 本轮不改变匿名加入会议（join meeting）的能力。

## 方案概述与核心决策

### 1. 登录主线改为“验证码优先”，注册入口保留

身份入口保留两条路径：

- **显式注册**
  - 用户输入邮箱、昵称并获取注册验证码。
  - 验证成功后创建用户，并回到登录态或直接进入已登录态。
- **验证码登录**
  - 用户输入邮箱并获取登录验证码。
  - 若邮箱已注册，则按现有用户登录。
  - 若邮箱未注册，则在验证码验证成功后自动创建用户并直接登录。

也就是说，注册入口仍然存在，但不再是新用户的唯一入口。

### 2. 自动注册默认昵称使用邮箱前缀生成

验证码登录自动注册时，用户并没有显式填写昵称，因此必须有一个稳定的默认昵称策略。

本方案采用：

- 默认昵称 = 邮箱 `@` 前的本地部分（local-part）
- 对非法字符做最小清洗，仅保留产品允许的字符
- 若生成结果为空，则回退到 `用户` + 4 位随机数字
- 若昵称冲突，则在末尾追加短后缀

示例：

- `chenlei@example.com` -> `chenlei`
- `a.b-c@example.com` -> `a.b-c`
- `@@bad@example.com` -> `用户4821`

这样可以避免“验证码登录后还要再补昵称”的二次打断，优先保证主流程打通。

### 3. `password_hash` 为空字符串表示“未设置密码”

当前表结构已有 `password_hash` 字段，本方案明确它的产品语义：

- `password_hash == ""`：账号存在，但尚未设置密码
- `password_hash != ""`：账号已设置密码，可参与密码登录校验

这里不使用 `NULL` 作为业务语义，而继续使用当前实现已经兼容的空字符串，避免在 SQLite 扫描和旧数据兼容上引入额外分支。

### 4. 增加最小密码登录接口，但不实现密码管理闭环

为了满足“用户尝试密码登录时进行提示”，需要补一个最小密码登录入口：

- `POST /api/auth/login/password`

返回语义：

- 用户不存在：`ErrNotRegistered`
- 用户存在但 `password_hash == ""`：`ErrPasswordNotSet`
- 用户存在且密码错误：`ErrPasswordInvalid`
- 用户存在且密码正确：建立会话并返回用户信息

本轮不提供“设置密码”入口，因此这个密码登录 API 的主要用途是：

- 为后续密码体系提供兼容接口
- 对已经有密码的存量用户或后续人工导入用户开放
- 对无密码账号给出明确提示，而不是模糊失败

### 5. 生产发信采用外部邮件服务，不新增独立邮件服务容器

本方案明确拒绝“在 compose 里新增一个验证码发送服务容器”作为生产首选。

原因：

- 当前用户量小，额外维护一个邮件服务容器收益低、运维成本高。
- 自建 SMTP / 邮件中继会带来域名信誉、SPF/DKIM/DMARC、出站端口和垃圾邮件治理问题。
- 对验证码邮件这种低频场景，最简单稳定的做法是由 `meeting-backend` 直接连接外部邮件服务。

因此生产部署策略为：

- `meeting-backend` 内置 `Mailer` 抽象
- 根据配置选择：
  - `debug mailer`
  - `smtp mailer`
  - `sendcloud api mailer`
- `docker-compose.yml` 只为 `meeting-backend` 注入外部邮件服务相关环境变量
- 不新增 `mail-service`、`worker`、`queue` 等新容器

当前实现优先推荐 SendCloud API 作为生产发信路径，SMTP 继续作为备选回退方案。开发或预发环境如需本地收信观察，可选接 `Mailpit/MailHog`，但这不是生产方案的一部分。

## 涉及模块 / 数据结构 / 接口 / 配置 / 存储影响

### 主要代码模块

- `internal/auth/service.go`
  - 放宽登录验证码的发码条件
  - 在验证码验证成功后支持自动注册
  - 新增密码登录分支和错误语义
  - 接入 `Mailer`

- `internal/httpapi/auth.go`
  - 调整登录验证码接口行为
  - 新增密码登录接口
  - 对 `ErrPasswordNotSet` 等错误做明确映射

- `internal/config/config.go`
  - 增加邮件发送相关配置项

- `internal/storage/sqlite/auth.go`
  - 如有需要，补用户密码查询 / 更新辅助方法
  - 保持 `password_hash` 空字符串语义一致

- `web/src/App.tsx`
  - 登录页补“验证码登录 / 密码登录”切换
  - 登录成功后对“自动注册”场景做正确提示
  - 密码登录失败且账号无密码时显示明确文案

- `web/src/api.ts`
  - 新增密码登录 API 包装
  - 扩展验证码登录返回结构，支持识别是否自动注册

- `docker-compose.yml`
  - 为 `meeting-backend` 注入 SendCloud API 或 SMTP 环境变量

### 数据结构

现有 `users` 表不必新增必需字段，但需要明确约束：

- `password_hash` 为空字符串表示“未设置密码”
- 自动注册用户的 `email_verified_at` 在验证码验证成功后立即写入
- 自动注册用户的 `nickname` 按默认昵称规则生成

如后续需要更强审计，可考虑后补：

- `last_login_at`
- `last_login_method`
- `password_set_at`

本轮不是必须项。

### 接口变化

保留现有接口：

- `POST /api/auth/register/code`
- `POST /api/auth/register/verify`
- `POST /api/auth/login/code`
- `POST /api/auth/login/verify`
- `GET /api/auth/me`
- `POST /api/auth/logout`

新增接口：

- `POST /api/auth/login/password`

返回字段建议扩展：

- `POST /api/auth/login/verify`
  - 增加 `autoRegistered: boolean`
  - 用于前端区分“老用户登录成功”和“新用户验证码登录后自动注册成功”

- `POST /api/auth/login/code`
  - 在生产模式下不再返回 `debugCode`
  - `deliveryMode` 从 `debug` 切换到 `sendcloud_api` 或 `smtp`

### 配置变化

建议新增以下后端环境变量：

- `MEETING_MAILER_MODE`
  - `debug` / `smtp` / `sendcloud_api`
- `MEETING_SMTP_HOST`
- `MEETING_SMTP_PORT`
- `MEETING_SMTP_USERNAME`
- `MEETING_SMTP_PASSWORD`
- `MEETING_SMTP_FROM_ADDRESS`
- `MEETING_SMTP_FROM_NAME`
- `MEETING_SMTP_REQUIRE_TLS`
- `MEETING_SENDCLOUD_API_BASE_URL`
- `MEETING_SENDCLOUD_API_USER`
- `MEETING_SENDCLOUD_API_KEY`
- `MEETING_SENDCLOUD_FROM_ADDRESS`
- `MEETING_SENDCLOUD_FROM_NAME`
- `MEETING_AUTH_CODE_SUBJECT_PREFIX`

其中：

- 本地开发默认 `MEETING_MAILER_MODE=debug`
- 生产环境优先使用 `MEETING_MAILER_MODE=sendcloud_api`

## 兼容性、迁移方案与风险

### 兼容性

- 现有显式注册流程保留，已注册用户体验不受破坏。
- 现有验证码登录接口路径保留，前端只需要调整分支语义，不需要整体重写。
- 现有数据库可直接兼容，`password_hash` 已存在，不需要新增表。

### 迁移策略

1. 先在开发环境保留 `debug mailer`
2. 增加外部发信 mailer 并通过配置切换
3. 登录验证码接口先支持“未注册也可发码”
4. 验证登录成功后自动注册
5. 再补最小密码登录入口和“未设置密码”提示
6. 最后切换生产环境 `MEETING_MAILER_MODE=sendcloud_api`

### 风险

1. **自动注册语义变化**
   - 以前未注册邮箱会直接失败，现在会变成可登录
   - 这属于产品行为变化，需要在文档和提示文案上同步

2. **默认昵称策略可能不符合用户预期**
   - 使用邮箱前缀会带来“昵称不够产品化”的问题
   - 但相对首次登录强制补资料，这个取舍更符合“功能优先”

3. **外部发信稳定性**
   - 发信失败、超时、认证错误都需要可观测日志
   - 生产环境必须确保域名验证、API 凭据或 SMTP 凭据配置正确

4. **密码登录入口会让用户以为已经支持完整密码体系**
   - 因此必须在 UI 和返回文案中明确：
     - 账号未设置密码时，请使用邮箱验证码登录

## 验证与回滚思路

### 验证

- 单元测试
  - 未注册邮箱请求登录验证码成功
  - 未注册邮箱验证码验证成功后自动创建用户
  - 自动注册昵称生成规则
  - `password_hash == ""` 时密码登录返回 `ErrPasswordNotSet`
  - SendCloud API / SMTP mailer 配置校验与失败路径

- 集成测试
  - 已注册用户验证码登录成功
  - 新用户验证码登录后自动注册成功
  - 显式注册后再验证码登录成功
  - 密码登录尝试命中“未设置密码”提示

- 部署验证
  - 生产环境通过外部邮件服务成功发送验证码邮件
  - 后端日志中能观察到发信成功 / 失败原因
  - 前端不再显示 `debugCode` 自动回填

### 回滚

- 若 SendCloud API 接入失败，可把 `MEETING_MAILER_MODE` 临时切回 `smtp` 或 `debug`
- 若自动注册带来产品问题，可恢复 `POST /api/auth/login/code` 的“仅已注册用户可发码”策略
- 新增的密码登录入口可以先隐藏前端入口，仅保留后端接口，为回滚留空间

## 备选方案与放弃原因

### 方案 A：继续维持“必须先注册，再验证码登录”

优点：

- 语义更简单
- 不需要默认昵称策略

放弃原因：

- 会增加新用户首登阻力
- 不符合“新用户直接邮箱验证码登录、成功后自动注册”的目标

### 方案 B：自动注册后强制补昵称

优点：

- 资料质量更高

放弃原因：

- 会在首次登录主路径上增加一步强制交互
- 当前目标是功能优先，不应先做 onboarding 打断

### 方案 C：在 Docker Compose 里新增独立邮件服务容器

优点：

- 邮件职责看起来更独立

放弃原因：

- 对当前规模明显过度设计
- 自建邮件发送基础设施的运维复杂度远高于直接接外部邮件服务
- 不符合“先实现功能为主”的优先级

## 推荐实施顺序

1. 先补 `Mailer` 抽象与 `debug/smtp/sendcloud_api` 多实现
2. 再改后端验证码登录语义，支持自动注册
3. 补前端“自动注册成功”提示与密码登录入口
4. 最后把生产环境 compose 配置切到 SMTP 模式并联调

## 相关链接

- [docs/proposals/20260331-media-reliability-and-identity-plan.md](/Users/chenlei/Codes/www/github.com/07c2/meeting/docs/proposals/20260331-media-reliability-and-identity-plan.md)
- [internal/auth/service.go](/Users/chenlei/Codes/www/github.com/07c2/meeting/internal/auth/service.go)
- [internal/httpapi/auth.go](/Users/chenlei/Codes/www/github.com/07c2/meeting/internal/httpapi/auth.go)
- [internal/storage/sqlite/schema.sql](/Users/chenlei/Codes/www/github.com/07c2/meeting/internal/storage/sqlite/schema.sql)
- [internal/storage/sqlite/auth.go](/Users/chenlei/Codes/www/github.com/07c2/meeting/internal/storage/sqlite/auth.go)
- [web/src/App.tsx](/Users/chenlei/Codes/www/github.com/07c2/meeting/web/src/App.tsx)
- [web/src/api.ts](/Users/chenlei/Codes/www/github.com/07c2/meeting/web/src/api.ts)
- [docker-compose.yml](/Users/chenlei/Codes/www/github.com/07c2/meeting/docker-compose.yml)
