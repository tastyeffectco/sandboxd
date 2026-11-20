import { useEffect, useState } from 'react'
import { api, App as TApp } from './api'
import { AppView } from './AppView'

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
  useEffect(() => {
    let live = true
    api
      .ensureAssistant()
      .then((a) => live && setAppId(a.app_id))
      .catch((e) => live && setFailed((e as Error).message))
    return () => {
      live = false
    }
  }, [])
  if (failed) {
    return (
      <div className="content-page" data-testid="assistant-failed">
        <h1>Your sandboxd agent could not start</h1>
        <p>{failed}</p>
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
