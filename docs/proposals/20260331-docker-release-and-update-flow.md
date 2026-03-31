# 20260331 Docker 化发布与原地更新流程方案

- Status: implemented
- Date: 2026-03-31

## 背景与问题定义

当前项目已经不再是单一后端程序，而是由三类运行组件共同组成：

1. **后端服务**
2. **前端服务**
3. **coturn 第三方 TURN 服务**

同时，后续生产发布又有一组明确约束：

- 三个运行组件都必须运行在 Docker 环境中。
- 生产环境通过 Nginx 反代到本机回环地址，不由本方案管理 Nginx。
- 发布产物必须遵循标准打包流程，最终输出：
  - `meeting_${commit}.tar.gz`
  - `latest.txt`
- 生产环境会通过 crontab 每 1 分钟执行一次 `update.sh`，以自动检测更新。
- `update.sh` 必须保持“下载新版本后原地更新”的方式，不使用临时目录，不备份旧版本，不做自动回滚。

此外，当前仓库根目录尚未建立统一的发布脚本目录，也没有把发布脚本纳入标准打包范围。原先 `/Users/chenlei/Codes/07c2/www/public` 下的脚本是为单后端服务准备的，不能直接覆盖当前项目的多组件发布场景。

因此，需要先形成一份独立的发布与更新方案，统一以下问题：

- 脚本放置位置与来源
- Docker 三容器的命名和启动方式
- `status.sh` 的输出边界
- `update.sh` 的就地更新语义
- Makefile 的标准打包发布流程

## 目标与非目标

### 目标

- 将发布脚本统一放到项目根目录的 `scripts/` 下。
- 以 `/Users/chenlei/Codes/07c2/www/public` 中的脚本为模板，先复制到本项目 `scripts/` 再按需要修改。
- 保证每次发布新版本时，`scripts/` 中的脚本会与后端、前端、coturn 一起被打包。
- 固定三个运行容器的名称：
  - `meeting-backend`
  - `meeting-frontend`
  - `meeting-coturn`
- `status.sh` 继续只展示后端状态，使用 `tail -F` 持续跟踪后端日志。
- 让 `update.sh` 保持就地更新风格，完成下载、校验、覆盖、重启三个容器的一键更新。
- 让 Makefile 补齐标准打包发布链路，并产出 `meeting_${commit}.tar.gz` 与 `latest.txt`。

### 非目标

- 不在 `update.sh` 中管理 Nginx 配置或重载 Nginx。
- 不做临时目录更新，不做旧版本备份，不做自动回滚。
- 不在此方案中引入多实例扩容策略。
- 不在此方案中实现真实邮件发送或生产部署编排。

## 方案概述与核心决策

### A. 脚本目录与模板来源

1. **脚本统一落在项目根目录的 `scripts/`**
   - 新项目约定下，所有发布脚本都应在仓库根目录下可见、可打包、可执行。
   - `scripts/` 应成为发布包的固定组成部分，而不是仅存在于运维机器上的私有文件。

2. **脚本以 `/Users/chenlei/Codes/07c2/www/public` 为模板复制后修改**
   - 当前 `public` 下的脚本是单后端发布模型。
   - 本项目应先复制一份到 `scripts/`，再按三容器 Docker 模型修改。
   - 这样可以最大限度保留原有脚本的惯用参数、提示和操作习惯，降低运维迁移成本。

### B. 三个运行组件统一 Docker 化

1. **后端**
   - 运行容器固定命名为 `meeting-backend`。
   - 对外只暴露到本机回环地址，由 Nginx 反代访问。

2. **前端**
   - 运行容器固定命名为 `meeting-frontend`。
   - 同样只暴露到本机回环地址，由 Nginx 反代访问。

3. **coturn**
   - 运行容器固定命名为 `meeting-coturn`。
   - 通过独立 TURN 端口直接对外提供服务，不纳入 Nginx 反代链路。

### C. 启停与状态脚本维持最小职责

1. **`start.sh`**
   - 负责启动 `meeting-backend`、`meeting-frontend`、`meeting-coturn`。
   - 不负责构建，不负责下载更新，不负责 Nginx。

2. **`stop.sh`**
   - 负责停止三个容器。
   - 不删除数据卷，不删除发布目录，不处理 Nginx。

3. **`restart.sh`**
   - 负责重新拉起三个容器。
   - 逻辑上可以是 stop + start 或 compose 的重建方式，但职责边界保持不变。

4. **`status.sh`**
   - 保持当前“只关注后端”的状态展示原则。
   - 继续只 tail 后端日志。
   - 明确改为使用 `tail -F`，避免日志轮转后丢失跟踪。

### D. `update.sh` 保持“原地更新”模式

`update.sh` 仍然负责自动检测更新，但必须遵守以下约束：

- 通过 `latest.txt` 判断是否有新版本。
- 下载 `meeting_${commit}.tar.gz`。
- 校验 `sha256sum`。
- 在当前部署目录内直接解压覆盖。
- 更新 `current` 版本标记文件。
- 重启三个 Docker 容器。
- 不使用临时目录。
- 不备份旧版本。
- 不做自动回滚。
- 不管理 Nginx。

### E. Makefile 补齐标准打包发布流程

Makefile 应作为标准发布入口，补齐以下目标：

- `build`
- `linux`
- `clean`
- `pack`
- `upload`
- `publish`

其中：

- `pack` 负责产出 `meeting_${commit}.tar.gz` 和 `latest.txt`。
- `upload` 只负责上传，不负责编译或打包。
- `publish` 按 `clean -> linux -> pack -> upload` 顺序执行。

## 涉及模块 / 文件 / 接口 / 配置 / 存储影响

### 新增或调整的目录约定

- `scripts/`
  - 放置 `start.sh`、`stop.sh`、`restart.sh`、`status.sh`、`update.sh`
  - 这些脚本要进入每次发布包

- `docker-compose.yml`
  - 定义 `meeting-backend`、`meeting-frontend`、`meeting-coturn`
  - 负责容器命名、端口映射、环境变量和卷挂载

- `build/`
  - 仍作为本地构建与打包中间产物目录

### 主要受影响的文件

- `Makefile`
  - 增加标准发布目标
  - 确保 `scripts/` 被纳入打包流程

- `scripts/start.sh`
- `scripts/stop.sh`
- `scripts/restart.sh`
- `scripts/status.sh`
- `scripts/update.sh`
  - 从 `public` 模板迁移而来，并适配 Docker 三容器模型

- `docker-compose.yml`
  - 新增或重构为三容器编排定义

### 发布包内容要求

`meeting_${commit}.tar.gz` 中至少应包含：

- `scripts/`
- `docker-compose.yml`
- 后端运行材料
- 前端运行材料
- coturn 配置材料
- 当前版本标记或清单文件

### 版本标记要求

- `latest.txt` 必须继续存在。
- 其内容应至少包含：
  - `filename: meeting_${commit}.tar.gz`
  - `sha256sum: <hash>`

### 组件命名要求

容器名必须稳定固定为：

- `meeting-backend`
- `meeting-frontend`
- `meeting-coturn`

这要求 compose 配置显式指定 `container_name`，避免默认命名漂移。

## 兼容性、迁移方案与风险

### 兼容性影响

- 原先 `/Users/chenlei/Codes/07c2/www/public` 的脚本仍可作为模板保留，但不应再作为当前项目的运行依赖。
- 新发布包会显式包含 `scripts/`，这意味着脚本将和应用版本绑定。
- `status.sh` 从一般化状态检查收缩回“只看后端日志”，与当前项目的运维习惯保持一致。

### 迁移风险

- `update.sh` 不做回滚，一旦中途失败，可能留下半更新状态。
- 三容器固定命名后，不适合后续直接扩展为多副本部署。
- `tail -F` 依赖日志路径或容器日志驱动稳定；如果后端日志策略变化，`status.sh` 也要同步调整。
- `scripts/` 迁移进仓库后，若 Makefile 漏打包，会导致生产环境找不到脚本，因此 `pack` 必须做完整性校验。

### 迁移方式

1. 先把 `public` 的脚本复制到本项目 `scripts/`。
2. 再按 Docker 三容器模型和本项目的命名规则逐个改造。
3. 同步调整 Makefile 的 `pack`、`upload`、`publish` 目标。
4. 最后把生产 crontab 指向新的 `scripts/update.sh`。

## 验证与回滚思路

### 验证

- 验证 `scripts/` 下脚本可在仓库根目录直接执行。
- 验证 `status.sh` 只跟踪后端日志，且使用 `tail -F`。
- 验证 `start.sh` / `stop.sh` / `restart.sh` 能正确控制三个 Docker 容器。
- 验证 `pack` 产物包含 `scripts/`、`docker-compose.yml` 和运行材料。
- 验证 `pack` 能生成：
  - `meeting_${commit}.tar.gz`
  - `latest.txt`
- 验证 `upload` 先传 tar.gz 后传 latest.txt。
- 验证已完成：
  - `make build`
  - `go test ./...`
  - `make -n build`
  - `make -n linux`
  - `make -n pack`

### 回滚

- 本方案不提供自动回滚。
- 如果更新失败，只能通过人工重新执行 `update.sh`、重新发布完整包，或在宿主机上进行人工修复。

## 推荐执行顺序

本方案建议按以下顺序实施：

1. 先从 `/Users/chenlei/Codes/07c2/www/public` 复制脚本到本项目根目录 `scripts/`。
2. 再把 `start.sh / stop.sh / restart.sh / status.sh / update.sh` 改造成三容器 Docker 模型。
3. 同步补齐 `docker-compose.yml` 和对应运行环境变量。
4. 最后改 Makefile，使其能够标准化打包、上传和发布。

## 相关链接

- `/Users/chenlei/Codes/07c2/www/public/start.sh`
- `/Users/chenlei/Codes/07c2/www/public/stop.sh`
- `/Users/chenlei/Codes/07c2/www/public/restart.sh`
- `/Users/chenlei/Codes/07c2/www/public/status.sh`
- `/Users/chenlei/Codes/07c2/www/public/update.sh`
- `/Users/chenlei/Codes/www/github.com/07c2/meeting/Makefile`
- `/Users/chenlei/Codes/www/github.com/07c2/meeting/docs/proposals/20260331-media-reliability-and-identity-plan.md`
- `/Users/chenlei/Codes/www/github.com/07c2/meeting/docs/deploy/coturn.md`
