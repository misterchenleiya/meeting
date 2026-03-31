# coturn 部署说明

这份说明用于把第 1 项“TURN / coturn 真正落地”补成可执行的部署样例。当前前端已经支持可配置的 ICE / STUN / TURN 服务器，但会议业务后端仍然不承担媒体转发，因此需要单独部署 coturn 作为网络基础设施。

## 作用边界

- 后端 `meeting` 只负责会议业务、信令转发和房间状态
- coturn 负责在浏览器无法直连时提供 TURN relay 中继
- 前端通过 `VITE_MEETING_ICE_SERVERS` 或 `VITE_MEETING_TURN_*` 在构建期注入 ICE 配置

## 端口

常见端口组合如下：

- `3478/udp`：TURN over UDP
- `3478/tcp`：TURN over TCP
- `5349/tcp`：TURN over TLS

当前项目发布包默认把 coturn relay 端口范围收敛到 `52000-52048`，以减少和宿主机上常见高位端口占用冲突；如果你需要其他范围，可以在打包时覆盖 `COTURN_MIN_PORT` / `COTURN_MAX_PORT`，并在防火墙里一并放行。

## 示例配置

下面是一个偏“可直接跑”的 `turnserver.conf` 示例：

```conf
listening-port=3478
tls-listening-port=5349
fingerprint
lt-cred-mech
realm=turn.meeting.07c2.com.cn
server-name=turn.meeting.07c2.com.cn
user=meeting:CHANGE_ME_STRONG_PASSWORD
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

如果你更倾向于 `static-auth-secret` 模式，也可以把前端 ICE 配置改成对应的 long-term credential 方式，但当前仓库里的前端环境变量示例默认按 `username / credential` 组合来写。

## 前端环境变量

前端构建时可直接读取下面这些变量：

```bash
VITE_MEETING_ICE_SERVERS='[{"urls":["stun:stun.l.google.com:19302"]},{"urls":["turn:turn.meeting.07c2.com.cn:3478?transport=udp","turn:turn.meeting.07c2.com.cn:3478?transport=tcp","turns:turn.meeting.07c2.com.cn:5349?transport=tcp"],"username":"meeting","credential":"CHANGE_ME_STRONG_PASSWORD"}]'
VITE_MEETING_STUN_URLS='stun:stun.l.google.com:19302'
VITE_MEETING_TURN_URLS='turn:turn.meeting.07c2.com.cn:3478?transport=udp,turn:turn.meeting.07c2.com.cn:3478?transport=tcp,turns:turn.meeting.07c2.com.cn:5349?transport=tcp'
VITE_MEETING_TURN_USERNAME=meeting
VITE_MEETING_TURN_CREDENTIAL=CHANGE_ME_STRONG_PASSWORD
```

说明：

- 如果设置了 `VITE_MEETING_ICE_SERVERS`，它会优先于 `VITE_MEETING_STUN_URLS` 和 `VITE_MEETING_TURN_*`
- 如果只是想快速验证 TURN，直接设置 `VITE_MEETING_TURN_*` 就够了
- 不要把测试凭据原样带到生产环境

## 验证方式

1. 启动 coturn，并确保 `3478`、`5349` 和 relay 端口范围 `52000-52048` 可达
2. 用上面的前端环境变量重新构建前端
3. 打开会议页，在调试信息里观察 candidate 类型
4. 当网络环境无法直连时，应该能看到 `relay` candidate，而不是只停留在 `host` / `srflx`

## 常见问题

- 如果只看到 `host` / `srflx`，通常是 TURN 没生效、端口没放行，或者浏览器当前网络已经可以直连
- 如果 TLS 证书不正确，`turns:` 连接会失败
- 如果用户名和密码不匹配，浏览器不会拿到 relay candidate
