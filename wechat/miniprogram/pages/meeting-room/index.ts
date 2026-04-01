import { leaveMeeting } from '../../utils/api'

Page({
  data: {
    meetingId: '',
    meetingNumber: '',
    meetingTitle: '',
    participantId: '',
    participantNickname: '',
    participantRole: '',
    leaving: false,
  },

  onLoad(query: Record<string, string | undefined>) {
    this.setData({
      meetingId: query.meetingId || '',
      meetingNumber: query.meetingNumber || '',
      meetingTitle: query.meetingTitle || 'meeting',
      participantId: query.participantId || '',
      participantNickname: query.participantNickname || '',
      participantRole: query.participantRole || '',
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
    } catch (_error) {
      // Ignore transport errors here and still let the user return to the shell.
    }

    wx.reLaunch({
      url: '/pages/home/index',
    })
  },
})
