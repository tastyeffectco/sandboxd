import { useCallback, useEffect, useState } from 'react'
import App from './App'
import type { DesktopConnection, DesktopState } from './desktop'
import { Icon } from './design/Icon'
import { Btn, Input } from './design/kit'
import { Dialog } from './design/Dialog'
import { confirmLeaveWorkspace } from './unsavedChanges'
import './desktop.css'

const bridge = window.sandboxdDesktop
const local = { name: 'This computer', url: 'http://127.0.0.1:9090', allowInsecure: false }

export default function DesktopHost() {
  const [state, setState] = useState<DesktopState | null>(null)
  const [connections, setConnections] = useState(false)
  const [connecting, setConnecting] = useState(false)
  const [revision, setRevision] = useState(0)
  const [online, setOnline] = useState(true)
  const [error, setError] = useState('')
  const probe = useCallback(async () => {
    try {
      const response = await fetch('/v1/auth/status', { signal: AbortSignal.timeout(5000) })
      setOnline(response.ok)
    } catch {
      setOnline(false)
    }
  }, [])
  useEffect(() => {
    if (!bridge) {
      setError('Open this build with the sandboxd desktop app.')
      return
    }
    void bridge
      .state()
      .then(setState)
      .catch((e) => setError(e.message))
    const remove = [
      bridge.onConnections(() => setConnections(true)),
      bridge.onNavigate((route) => {
        location.hash = `#/${route}`
      }),
      bridge.onSearch(() => window.dispatchEvent(new KeyboardEvent('keydown', { key: 'k', ctrlKey: true }))),
      bridge.onEngineProgress((localEngine) =>
        setState((previous) => previous && { ...previous, localEngine }),
      ),
    ]
    return () => remove.forEach((unsubscribe) => unsubscribe())
  }, [])
  useEffect(() => {
    if (!state?.selected) return
    void probe()
    const timer = setInterval(() => void probe(), 15000)
    return () => clearInterval(timer)
  }, [state?.selected, probe])
  const selected = state?.connections.find((c) => c.url === state.selected)
  const connected = (next: DesktopState) => {
    setError('')
    setState(next)
    setRevision((n) => n + 1)
    setConnections(false)
    setOnline(true)
    location.hash = '#/apps'
  }
  return (
    <div className={`desktop-client platform-${state?.platform || 'unknown'}`}>
      <header className="desktop-titlebar">
        <span className="desktop-wordmark">
          <Icon name="box" size={16} /> sandboxd <small>STUDIO</small>
        </span>
        {selected && (
          <button className="desktop-engine-pill" onClick={() => setConnections(true)} title={selected.url}>
            <span className={online ? 'engine-status online' : 'engine-status'} />
            {selected.name}
            <Icon name="chevron" size={12} />
          </button>
        )}
      </header>
      {!online && selected && (
        <div className="desktop-offline" role="status">
          <span>
            {state?.localEngine?.busy
              ? state.localEngine.message
              : `Cannot reach ${selected.name}. Your workspace is still on the engine.`}
          </span>
          <button
            className="text-button"
            onClick={() => {
              if (confirmLeaveWorkspace()) {
                setRevision((n) => n + 1)
                void probe()
              }
            }}
          >
            Retry
          </button>
          <button className="text-button" onClick={() => setConnections(true)}>
            Connections
          </button>
        </div>
      )}
      <div className="desktop-content">
        {error ? (
          <div className="desktop-start-error" role="alert">
            <h1>Let’s reconnect.</h1>
            <p>{error}</p>
          </div>
        ) : !state ? (
          <div className="desktop-start-error" role="status">
            Opening your studio…
          </div>
        ) : state.selected ? (
          <App key={`${state.selected}:${revision}`} />
        ) : (
          <div className="desktop-welcome">
            <aside className="desktop-welcome-story">
              <div className="desktop-app-mark">
                <Icon name="box" size={36} />
              </div>
              <span className="eyebrow">A LITTLE SPACE. BIG POSSIBILITIES.</span>
              <h1>
                Your ideas,
                <br />
                at home.
              </h1>
              <p>
                Build with your coding agent. Shape every detail. Keep your work on infrastructure you
                control.
              </p>
              <div className="desktop-feature-list">
                <span>
                  <Icon name="spark" />
                  Start with a prompt
                </span>
                <span>
                  <Icon name="monitor" />
                  See your ideas take shape
                </span>
                <span>
                  <Icon name="lock" />
                  Your engine. Your workspace.
                </span>
              </div>
              <small>Open source · Made for your desktop</small>
            </aside>
            <main className="desktop-connect-page">
              <div className="eyebrow">WELCOME TO SANDBOXD</div>
              <h2>Connect your engine.</h2>
              <p>Your desktop is ready. Choose where your apps will run.</p>
              <ConnectionForm state={state} onConnected={connected} onBusy={setConnecting} />
              <div className="desktop-setup-help">
                <strong>New to sandboxd?</strong>
                <p>
                  {state.platform === 'win32'
                    ? 'Choose Install & open workspace to set up an engine on this computer. No terminal needed.'
                    : 'The engine runs your apps and agents. Connect an existing engine, or follow the local setup guide.'}
                </p>
                <button
                  className="text-button"
                  onClick={() =>
                    void bridge?.openExternal(
                      'https://github.com/tastyeffectco/sandboxd/blob/main/docs/desktop.md',
                    )
                  }
                >
                  Set up an engine <Icon name="external" size={13} />
                </button>
              </div>
            </main>
          </div>
        )}
      </div>
      {state && (
        <footer className="desktop-statusbar">
          <span>
            <span className={`engine-status ${online && selected ? 'online' : ''}`} />
            {selected
              ? `${selected.name} · ${online ? 'Connected' : 'Offline'}`
              : 'Choose an engine to begin'}
          </span>
          <span>
            {state.platform === 'darwin' ? '⌘' : 'Ctrl'} K · Quick search <i /> Desktop {state.version}
          </span>
        </footer>
      )}
      {connections && state && (
        <Dialog
          title="Engine connections"
          close={() => {
            if (!connecting) setConnections(false)
          }}
        >
          <ConnectionForm state={state} onConnected={connected} onBusy={setConnecting} />
          {selected && (
            <div className="desktop-disconnect">
              <p>Disconnecting leaves your apps running on the engine.</p>
              <Btn
                disabled={connecting}
                onClick={async () => {
                  if (!confirmLeaveWorkspace()) return
                  try {
                    connected(await bridge!.disconnect(false))
                  } catch (e) {
                    setError((e as Error).message)
                  }
                }}
              >
                Disconnect
              </Btn>
              <Btn
                variant="danger"
                disabled={connecting}
                onClick={async () => {
                  if (!confirmLeaveWorkspace()) return
                  try {
                    connected(await bridge!.disconnect(true))
                  } catch (e) {
                    setError((e as Error).message)
                  }
                }}
              >
                Forget this engine
              </Btn>
            </div>
          )}
        </Dialog>
      )}
    </div>
  )
}

function ConnectionForm({
  state,
  onConnected,
  onBusy,
}: {
  state: DesktopState
  onConnected: (state: DesktopState) => void
  onBusy: (busy: boolean) => void
}) {
  const [choice, setChoice] = useState<DesktopConnection>(
    state.connections.find((c) => c.url === state.selected) || local,
  )
  const [mode, setMode] = useState(choice.url === local.url ? 'local' : 'remote')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const working = busy || !!state.localEngine?.busy
  useEffect(() => {
    onBusy(working)
  }, [working, onBusy])
  const install = async () => {
    if (working || !confirmLeaveWorkspace()) return
    setBusy(true)
    setError('')
    try {
      const result = await bridge!.installEngine()
      if (result.phase === 'ready') onConnected(await bridge!.connect(local))
    } catch (e) {
      setError((e as Error).message.replace(/^Error invoking remote method '[^']+': Error: /, ''))
    } finally {
      setBusy(false)
    }
  }
  const connect = async (connection = choice) => {
    if (working || !confirmLeaveWorkspace()) return
    setBusy(true)
    onBusy(true)
    setError('')
    try {
      onConnected(await bridge!.connect(connection))
    } catch (e) {
      setError((e as Error).message.replace(/^Error invoking remote method '[^']+': Error: /, ''))
    } finally {
      setBusy(false)
      onBusy(false)
    }
  }
  return (
    <div className="desktop-connection-form">
      <div className="desktop-connection-modes">
        <button
          aria-pressed={mode === 'local'}
          disabled={working}
          onClick={() => {
            setMode('local')
            setChoice(local)
          }}
        >
          <Icon name="monitor" />
          <strong>This computer</strong>
          <span>A local sandboxd engine</span>
        </button>
        <button
          aria-pressed={mode === 'remote'}
          disabled={working}
          onClick={() => {
            setMode('remote')
            setChoice({ name: '', url: '', allowInsecure: false })
          }}
        >
          <Icon name="external" />
          <strong>Remote engine</strong>
          <span>Your server or team instance</span>
        </button>
      </div>
      {mode === 'local' && state.platform === 'win32' && (
        <section className="desktop-local-setup" aria-label="Local engine setup">
          <strong>
            {state.localEngine?.installed
              ? 'Your engine on this computer'
              : 'Everything you need, installed for you.'}
          </strong>
          <p>
            Sets up a private Linux environment and the sandboxd engine. First setup downloads several GB and
            can take 15–30 minutes. Windows may ask for administrator permission or a restart.
          </p>
          {state.localEngine?.message && (
            <p
              role={state.localEngine.phase === 'error' ? 'alert' : 'status'}
              className={state.localEngine.phase === 'error' ? 'inline-error' : 'desktop-setup-progress'}
            >
              {state.localEngine.message}
            </p>
          )}
          <Btn
            variant="primary"
            disabled={working}
            onClick={() => void install()}
            data-testid="desktop-install-engine"
          >
            {working
              ? 'Setting up your engine…'
              : state.localEngine?.phase === 'restart'
                ? 'Continue setup'
                : state.localEngine?.phase === 'error'
                  ? 'Retry setup'
                  : state.localEngine?.installed
                    ? 'Start & open workspace'
                    : 'Install & open workspace'}
            {!working && <Icon name="arrow" size={16} />}
          </Btn>
          <small>
            {working
              ? 'Keep this window open. Your progress is saved if setup needs to restart.'
              : 'Already have an engine? Connect using its address below.'}
          </small>
        </section>
      )}
      <form
        onSubmit={(e) => {
          e.preventDefault()
          void connect()
        }}
      >
        {mode === 'remote' && (
          <label className="form-field">
            Connection name
            <Input
              value={choice.name}
              onChange={(e) => setChoice({ ...choice, name: e.target.value })}
              placeholder="My studio"
              maxLength={80}
              disabled={working}
            />
          </label>
        )}
        <label className="form-field">
          Engine API address
          <Input
            autoFocus
            type="url"
            required
            value={choice.url}
            onChange={(e) => setChoice({ ...choice, url: e.target.value })}
            placeholder="https://sandboxd.example.com"
            disabled={working}
            data-testid="desktop-engine-url"
          />
        </label>
        {mode === 'local' && (
          <p className="field-help">
            {state.platform === 'win32'
              ? 'Use the address of an existing engine, or install one above.'
              : 'Usually http://127.0.0.1:9090. The engine needs to be running before you connect.'}
          </p>
        )}
        {mode === 'remote' && (
          <details className="desktop-advanced">
            <summary>Connection options</summary>
            <label className="check-label">
              <input
                type="checkbox"
                checked={choice.allowInsecure}
                onChange={(e) => setChoice({ ...choice, allowInsecure: e.target.checked })}
                disabled={working}
              />{' '}
              Allow unencrypted HTTP
            </label>
            {choice.allowInsecure && (
              <p className="field-help">
                HTTP sends your password and workspace data without transport encryption. Use only on a
                network you trust.
              </p>
            )}
          </details>
        )}
        {error && (
          <p role="alert" className="inline-error">
            {error}
          </p>
        )}
        <Btn
          type="submit"
          variant="primary"
          disabled={working || !choice.url.trim()}
          data-testid="desktop-connect"
        >
          {busy ? 'Connecting…' : 'Open workspace'}
          <Icon name="arrow" size={16} />
        </Btn>
        <p className="desktop-login-note">
          <Icon name="lock" size={12} /> Sign in with your engine’s console password after connecting.
          {!state.canRemember && ' You will need to sign in again after closing the app.'}
        </p>
      </form>
      {state.connections.length > 0 && (
        <div className="desktop-recent">
          <h3>Recent engines</h3>
          {state.connections.map((connection) => (
            <button key={connection.url} disabled={working} onClick={() => void connect(connection)}>
              <Icon name="box" size={17} />
              <span>
                <strong>{connection.name}</strong>
                <small>{connection.url}</small>
              </span>
              <Icon name="arrow" size={15} />
            </button>
          ))}
        </div>
      )}
    </div>
  )
}
