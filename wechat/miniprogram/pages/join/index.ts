import { fetchCurrentUser, getMeeting, joinMeeting } from '../../utils/api'

Page({
  data: {
    meetingNumberInput: '',
    passwordInput: '',
    loading: false,
    errorMessage: '',
    meetingId: '',
    meetingTitle: '',
    meetingNumber: '',
    passwordRequired: false,
  },

  onMeetingNumberInput(e: WechatMiniprogram.Input) {
    this.setData({
      meetingNumberInput: e.detail.value,
    })
  },

  onPasswordInput(e: WechatMiniprogram.Input) {
    this.setData({
      passwordInput: e.detail.value,
    })
  },

  async handleLookupMeeting() {
    if (this.data.loading) {
      return
    }

    const meetingNumber = normalizeMeetingNumber(this.data.meetingNumberInput)
    if (!/^\d{9}$/.test(meetingNumber)) {
      this.setData({
        errorMessage: '请输入 9 位会议号',
      })
      return
    }

    this.setData({
      loading: true,
      errorMessage: '',
    })

    try {
      const response = await getMeeting(meetingNumber)
      this.setData({
        loading: false,
        meetingId: response.meeting.id,
        meetingTitle: response.meeting.title,
        meetingNumber: formatMeetingNumber(meetingNumber),
        passwordRequired: !!response.meeting.passwordRequired,
      })
    } catch (error) {
      this.setData({
        loading: false,
        errorMessage: error instanceof Error ? error.message : '查询会议失败',
      })
    }
  },

  async handleJoinMeeting() {
    if (this.data.loading || !this.data.meetingId) {
      return
    }

    this.setData({
      loading: true,
      errorMessage: '',
    })

    try {
      const currentUser = await fetchCurrentUser()
      const response = await joinMeeting({
        meetingId: this.data.meetingId,
        password: this.data.passwordInput,
        nickname: currentUser.user.nickname,
      })

      wx.redirectTo({
        url:
          `/pages/meeting-room/index?meetingId=${encodeURIComponent(response.meeting.id)}` +
          `&meetingNumber=${encodeURIComponent(formatMeetingNumber(response.meeting.meetingNumber || this.data.meetingNumberInput))}` +
          `&meetingTitle=${encodeURIComponent(response.meeting.title)}` +
          `&participantId=${encodeURIComponent(response.participant.id)}` +
          `&participantNickname=${encodeURIComponent(response.participant.nickname)}` +
          `&participantRole=${encodeURIComponent(response.participant.role)}`,
      })
    } catch (error) {
      this.setData({
        loading: false,
        errorMessage: error instanceof Error ? error.message : '加入会议失败',
      })
      return
    }

    this.setData({
      loading: false,
    })
  },
})

function normalizeMeetingNumber(value: string) {
  return value.replace(/\s+/g, '').trim()
}

function formatMeetingNumber(value: string) {
  const normalized = normalizeMeetingNumber(value)
  if (normalized.length !== 9) {
    return value
  }

  return `${normalized.slice(0, 3)} ${normalized.slice(3, 6)} ${normalized.slice(6, 9)}`
}
