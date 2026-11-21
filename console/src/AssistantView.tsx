import { useEffect, useState } from 'react'
import { api, App as TApp } from './api'
import { AppView } from './AppView'
import { Btn } from './design/kit'

// The project that holds the person's sandboxd agent; kept out of the project list.
export const ASSISTANT_TAG = 'sandboxd:assistant'

// "Ask sandboxd": one chat with the person's main agent. It is the coding
// agent of a hidden project, given sandboxd's tools: it builds and installs
// apps, adds agents, gives them tasks and reads their memories. Opening it
// makes it on first use and refreshes its tool settings.
export function AssistantView({
  onError,
  toast,
  goApps,
  goSettings,
  apps,
  onOpenApp,
  reloadApps,
}: {
  reloadApps?: () => void
  onError: (m: string) => void
  toast: (m: string) => void
  goApps: () => void
  goSettings: () => void
  apps?: TApp[]
  onOpenApp?: (id: string, tab?: string) => void
}) {
  const [appId, setAppId] = useState('')
  const [failed, setFailed] = useState('')
  const [attempt, setAttempt] = useState(0)
  useEffect(() => {
    let live = true
    setFailed('')
    api
      .ensureAssistant()
      .then((a) => live && setAppId(a.app_id))
      .catch((e: Error & { status?: number }) => {
        if (!live) return
        // An engine from before the sandboxd agent has no /v1/assistant:
        // open the projects page, where building an app works, instead of
        // showing an error the person can do nothing about.
        if (e.status === 404) goApps()
        else setFailed(e.message)
      })
    return () => {
      live = false
    }
  }, [attempt])
  if (failed) {
    return (
      <div className="content-page" data-testid="assistant-failed">
        <h1>sandboxd isn't ready yet</h1>
        <p>{failed}</p>
        <div style={{ display: 'flex', gap: 8, marginTop: 16 }}>
          <Btn variant="primary" onClick={() => setAttempt((n) => n + 1)}>
            Try again
          </Btn>
          <Btn variant="ghost" onClick={goApps}>
            Build an app instead
          </Btn>
        </div>
      </div>
    )
  }
  if (!appId) {
    return (
      <div className="content-page" data-testid="assistant-loading">
        <p>Getting your sandboxd agent ready…</p>
      </div>
    )
  }
  return (
    <AppView
      assistant
      appId={appId}
      onError={onError}
      toast={toast}
      goApps={goApps}
      goSettings={goSettings}
      apps={apps}
      onOpenApp={onOpenApp}
      reloadApps={reloadApps}
    />
  )
}
