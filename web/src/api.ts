export interface Channel {
  id: string
  name: string
  genre: string
  desc: string
  url: string
  comment: string
  bitrate: number
  contentType: string
  track: { title: string; artist: string; album: string; contact: string }
  tracker: { ip: string; port: number; firewalled: boolean }
  numListeners: number
  numRelays: number
  upTime: number
}

export interface Session {
  id: number
  channelId: string
  channelName: string
  bitrate: number
  contentType: string
  genre: string
  url: string
  startedAt: string
  endedAt: string | null
  durationMin: number
}

export interface ActivityDay {
  date: string    // "YYYY-MM-DD"
  minutes: number
}

export interface SiteConfig {
  ypIndexURL: string
  pcpAddress: string
  genrePrefix: string
}

export interface TimelineRow {
  recordedAt: string
  listeners: number
  relays: number
  changed: boolean
  name?: string
  genre?: string
  description?: string
  url?: string
  comment?: string
  trackTitle?: string
  trackArtist?: string
}

async function get<T>(path: string): Promise<T> {
  const res = await fetch(path)
  if (!res.ok) throw new Error(`${res.status} ${res.statusText}`)
  return res.json()
}

export const api = {
  config: () =>
    get<SiteConfig>('/yp/api/config'),
  channels: () =>
    get<Channel[]>('/yp/api/channels'),
  history: (limit = 50, offset = 0) =>
    get<Session[]>(`/yp/api/history?limit=${limit}&offset=${offset}`),
  activity: (name: string) =>
    get<ActivityDay[]>(`/yp/api/channels/activity?name=${encodeURIComponent(name)}`),
  timeline: (name: string, date: string) =>
    get<TimelineRow[]>(`/yp/api/channels/timeline?name=${encodeURIComponent(name)}&date=${date}`),
}
