export type AuthUser = {
  id: string
  email: string
  nickname: string
  emailVerifiedAt?: string
  createdAt: string
  updatedAt: string
}

export type AuthLoginResponse = {
  status: string
  user: AuthUser
  sessionToken: string
  sessionEndsAt: string
  autoRegistered?: boolean
  loginMethod?: string
}

export type Meeting = {
  id: string
  meetingNumber: string
  title: string
  passwordRequired: boolean
  status: string
  participants: Record<string, Participant>
}

export type Participant = {
  id: string
  nickname: string
  role: 'host' | 'assistant' | 'participant'
}
