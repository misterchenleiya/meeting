import { joinMeeting } from '../../utils/api'
import { writeRecentMeetingSummary } from '../../utils/recent-meeting'

type PrejoinPayload = {
  meetingId: string
  meetingTitle: string
  meetingNumber: string
  meetingNumberInput: string
  nicknameInput: string
  passwordInput: string
  passwordRequired: boolean
  requestCameraEnabled: boolean
  requestMicrophoneEnabled: boolean
}

Page({
  data: {
    meetingId: '',
    meetingTitle: '',
    meetingNumber: '',
    meetingNumberInput: '',
    nicknameInput: '',
    nicknameInitial: '会',
    passwordInput: '',
    passwordRequired: false,
    requestCameraEnabled: false,
    requestMicrophoneEnabled: false,
    joining: false,
    errorMessage: '',
    statusMessage: '',
  },

  onLoad() {
    const eventChannel = this.getOpenerEventChannel()
    eventChannel.on('acceptPrejoinPayload', (payload: PrejoinPayload) => {
      this.setData({
        meetingId: payload.meetingId,
        meetingTitle: payload.meetingTitle,
        meetingNumber: payload.meetingNumber,
        meetingNumberInput: payload.meetingNumberInput,
        nicknameInput: payload.nicknameInput,
        nicknameInitial: buildInitial(payload.nicknameInput),
        passwordInput: payload.passwordInput,
        passwordRequired: payload.passwordRequired,
        requestCameraEnabled: payload.requestCameraEnabled,
        requestMicrophoneEnabled: payload.requestMicrophoneEnabled,
      })
    })
  },

  handleToggleCameraPreference() {
    this.setData({
      requestCameraEnabled: !this.data.requestCameraEnabled,
      errorMessage: '',
    })
  },

  handleToggleMicrophonePreference() {
    this.setData({
      requestMicrophoneEnabled: !this.data.requestMicrophoneEnabled,
      errorMessage: '',
    })
  },

  handleBackToJoin() {
    const eventChannel = this.getOpenerEventChannel()
    eventChannel.emit('prejoinUpdated', {
      meetingId: this.data.meetingId,
      meetingTitle: this.data.meetingTitle,
      meetingNumber: this.data.meetingNumber,
      meetingNumberInput: this.data.meetingNumberInput,
      nicknameInput: this.data.nicknameInput,
      passwordInput: this.data.passwordInput,
      passwordRequired: this.data.passwordRequired,
      requestCameraEnabled: this.data.requestCameraEnabled,
      requestMicrophoneEnabled: this.data.requestMicrophoneEnabled,
    } satisfies Partial<PrejoinPayload>)

    wx.navigateBack()
  },

  async handleEnterMeeting() {
    if (this.data.joining || !this.data.meetingId) {
      return
    }

    if (!this.data.nicknameInput.trim()) {
      this.setData({
        errorMessage: '未读取到有效昵称，请返回上一页修改',
      })
      return
    }

    this.setData({
      joining: true,
      errorMessage: '',
      statusMessage: '正在进入会议...',
    })

    try {
      const response = await joinMeeting({
        meetingId: this.data.meetingId,
        password: this.data.passwordInput,
        nickname: this.data.nicknameInput.trim(),
        requestCameraEnabled: this.data.requestCameraEnabled,
        requestMicrophoneEnabled: this.data.requestMicrophoneEnabled,
      })

      writeRecentMeetingSummary({
        meetingId: response.meeting.id,
        meetingNumber: this.data.meetingNumber,
        title: response.meeting.title,
        passwordRequired: !!response.meeting.passwordRequired,
        updatedAt: new Date().toLocaleTimeString('zh-CN', { hour12: false }),
      })

      wx.redirectTo({
        url:
          `/pages/meeting-room/index?meetingId=${encodeURIComponent(response.meeting.id)}` +
          `&meetingNumber=${encodeURIComponent(this.data.meetingNumber)}` +
          `&meetingTitle=${encodeURIComponent(response.meeting.title)}` +
          `&participantId=${encodeURIComponent(response.participant.id)}` +
          `&participantNickname=${encodeURIComponent(response.participant.nickname)}` +
          `&participantRole=${encodeURIComponent(response.participant.role)}` +
          `&requestCameraEnabled=${this.data.requestCameraEnabled ? '1' : '0'}` +
          `&requestMicrophoneEnabled=${this.data.requestMicrophoneEnabled ? '1' : '0'}`,
      })
      return
    } catch (error) {
      this.setData({
        joining: false,
        errorMessage: error instanceof Error ? error.message : '进入会议失败',
        statusMessage: '',
      })
      return
    }
  },
})

function buildInitial(value: string) {
  const trimmed = value.trim()
  return trimmed ? trimmed.slice(0, 1) : '会'
}
