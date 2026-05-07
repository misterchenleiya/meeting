# coturn 部署说明

这份说明用于把当前仓库的 TURN 动态凭据链路补成可执行部署方案。现在的生产主路径已经不是“把固定 TURN 密码打进前端包”，而是：

1. coturn 运行在 `use-auth-secret` 模式
2. 后端用共享密钥为当前参会者签发短期 TURN 凭据
3. 前端在运行时从后端获取 `iceServers`

## 作用边界

- 后端 `meeting` 只负责会议业务、信令转发和房间状态
- coturn 负责在浏览器无法直连时提供 TURN relay 中继
- 前端生产包只保留 STUN fallback；运行时 TURN 配置由后端接口下发

## 端口

常见端口组合如下：

- `3478/udp`：TURN over UDP
- `3478/tcp`：TURN over TCP
- `5349/tcp`：TURN over TLS

当前项目发布包默认把 coturn relay 端口范围收敛到 `52000-52048`，以减少和宿主机上常见高位端口占用冲突；如果你需要其他范围，可以在打包时覆盖 `COTURN_MIN_PORT` / `COTURN_MAX_PORT`，并在防火墙里一并放行。

## coturn 示例配置

下面是一个偏“可直接跑”的 `turnserver.conf` 示例：

```conf
listening-port=3478
tls-listening-port=5349
fingerprint
use-auth-secret
realm=turn.meeting.07c2.com.cn
server-name=turn.meeting.07c2.com.cn
static-auth-secret=CHANGE_ME_TURN_SHARED_SECRET
no-loopback-peers
no-multicast-peers
stale-nonce
cert=/etc/letsencrypt/live/turn.meeting.07c2.com.cn/fullchain.pem
pkey=/etc/letsencrypt/live/turn.meeting.07c2.com.cn/privkey.pem
min-port=52000
max-port=52048
log-file=stdout
simple-log
```

注意：

- `CHANGE_ME_TURN_SHARED_SECRET` 只是模板占位，不得提交真实值到仓库
- 发布时应通过本地环境变量 `TURN_SHARED_SECRET` 渲染到最终发布包里的 `coturn/turnserver.conf`
- coturn 与后端必须使用同一份共享密钥，否则浏览器拿不到 relay candidate

## 后端环境变量

后端运行时至少需要以下配置：

```bash
MEETING_STUN_URLS=stun:stun.l.google.com:19302
MEETING_TURN_URLS=turn:turn.meeting.07c2.com.cn:3478?transport=udp,turn:turn.meeting.07c2.com.cn:3478?transport=tcp,turns:turn.meeting.07c2.com.cn:5349?transport=tcp
MEETING_TURN_SHARED_SECRET=replace_with_real_turn_shared_secret
MEETING_TURN_TTL_SECONDS=43200
```

说明：

- `MEETING_TURN_SHARED_SECRET` 只应该放在后端环境文件或本地发布环境变量里
- `MEETING_TURN_TTL_SECONDS` 默认 `43200` 秒，也就是 12 小时
- 如果 `MEETING_TURN_URLS` 或 `MEETING_TURN_SHARED_SECRET` 缺失，后端会退回只返回 STUN，不会向前端暴露固定 TURN 密码

## 发布链路

生产发布前，本地至少需要准备：

```bash
export TURN_SHARED_SECRET='replace_with_real_turn_shared_secret'
```

然后执行正常的发布命令：

```bash
make pack
# 或 make publish
```

当前发布链路会做两件事：

- 前端生产包不再注入固定 TURN 用户名和密码
- coturn 发布模板会把 `TURN_SHARED_SECRET` 渲染到最终的 `static-auth-secret`

## 验证方式

1. 启动 coturn，并确保 `3478`、`5349` 和 relay 端口范围 `52000-52048` 可达
2. 确认后端环境文件里已经配置 `MEETING_TURN_URLS` 和 `MEETING_TURN_SHARED_SECRET`
3. 用浏览器加入会议，让前端调用 `POST /api/meetings/{meetingID}/participants/{participantID}/ice-servers`
4. 打开调试信息观察 candidate 类型
5. 当网络环境无法直连时，应该能看到 `relay` candidate，而不是只停留在 `host` / `srflx`

## 常见问题

- 如果只看到 `host` / `srflx`，通常是 TURN 没生效、端口没放行，后端没配置 `MEETING_TURN_SHARED_SECRET`，或者浏览器当前网络已经可以直连
- 如果 TLS 证书不正确，`turns:` 连接会失败
- 如果 coturn 的 `static-auth-secret` 与后端 `MEETING_TURN_SHARED_SECRET` 不一致，浏览器不会拿到 relay candidate
- 如果 coturn 容器读取 `/etc/letsencrypt/live/...` 证书仍有权限问题，需要在运行用户、ACL 或挂载策略上单独处理；这一步不由前端或后端代码自动解决
