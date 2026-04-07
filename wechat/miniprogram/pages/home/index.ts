import { fetchCurrentUser, logout } from '../../utils/api'
import { clearSessionToken } from '../../utils/session'

Page({
  data: {
    loading: true,
    nickname: '',
    email: '',
  },

  async onShow() {
    try {
      const response = await fetchCurrentUser()
      const app = getApp<IAppOption>()
      app.globalData.authUser = response.user
      this.setData({
        loading: false,
        nickname: response.user.nickname,
        email: response.user.email || '微信快捷登录用户',
      })
    } catch (_error) {
      clearSessionToken()
      wx.reLaunch({
        url: '/pages/index/index',
      })
    }
  },

  handleJoinMeeting() {
    wx.navigateTo({
      url: '/pages/join/index',
    })
  },

  async handleLogout() {
    try {
      await logout()
    } catch (_error) {
      // Ignore logout transport errors and clear local session anyway.
    }

    clearSessionToken()
    wx.reLaunch({
      url: '/pages/index/index',
    })
  },
})
