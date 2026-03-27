# Meeting Design Assets

本目录存放 `Meeting` 当前前端和后续方案的可复用设计资产。

## 仓库约定

- HTML / CSS 预览源码和渲染脚本可以直接入库。
- `png` / `jpg` / `webp` 等示意图图片按本地产物处理，默认加入根目录 `.gitignore`，便于后续反复渲染但不污染仓库历史。
- 后续只要有新的前端 UI 方案，需要把可复用源码放到本目录，再按需本地渲染图片用于评审。
- 当前目录同时保留两类预览：`当前产品化 UI 预览` 和 `下一版布局提案预览`。
- `当前产品化 UI 预览` 主要对应已经落地到前端主界面的登录 / 入会设计。
- `下一版布局提案预览` 对应尚未落地但已经形成设计文档和渲染源码的会中全屏菜单化方案。

## 文件说明

- `meeting-auth-preview.html`：当前产品化登录 / 登录后入口 / 预定会议 / 加入会议 / 密码悬浮窗预览
- `meeting-auth-preview.css`：当前产品化入会前页面样式
- `meeting-room-preview.html`：参考截图的会中全屏房间预览
- `meeting-room-preview.css`：参考截图的会中全屏房间样式
- `20260325-product-ui-rollout.md`：本轮产品化 UI 落地方案、兼容实现与验证记录
- `20260327-room-fullscreen-menu-layout.md`：参考截图风格的会中单屏布局方案
- `render-previews.sh`：本地后台渲染所有设计预览的桌面端和移动端图片
- `meeting-auth-preview-desktop.png`：入会前产品预览桌面图，本地生成，不入库
- `meeting-auth-preview-mobile.png`：入会前产品预览移动图，本地生成，不入库
- `meeting-room-preview-desktop.png`：会中产品预览桌面图，本地生成，不入库
- `meeting-room-preview-mobile.png`：会中产品预览移动图，本地生成，不入库
- `meeting-host-login-preview.png`：主持人视角的登录页独立渲染图，本地生成，不入库
- `meeting-host-create-preview.png`：主持人视角的创建会议页独立渲染图，本地生成，不入库
- `meeting-host-room-preview.png`：主持人视角的进入会议页独立渲染图，本地生成，不入库

## 使用方式

直接在浏览器中打开对应的 `html` 文件即可查看预览页；样式会自动从同目录下的 `css` 文件加载。

推荐优先查看：

- `meeting-auth-preview.html`
- `meeting-room-preview.html`

前两份分别对应已落地的产品化登录 / 入会预览和参考截图风格的会中房间预览。

- `meeting-auth-preview.html` 顶部按钮可切换登录、登录后入口、预定会议、加入会议和输入会议号后的密码悬浮窗。
- `meeting-room-preview.html` 直接展示参考截图风格的会中全屏房间，默认包含昵称修改、参会者工具和主持人工具等隐藏层结构。

补充阅读：

- `20260327-room-fullscreen-menu-layout.md`：参考截图风格的会中单屏布局方案
- `meeting-room-preview.html`：对应方案的 HTML 预览页
- `meeting-room-preview.css`：对应方案的样式文件

如需重新生成示意图，可执行：

```bash
./docs/design/render-previews.sh all
```

也可以只渲染某一组页面：

```bash
./docs/design/render-previews.sh auth
./docs/design/render-previews.sh room
./docs/design/render-previews.sh host-flow
```
