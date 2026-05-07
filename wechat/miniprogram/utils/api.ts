import { requestJSON } from './request'
import type { AuthLoginResponse, AuthUser, Meeting, Participant } from './types'

export function loginWithWechatCode(code: string) {
  return requestJSON<AuthLoginResponse>('/api/auth/wechat/mini/login', {
    method: 'POST',
    data: { code },
  })
}

export function fetchCurrentUser() {
  return requestJSON<{ user: AuthUser; sessionEndsAt: string }>('/api/auth/me')
}

export function logout() {
  return requestJSON<{ status: string }>('/api/auth/logout', {
    method: 'POST',
  })
}

export function getMeeting(meetingID: string) {
  return requestJSON<{ meeting: Meeting }>(`/api/meetings/${meetingID}`)
}

export function joinMeeting(input: {
  meetingId: string
  password: string
  nickname: string
  requestCameraEnabled?: boolean
  requestMicrophoneEnabled?: boolean
}) {
  return requestJSON<{ meeting: Meeting; participant: Participant }>(`/api/meetings/${input.meetingId}/join`, {
    method: 'POST',
    data: {
      password: input.password,
      nickname: input.nickname,
      deviceType: 'wechat_miniprogram',
      isAnonymous: false,
      requestCameraEnabled: input.requestCameraEnabled,
      requestMicrophoneEnabled: input.requestMicrophoneEnabled,
    },
  })
}

export function leaveMeeting(input: {
  meetingId: string
  participantId: string
}) {
  return requestJSON<{ status: string }>(
    `/api/meetings/${input.meetingId}/participants/${input.participantId}/leave`,
    {
      method: 'POST',
      data: {
        deviceType: 'wechat_miniprogram',
      },
    }
  )
}
