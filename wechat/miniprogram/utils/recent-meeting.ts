export type RecentMeetingSummary = {
  meetingId: string
  meetingNumber: string
  title: string
  passwordRequired: boolean
  updatedAt: string
}

const storageKey = 'meeting:miniprogram:recent-meeting'

export function readRecentMeetingSummary(): RecentMeetingSummary | null {
  try {
    const raw = wx.getStorageSync(storageKey)
    if (!raw || typeof raw !== 'object') {
      return null
    }

    const candidate = raw as Partial<RecentMeetingSummary>
    if (
      typeof candidate.meetingId !== 'string' ||
      typeof candidate.meetingNumber !== 'string' ||
      typeof candidate.title !== 'string' ||
      typeof candidate.passwordRequired !== 'boolean' ||
      typeof candidate.updatedAt !== 'string'
    ) {
      return null
    }

    return {
      meetingId: candidate.meetingId,
      meetingNumber: candidate.meetingNumber,
      title: candidate.title,
      passwordRequired: candidate.passwordRequired,
      updatedAt: candidate.updatedAt,
    }
  } catch (error) {
    console.warn('recent_meeting_read_failed', error)
    return null
  }
}

export function writeRecentMeetingSummary(summary: RecentMeetingSummary) {
  try {
    wx.setStorageSync(storageKey, summary)
  } catch (error) {
    console.warn('recent_meeting_write_failed', error)
  }
}
