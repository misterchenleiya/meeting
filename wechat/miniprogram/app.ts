import { getStoredSessionToken } from './utils/session'
import { getMiniProgramApiBaseURL } from './utils/runtime'

App<IAppOption>({
  globalData: {
    apiBaseURL: getMiniProgramApiBaseURL(),
    sessionToken: '',
  },
  onLaunch() {
    const sessionToken = getStoredSessionToken()
    if (sessionToken) {
      this.globalData.sessionToken = sessionToken
    }
  },
})
