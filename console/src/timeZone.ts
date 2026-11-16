import { api, Settings } from './api'

// Agents run in the person's time zone, so "every morning at 8" means 8 where
// they live. The engine starts without one (UTC); the first browser that
// opens the console fills it in. People change it on the Add an agent page.
export const browserTimeZone = () => {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone || ''
  } catch {
    return ''
  }
}

export const canSetTimeZone = (s: Settings) => (s.editable || []).includes('agents.time_zone')

export async function syncAgentTimeZone(zone = browserTimeZone()): Promise<string> {
  const s = await api.getSettings()
  const current = s.agents.time_zone || ''
  if (current || !zone || zone === 'UTC' || !canSetTimeZone(s)) return current
  const next = await api.patchSettings({ agents: { time_zone: zone } })
  return next.agents.time_zone || ''
}
