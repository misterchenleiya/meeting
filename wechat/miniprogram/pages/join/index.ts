import { fetchCurrentUser, getMeeting } from '../../utils/api'
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
    meetingNumberInput: '',
    nicknameInput: '',
    passwordInput: '',
    statusMessage: '',
    loading: false,
    errorMessage: '',
    meetingId: '',
    meetingTitle: '',
    meetingNumber: '',
    passwordRequired: false,
    showPasswordSheet: false,
    requestCameraEnabled: true,
    requestMicrophoneEnabled: false,
  },

  onLoad(query: Record<string, string | undefined>) {
    if (query.meetingNumber) {
      this.setData({
        meetingNumberInput: query.meetingNumber,
      })
    }
  },

  async onShow() {
    if (this.data.nicknameInput) {
      return
    }

    try {
      const response = await fetchCurrentUser()
      this.setData({
        nicknameInput: response.user.nickname,
      })
    } catch (error) {
      console.warn('join_fetch_current_user_failed', error)
      this.setData({
        statusMessage: '未读取到最新昵称，将沿用当前输入',
      })
    }
  },

  onMeetingNumberInput(e: WechatMiniprogram.Input) {
    this.setData({
      meetingNumberInput: e.detail.value,
      meetingId: '',
      meetingTitle: '',
      meetingNumber: '',
      passwordRequired: false,
      showPasswordSheet: false,
      errorMessage: '',
    })
  },

  handleScanMeetingQRCode() {
    if (this.data.loading) {
      return
    }

    wx.scanCode({
      onlyFromCamera: true,
      scanType: ['qrCode'],
      success: (result) => {
        const payload = parseMeetingQRCodePayload(result.result)
        if (!payload.meetingLookupValue) {
          this.setData({
            errorMessage: '未识别到有效的会议二维码',
            statusMessage: '',
          })
          return
        }

        this.setData({
          meetingNumberInput: formatMeetingLookupDisplay(payload.meetingLookupValue),
          passwordInput: payload.password,
          meetingId: '',
          meetingTitle: '',
          meetingNumber: '',
          passwordRequired: false,
          showPasswordSheet: false,
          errorMessage: '',
          statusMessage: payload.password
            ? '已扫码识别会议号和密码'
            : '已扫码识别会议号',
        })
      },
      fail: (error) => {
        if (typeof error.errMsg === 'string' && error.errMsg.includes('cancel')) {
          return
        }

        this.setData({
          errorMessage: '无法启动扫码，请检查摄像头权限',
          statusMessage: '',
        })
      },
    })
  },

  onPasswordInput(e: WechatMiniprogram.Input) {
    this.setData({
      passwordInput: e.detail.value,
    })
  },

  onNicknameInput(e: WechatMiniprogram.Input) {
    this.setData({
      nicknameInput: e.detail.value,
    })
  },

  handleToggleCameraPreference() {
    this.setData({
      requestCameraEnabled: !this.data.requestCameraEnabled,
    })
  },

  handleToggleMicrophonePreference() {
    this.setData({
      requestMicrophoneEnabled: !this.data.requestMicrophoneEnabled,
    })
  },

  async handleLookupMeeting() {
    if (this.data.loading) {
      return
    }

    const meetingLookupValue = normalizeMeetingLookupValue(this.data.meetingNumberInput)
    if (!isSupportedMeetingLookupValue(meetingLookupValue)) {
      this.setData({
        errorMessage: '请输入 9 位会议号',
      })
      return
    }

    if (!this.data.nicknameInput.trim()) {
      this.setData({
        errorMessage: '请输入昵称',
      })
      return
    }

    this.setData({
      loading: true,
      errorMessage: '',
      statusMessage: '',
    })

    try {
      const response = await getMeeting(meetingLookupValue)
      const formattedMeetingNumber = formatMeetingLookupDisplay(
        response.meeting.meetingNumber || meetingLookupValue
      )
      writeRecentMeetingSummary({
        meetingId: response.meeting.id,
        meetingNumber: formattedMeetingNumber,
        title: response.meeting.title,
        passwordRequired: !!response.meeting.passwordRequired,
        updatedAt: new Date().toLocaleTimeString('zh-CN', { hour12: false }),
      })

      this.setData({
        loading: false,
        meetingId: response.meeting.id,
        meetingTitle: response.meeting.title,
        meetingNumber: formattedMeetingNumber,
        passwordRequired: !!response.meeting.passwordRequired,
        showPasswordSheet: !!response.meeting.passwordRequired,
        statusMessage: response.meeting.passwordRequired
          ? '会议号已验证，请继续输入会议密码'
          : '会议号已验证',
      })

      if (!response.meeting.passwordRequired) {
        this.navigateToPrejoin('')
      }
    } catch (error) {
      this.setData({
        loading: false,
        errorMessage: error instanceof Error ? error.message : '查询会议失败',
      })
    }
  },

  handleClosePasswordSheet() {
    this.setData({
      showPasswordSheet: false,
    })
  },

  handleContinueToPreview() {
    if (this.data.loading || !this.data.meetingId) {
      return
    }

    if (this.data.passwordRequired && !this.data.passwordInput.trim()) {
      this.setData({
        errorMessage: '请输入会议密码',
      })
      return
    }

    this.navigateToPrejoin(this.data.passwordInput.trim())
  },

  handleBackHome() {
    wx.navigateBack({
      fail: () => {
        wx.reLaunch({
          url: '/pages/home/index',
        })
      },
    })
  },

  navigateToPrejoin(passwordInput: string) {
    const payload: PrejoinPayload = {
      meetingId: this.data.meetingId,
      meetingTitle: this.data.meetingTitle,
      meetingNumber: this.data.meetingNumber,
      meetingNumberInput: this.data.meetingNumberInput,
      nicknameInput: this.data.nicknameInput,
      passwordInput,
      passwordRequired: this.data.passwordRequired,
      requestCameraEnabled: this.data.requestCameraEnabled,
      requestMicrophoneEnabled: this.data.requestMicrophoneEnabled,
    }

    wx.navigateTo({
      url: '/pages/prejoin/index',
      success: (result) => {
        result.eventChannel.emit('acceptPrejoinPayload', payload)
        result.eventChannel.on('prejoinUpdated', (nextPayload: Partial<PrejoinPayload>) => {
          this.setData({
            meetingId: nextPayload.meetingId ?? this.data.meetingId,
            meetingTitle: nextPayload.meetingTitle ?? this.data.meetingTitle,
            meetingNumber: nextPayload.meetingNumber ?? this.data.meetingNumber,
            meetingNumberInput: nextPayload.meetingNumberInput ?? this.data.meetingNumberInput,
            nicknameInput: nextPayload.nicknameInput ?? this.data.nicknameInput,
            passwordInput: nextPayload.passwordInput ?? this.data.passwordInput,
            passwordRequired: nextPayload.passwordRequired ?? this.data.passwordRequired,
            requestCameraEnabled:
              nextPayload.requestCameraEnabled ?? this.data.requestCameraEnabled,
            requestMicrophoneEnabled:
              nextPayload.requestMicrophoneEnabled ?? this.data.requestMicrophoneEnabled,
            showPasswordSheet: false,
            statusMessage: '已返回加入页',
            errorMessage: '',
          })
        })
      },
    })
  },
})

function normalizeMeetingNumber(value: string) {
  return value.replace(/\s+/g, '').trim()
}

function normalizeMeetingLookupValue(value: string) {
  const trimmed = value.trim()
  if (!trimmed) {
    return ''
  }

  const normalizedMeetingNumber = normalizeMeetingNumber(trimmed)
  if (/^\d{9}$/.test(normalizedMeetingNumber)) {
    return normalizedMeetingNumber
  }

  return trimmed
}

function isSupportedMeetingLookupValue(value: string) {
  return /^\d{9}$/.test(normalizeMeetingNumber(value)) || /^[A-Za-z0-9][A-Za-z0-9_-]{5,}$/.test(value)
}

function formatMeetingLookupDisplay(value: string) {
  const normalized = normalizeMeetingNumber(value)
  if (normalized.length !== 9) {
    return value.trim()
  }

  return `${normalized.slice(0, 3)} ${normalized.slice(3, 6)} ${normalized.slice(6, 9)}`
}

function parseMeetingQRCodePayload(payloadText: string) {
  const trimmed = payloadText.trim()
  if (!trimmed) {
    return {
      meetingLookupValue: '',
      password: '',
    }
  }

  const meetingLookupValue = normalizeMeetingLookupValue(
    readQueryParameter(trimmed, 'meetingNumber') || readQueryParameter(trimmed, 'meetingId') || trimmed
  )

  return {
    meetingLookupValue,
    password: readQueryParameter(trimmed, 'password'),
  }
}

function readQueryParameter(payloadText: string, key: string) {
  const match = payloadText.match(new RegExp(`[?&#]${key}=([^&#]+)`, 'i'))
  if (!match || !match[1]) {
    return ''
  }

  try {
    return decodeURIComponent(match[1].replace(/\+/g, ' '))
  } catch (_error) {
    return match[1]
  }
}
