import { leaveMeeting } from '../../utils/api'

Page({
  data: {
    meetingId: '',
    meetingNumber: '',
    meetingTitle: '',
    participantId: '',
    participantNickname: '',
    participantInitial: '会',
    participantRole: '',
    requestCameraEnabled: false,
    requestMicrophoneEnabled: false,
    currentPanel: 'none',
    stageMode: 'shell',
    statusMessage: '',
    errorMessage: '',
    leaving: false,
  },

  onLoad(query: Record<string, string | undefined>) {
    this.setData({
      meetingId: query.meetingId || '',
      meetingNumber: query.meetingNumber || '',
      meetingTitle: query.meetingTitle || 'meeting',
      participantId: query.participantId || '',
      participantNickname: query.participantNickname || '',
      participantInitial: buildInitial(query.participantNickname || ''),
      participantRole: query.participantRole || '',
      requestCameraEnabled: query.requestCameraEnabled === '1',
      requestMicrophoneEnabled: query.requestMicrophoneEnabled === '1',
    })
  },

  handleTogglePanel(e: WechatMiniprogram.TouchEvent) {
    const nextPanel = e.currentTarget.dataset.panel
    if (
      nextPanel !== 'members' &&
      nextPanel !== 'chat' &&
      nextPanel !== 'actions'
    ) {
      return
    }

    this.setData({
      currentPanel: this.data.currentPanel === nextPanel ? 'none' : nextPanel,
    })
  },

  handleClosePanel() {
    this.setData({
      currentPanel: 'none',
    })
  },

  handleToggleStageMode() {
    this.setData({
      stageMode: this.data.stageMode === 'shell' ? 'video' : 'shell',
      statusMessage: '已切换视图',
    })
  },

  handleActionTap(e: WechatMiniprogram.TouchEvent) {
    const action = e.currentTarget.dataset.action
    if (action === 'copy') {
      wx.setClipboardData({
        data: this.data.meetingNumber,
      })
      this.setData({
        statusMessage: '会议号已复制',
      })
      return
    }

    const actionLabel =
      action === 'invite'
        ? '邀请参会者'
        : action === 'profile'
          ? '身份信息'
          : action === 'settings'
            ? '更多设置'
            : '更多操作'

    this.setData({
      statusMessage:
        action === 'profile'
          ? `${actionLabel}：${this.data.participantRole || '参会者'}`
          : `${actionLabel} 即将开放`,
    })
  },

  async handleLeaveMeeting() {
    if (this.data.leaving || !this.data.meetingId || !this.data.participantId) {
      return
    }

    this.setData({
      leaving: true,
    })

    try {
      await leaveMeeting({
        meetingId: this.data.meetingId,
        participantId: this.data.participantId,
      })
    } catch (error) {
      console.warn('meeting_room_leave_failed', error)
      wx.showToast({
        title: '离会请求未确认，已返回首页',
        icon: 'none',
      })
    }

    wx.reLaunch({
      url: '/pages/home/index',
    })
  },
})

function buildInitial(value: string) {
  const trimmed = value.trim()
  return trimmed ? trimmed.slice(0, 1) : '会'
}
