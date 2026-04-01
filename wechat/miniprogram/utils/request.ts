import { getStoredSessionToken } from './session'

type RequestOptions = {
  method?: WechatMiniprogram.RequestOption['method']
  data?: Record<string, unknown>
}

export function requestJSON<T>(path: string, options: RequestOptions = {}): Promise<T> {
  const app = getApp<IAppOption>()
  const token = getStoredSessionToken()

  return new Promise((resolve, reject) => {
    wx.request({
      url: `${app.globalData.apiBaseURL}${path}`,
      method: options.method ? options.method : 'GET',
      data: options.data,
      timeout: 10000,
      header: {
        'content-type': 'application/json',
        ...(token ? { Authorization: `Bearer ${token}` } : {}),
      },
      success: (response) => {
        const payload = response.data as { error?: string }
        if (response.statusCode >= 200 && response.statusCode < 300) {
          resolve(response.data as T)
          return
        }

        reject(
          new Error(
            payload && payload.error
              ? payload.error
              : `Request failed with status ${response.statusCode}`
          )
        )
      },
      fail: (error) => {
        reject(new Error(error.errMsg || 'request failed'))
      },
    })
  })
}
