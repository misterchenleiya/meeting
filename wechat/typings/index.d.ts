/// <reference path="./types/index.d.ts" />

interface IAppOption extends WechatMiniprogram.IAnyObject {
  globalData: {
    apiBaseURL: string,
    authUser?: {
      id: string,
      email: string,
      nickname: string,
      emailVerifiedAt?: string,
      createdAt: string,
      updatedAt: string,
    },
    sessionToken?: string,
  }
}
