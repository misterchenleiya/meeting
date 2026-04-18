import { fetchCurrentUser, logout } from '../../utils/api'
import { readRecentMeetingSummary } from '../../utils/recent-meeting'
import { clearSessionToken } from '../../utils/session'

Page({
  data: {
    loading: true,
    nickname: '',
    email: '',
    recentMeeting: null as null | {
      meetingId: string
      meetingNumber: string
      title: string
      passwordRequired: boolean
      updatedAt: string
    },
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
        recentMeeting: readRecentMeetingSummary(),
      })
    } catch (error) {
      console.warn('home_fetch_current_user_failed', error)
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

  handleContinueRecentMeeting() {
    const recentMeeting = this.data.recentMeeting
    if (!recentMeeting) {
      return
    }

    wx.navigateTo({
      url: `/pages/join/index?meetingNumber=${encodeURIComponent(recentMeeting.meetingNumber)}`,
    })
  },

  async handleLogout() {
    try {
      await logout()
    } catch (error) {
      console.warn('home_logout_failed', error)
    }

    clearSessionToken()
    wx.reLaunch({
      url: '/pages/index/index',
    })
  },
})
