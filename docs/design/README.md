# Meeting Design Assets

本目录存放 `Meeting` 当前前端和历史快照的可复用设计资产。

## 仓库约定

- HTML / CSS 预览源码和渲染脚本可以直接入库。
- `png` / `jpg` / `webp` 等示意图图片按本地产物处理，默认加入根目录 `.gitignore`，便于后续反复渲染但不污染仓库历史。
- 后续只要有新的前端 UI 方案，需要把可复用源码放到本目录，再按需本地渲染图片用于评审。
- 当前目录同时保留两类预览：`旧版综合控制台快照` 和 `当前产品化 UI 预览`。
- `旧版综合控制台快照` 对应前一版综合控制台结构，主要用于回顾历史实现。
- `当前产品化 UI 预览` 对应已经落地到前端主界面的登录 / 入会 / 会中舞台化设计。

## 文件说明

- `meeting-entry-preview.html`：当前实现下的创建 / 加入会议状态快照
- `meeting-entry-preview.css`：当前实现下的创建 / 加入状态样式
- `meeting-console-preview.html`：当前实现下的会中综合控制台快照
- `meeting-console-preview.css`：当前实现下的会中控制台样式
- `meeting-auth-preview.html`：当前产品化登录 / 登录后入口 / 预定会议 / 加入会议 / 密码悬浮窗预览
- `meeting-auth-preview.css`：当前产品化入会前页面样式
- `meeting-room-preview.html`：当前产品化会中舞台、右侧抽屉、邀请弹窗、录制申请弹窗预览
- `meeting-room-preview.css`：当前产品化会中页面样式
- `20260325-product-ui-rollout.md`：本轮产品化 UI 落地方案、兼容实现与验证记录
- `render-previews.sh`：本地后台渲染所有设计预览的桌面端和移动端图片
- `meeting-auth-preview-desktop.png`：入会前产品预览桌面图，本地生成，不入库
- `meeting-auth-preview-mobile.png`：入会前产品预览移动图，本地生成，不入库
- `meeting-room-preview-desktop.png`：会中产品预览桌面图，本地生成，不入库
- `meeting-room-preview-mobile.png`：会中产品预览移动图，本地生成，不入库
- `meeting-host-login-preview.png`：主持人视角的登录页独立渲染图，本地生成，不入库
- `meeting-host-create-preview.png`：主持人视角的创建会议页独立渲染图，本地生成，不入库
- `meeting-host-room-preview.png`：主持人视角的进入会议页独立渲染图，本地生成，不入库
- `meeting-entry-preview-desktop.png`：当前创建 / 加入页桌面端渲染图，本地生成，不入库
- `meeting-entry-preview-mobile.png`：当前创建 / 加入页移动端渲染图，本地生成，不入库
- `meeting-console-preview-desktop.png`：当前会中控制台桌面端渲染图，本地生成，不入库
- `meeting-console-preview-mobile.png`：当前会中控制台移动端渲染图，本地生成，不入库

## 使用方式

直接在浏览器中打开对应的 `html` 文件即可查看预览页；样式会自动从同目录下的 `css` 文件加载。

推荐优先查看：

- `meeting-auth-preview.html`
- `meeting-room-preview.html`

这两份是当前已经落地的产品化 UI 预览，顶部按钮可切换主要状态。

- `meeting-auth-preview.html` 顶部按钮可切换登录、登录后入口、预定会议、加入会议和输入会议号后的密码悬浮窗。
- `meeting-room-preview.html` 顶部按钮可切换无视频 / 无共享时的头像墙舞台、有主画面时的舞台布局、成员侧栏、聊天侧栏、邀请弹窗和录制申请弹窗。
- `meeting-room-preview.html` 右侧缩略窗支持双击切换主画面。

如需重新生成示意图，可执行：

```bash
./docs/design/render-previews.sh all
```

也可以只渲染某一组页面：

```bash
./docs/design/render-previews.sh auth
./docs/design/render-previews.sh room
./docs/design/render-previews.sh host-flow
./docs/design/render-previews.sh entry
./docs/design/render-previews.sh console
```
