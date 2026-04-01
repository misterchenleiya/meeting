import { fetchCurrentUser, loginWithWechatCode } from '../../utils/api'
import { saveSessionToken } from '../../utils/session'

Page({
  data: {
    checkingSession: true,
    loginLoading: false,
    errorMessage: '',
  },

  async onShow() {
    const app = getApp<IAppOption>()
    if (!app.globalData.sessionToken) {
      this.setData({
        checkingSession: false,
      })
      return
    }

    try {
      const response = await fetchCurrentUser()
      app.globalData.authUser = response.user
      wx.reLaunch({
        url: '/pages/home/index',
      })
      return
    } catch (_error) {
      app.globalData.sessionToken = ''
    }

    this.setData({
      checkingSession: false,
    })
  },

  handleWechatQuickLogin() {
    if (this.data.loginLoading) {
      return
    }

    this.setData({
      loginLoading: true,
      errorMessage: '',
    })

    wx.login({
      success: async (result) => {
        if (!result.code) {
          this.setData({
            loginLoading: false,
            errorMessage: '微信登录失败，请稍后重试',
          })
          return
        }

        try {
          const response = await loginWithWechatCode(result.code)
          saveSessionToken(response.sessionToken)
          const app = getApp<IAppOption>()
          app.globalData.authUser = response.user
          wx.reLaunch({
            url: '/pages/home/index',
          })
        } catch (error) {
          this.setData({
            errorMessage: error instanceof Error ? error.message : '微信登录失败，请稍后重试',
          })
        } finally {
          this.setData({
            loginLoading: false,
          })
        }
      },
      fail: () => {
        this.setData({
          loginLoading: false,
          errorMessage: '微信登录失败，请稍后重试',
        })
      },
    })
  },
})
