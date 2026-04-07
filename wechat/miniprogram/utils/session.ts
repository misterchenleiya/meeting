const sessionTokenStorageKey = 'meeting_session_token'

export function getStoredSessionToken(): string {
  return wx.getStorageSync(sessionTokenStorageKey) || ''
}

export function saveSessionToken(token: string) {
  wx.setStorageSync(sessionTokenStorageKey, token)
  getApp<IAppOption>().globalData.sessionToken = token
}

export function clearSessionToken() {
  wx.removeStorageSync(sessionTokenStorageKey)
  getApp<IAppOption>().globalData.sessionToken = ''
  delete getApp<IAppOption>().globalData.authUser
}
