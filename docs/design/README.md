# Meeting Design Assets

本目录存放 `Meeting` 当前前端和后续方案的可复用设计资产。

当前 `H5` 和 `微信小程序` 设计稿的主路径已经分别对应落地到 `web/src/App.tsx` 和 `wechat/miniprogram/pages/` 中；后续若继续调整布局、入口顺序或状态文案，应先同步更新本目录下的预览稿和 UI 规格，再回写正式实现。

## 仓库约定

- HTML / CSS 预览源码和渲染脚本可以直接入库。
- `png` / `jpg` / `webp` 等示意图图片按本地产物处理，默认加入根目录 `.gitignore`，便于后续反复渲染但不污染仓库历史。
- 后续只要有新的前端 UI 方案，需要把可复用源码放到本目录，再按需本地渲染图片用于评审。
- 当前目录现在按四类预览组织：
- `PC / 通用浏览器预览`：对应桌面浏览器风格的登录 / 入会 / 会中设计稿。
- `H5 独立预览`：对应手机浏览器和 `iPad` 这类小屏设备的独立设计稿。
- `微信小程序预览`：对应小程序容器下的登录、加入会议、入会预览和会中壳层设计稿。
- `下一版布局提案预览`：对应尚未落地但已经形成设计文档和渲染源码的方案稿。

## 文件说明

- `meeting-auth-preview.html`：`PC / 通用浏览器` 登录 / 登录后入口 / 预定会议 / 加入会议 / 密码悬浮窗预览
- `meeting-auth-preview.css`：`PC / 通用浏览器` 入会前页面样式
- `meeting-room-preview.html`：`PC / 通用浏览器` 会中全屏房间预览
- `meeting-room-preview.css`：`PC / 通用浏览器` 会中全屏房间样式
- `h5-auth-preview.html`：`H5` 登录 / 登录后入口 / 预定会议 / 加入会议 / 密码确认 / 入会预览
- `h5-auth-preview.css`：`H5` 入会前页面样式
- `h5-room-preview.html`：`H5` 会中首屏、成员缩略条、底部工具栏和面板预览
- `h5-room-preview.css`：`H5` 会中页面样式
- `h5-ui-spec.md`：`H5` 结构化 UI 规格，约束手机与 `iPad` 的页面清单、菜单顺序和关键交互
- `wechat-auth-preview.html`：`微信小程序` 登录、首页、加入会议、密码确认和入会预览
- `wechat-auth-preview.css`：`微信小程序` 入会前页面样式
- `wechat-room-preview.html`：`微信小程序` 会中壳层和未来视频态预览
- `wechat-room-preview.css`：`微信小程序` 会中页面样式
- `wechat-ui-spec.md`：`微信小程序` 结构化 UI 规格，约束导航栏、页面顺序和会中壳层边界
- `20260325-product-ui-rollout.md`：本轮产品化 UI 落地方案、兼容实现与验证记录
- `20260327-room-fullscreen-menu-layout.md`：参考截图风格的会中单屏布局方案
- `render-previews.sh`：本地后台渲染所有设计预览的桌面端和移动端图片
- `meeting-auth-preview-desktop.png`：入会前产品预览桌面图，本地生成，不入库
- `h5-auth-preview-phone.png`：`H5` 登录与入会预览手机图，本地生成，不入库
- `h5-auth-preview-pad.png`：`H5` 登录与入会预览 `iPad` 图，本地生成，不入库
- `h5-prejoin-preview-phone.png`：`H5` 入会预览手机图，本地生成，不入库
- `h5-prejoin-preview-pad.png`：`H5` 入会预览 `iPad` 图，本地生成，不入库
- `meeting-room-preview-desktop.png`：会中产品预览桌面图，本地生成，不入库
- `h5-room-preview-phone.png`：`H5` 会中预览手机图，本地生成，不入库
- `h5-room-preview-pad.png`：`H5` 会中预览 `iPad` 图，本地生成，不入库
- `wechat-auth-preview-phone.png`：`微信小程序` 入会前预览手机图，本地生成，不入库
- `wechat-auth-preview-pad.png`：`微信小程序` 入会前预览 `Pad` 图，本地生成，不入库
- `wechat-room-preview-phone.png`：`微信小程序` 会中预览手机图，本地生成，不入库
- `wechat-room-preview-pad.png`：`微信小程序` 会中预览 `Pad` 图，本地生成，不入库
- `meeting-host-login-preview.png`：主持人视角的登录页独立渲染图，本地生成，不入库
- `meeting-logo-black.svg`：黑色风格的 `meeting` 主图标，保留 `me` 两个小写字母并带有底部聚光灯效果，适用于移动 APP、微信和网站 Logo
- `meeting-host-create-preview.png`：主持人视角的创建会议页独立渲染图，本地生成，不入库
- `meeting-host-room-preview.png`：主持人视角的进入会议页独立渲染图，本地生成，不入库

## 使用方式

直接在浏览器中打开对应的 `html` 文件即可查看预览页；样式会自动从同目录下的 `css` 文件加载。

推荐优先查看：

- `meeting-auth-preview.html`
- `meeting-room-preview.html`
- `h5-auth-preview.html`
- `h5-room-preview.html`
- `wechat-auth-preview.html`
- `wechat-room-preview.html`

前两份对应 `PC / 通用浏览器` 预览，中间两份对应独立拆分出来的 `H5` 设计稿，后两份对应微信小程序适配稿。

- `meeting-auth-preview.html` 顶部按钮可切换登录、登录后入口、预定会议、加入会议和输入会议号后的密码悬浮窗。
- `meeting-room-preview.html` 直接展示参考截图风格的会中全屏房间，默认包含昵称修改、参会者工具和主持人工具等隐藏层结构。
- `h5-auth-preview.html` 顶部按钮可切换手机 / `iPad` 视图，以及 `H5` 登录到入会预览的完整状态。
- `h5-room-preview.html` 顶部按钮可切换手机 / `iPad`、主画面 / 头像墙和聊天 / 成员 / 邀请 / 会议工具等面板状态。
- `wechat-auth-preview.html` 顶部按钮可切换手机 / `Pad` 视图，以及微信小程序的登录、首页、加入会议、密码确认和入会预览状态。
- `wechat-room-preview.html` 顶部按钮可切换手机 / `Pad`、会中壳层 / 未来视频态和成员 / 聊天 / 更多面板。

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
./docs/design/render-previews.sh h5
./docs/design/render-previews.sh h5-auth
./docs/design/render-previews.sh h5-prejoin
./docs/design/render-previews.sh h5-room
./docs/design/render-previews.sh wechat
./docs/design/render-previews.sh wechat-auth
./docs/design/render-previews.sh wechat-room
```
