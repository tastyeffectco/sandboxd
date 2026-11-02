import { useCallback, useEffect, useState } from 'react'
import type { AddServerInput, AddServerStep, DesktopServer, DesktopServers, ServerStatus } from './desktop'
import { Btn, Input, Pill } from './design/kit'
import { Dialog } from './design/Dialog'
import { Icon } from './design/Icon'

const clean = (message: string) => message.replace(/^Error invoking remote method '[^']+': (Error: )?/, '')

function statusLabel(status: ServerStatus) {
  if (status.state === 'connected') return 'Connected'
  if (status.state === 'connecting') return 'Connecting…'
  return 'Offline'
}

// Settings → Your servers (desktop only). Servers are added once here; later
// screens (environments, publish) pick one from this list.
export function ServersPanel({ servers, toast }: { servers: DesktopServers; toast: (m: string) => void }) {
  const [list, setList] = useState<DesktopServer[] | null>(null)
  const [error, setError] = useState('')
  const [adding, setAdding] = useState(false)
  const [removing, setRemoving] = useState<DesktopServer | null>(null)
  const [busy, setBusy] = useState(false)
  const load = useCallback(
    () =>
      servers
        .list()
        .then(setList)
        .catch((e) => setError(clean((e as Error).message))),
    [servers],
  )
  useEffect(() => {
    void load()
    return servers.onChange(({ id, status }) =>
      setList((previous) => previous && previous.map((s) => (s.id === id ? { ...s, status } : s))),
    )
  }, [servers, load])
  const trust = async (server: DesktopServer) => {
    setError('')
    try {
      const result = await servers.trustIdentity(server.id)
      if (result.trusted) {
        toast(`${server.name} is trusted again`)
        await load()
      }
    } catch (e) {
      setError(clean((e as Error).message))
    }
  }
  return (
    <div className="servers-panel" data-testid="servers-panel">
      <div className="info-banner">
        <Icon name="monitor" />
        <p>
          Put your apps online on a server you rent (for example from Hetzner, DigitalOcean or OVH). Add it
          once with the address and password from your provider’s email; sandboxd sets everything up.
        </p>
      </div>
      {error && (
        <p role="alert" className="inline-error">
          {error}
        </p>
      )}
      {!list ? (
        <p role="status">Loading your servers…</p>
      ) : list.length === 0 ? (
        <p className="field-help" data-testid="servers-empty">
          No servers yet.
        </p>
      ) : (
        <ul className="servers-list" style={{ listStyle: 'none', padding: 0, display: 'grid', gap: 10 }}>
          {list.map((server) => (
            <li
              key={server.id}
              className="agent-account"
              data-testid={`server-${server.id}`}
              style={{ display: 'grid', gap: 8 }}
            >
              <div style={{ display: 'flex', alignItems: 'center', gap: 12 }}>
                <span style={{ flex: 1, minWidth: 0 }}>
                  <strong>{server.name}</strong>
                  {server.name !== server.host && <small className="field-help"> · {server.host}</small>}
                </span>
                <Pill
                  dot
                  tone={
                    server.status.state === 'connected'
                      ? 'good'
                      : server.status.state === 'connecting'
                        ? 'warn'
                        : 'bad'
                  }
                >
                  {statusLabel(server.status)}
                </Pill>
                <Btn sm variant="danger" onClick={() => setRemoving(server)} data-testid="server-remove">
                  Remove
                </Btn>
              </div>
              {server.status.state === 'offline' && server.status.reason && (
                <p role={server.status.identityChanged ? 'alert' : undefined} className="field-help">
                  {server.status.reason}
                </p>
              )}
              {server.status.identityChanged && (
                <div>
                  <Btn sm onClick={() => void trust(server)} data-testid="server-trust">
                    I reinstalled it: trust the new identity
                  </Btn>
                </div>
              )}
            </li>
          ))}
        </ul>
      )}
      <div style={{ marginTop: 16 }}>
        <Btn variant="primary" onClick={() => setAdding(true)} data-testid="server-add-open">
          <Icon name="plus" size={15} /> Add a server
        </Btn>
      </div>
      {adding && (
        <AddServerDialog
          servers={servers}
          close={() => setAdding(false)}
          added={(server) => {
            setAdding(false)
            toast(`${server.name} is ready`)
            void load()
          }}
        />
      )}
      {removing && (
        <Dialog title={`Remove ${removing.name}?`} close={() => !busy && setRemoving(null)}>
          <p>
            This computer stops connecting to {removing.name}. sandboxd and your apps keep running on the
            server; to stop them, delete them first or switch the server off with your provider.
          </p>
          <div style={{ display: 'flex', gap: 8, justifyContent: 'flex-end', marginTop: 16 }}>
            <Btn disabled={busy} onClick={() => setRemoving(null)}>
              Cancel
            </Btn>
            <Btn
              variant="danger"
              disabled={busy}
              data-testid="server-remove-confirm"
              onClick={async () => {
                setBusy(true)
                setError('')
                try {
                  const result = await servers.remove(removing.id)
                  toast(result.note)
                  setRemoving(null)
                  await load()
                } catch (e) {
                  setError(clean((e as Error).message))
                  setRemoving(null)
                } finally {
                  setBusy(false)
                }
              }}
            >
              Remove server
            </Btn>
          </div>
        </Dialog>
      )}
    </div>
  )
}

const STEPS: { step: AddServerStep; label: string }[] = [
  { step: 'connecting', label: 'Connecting to your server' },
  { step: 'checking', label: 'Checking your server' },
  { step: 'docker', label: 'Installing Docker' },
  { step: 'installing', label: 'Installing sandboxd (this takes a few minutes)' },
  { step: 'securing', label: 'Securing the connection' },
]

function AddServerDialog({
  servers,
  close,
  added,
}: {
  servers: DesktopServers
  close: () => void
  added: (server: DesktopServer) => void
}) {
  const [host, setHost] = useState('')
  const [password, setPassword] = useState('')
  const [username, setUsername] = useState('')
  const [port, setPort] = useState('')
  const [name, setName] = useState('')
  const [busy, setBusy] = useState(false)
  const [step, setStep] = useState<AddServerStep | null>(null)
  const [docker, setDocker] = useState(false)
  const [failure, setFailure] = useState<{ code: string; message: string; detail?: string } | null>(null)
  useEffect(
    () =>
      servers.onProgress((progress) => {
        setStep(progress.step)
        if (progress.step === 'docker') setDocker(true)
      }),
    [servers],
  )
  const submit = async (installDocker = false) => {
    if (busy) return
    setBusy(true)
    setFailure(null)
    setStep('connecting')
    const input: AddServerInput = { host: host.trim(), password, installDocker }
    if (username.trim()) input.username = username.trim()
    if (port.trim()) input.port = Number(port)
    if (name.trim()) input.name = name.trim()
    try {
      const result = await servers.add(input)
      if (result.ok) {
        setPassword('')
        added(result.server)
        return
      }
      setFailure(result)
    } catch (e) {
      setFailure({ code: 'error', message: clean((e as Error).message) })
    } finally {
      setBusy(false)
      setStep(null)
    }
  }
  const reached = step ? STEPS.findIndex((s) => s.step === step) : -1
  return (
    <Dialog title="Add a server" close={() => !busy && close()}>
      {busy ? (
        <div role="status" data-testid="server-add-progress">
          <ol style={{ listStyle: 'none', padding: 0, display: 'grid', gap: 8 }}>
            {STEPS.filter((s) => s.step !== 'docker' || docker).map((s) => {
              const index = STEPS.findIndex((x) => x.step === s.step)
              const state = index < reached ? 'done' : index === reached ? 'current' : 'waiting'
              return (
                <li
                  key={s.step}
                  aria-current={state === 'current' ? 'step' : undefined}
                  style={{
                    display: 'flex',
                    gap: 8,
                    alignItems: 'center',
                    opacity: state === 'waiting' ? 0.5 : 1,
                    fontWeight: state === 'current' ? 600 : 400,
                  }}
                >
                  <Icon
                    name={state === 'done' ? 'check' : state === 'current' ? 'refresh' : 'arrow'}
                    size={14}
                  />
                  {s.label}
                  {state === 'current' && '…'}
                </li>
              )
            })}
          </ol>
          <p className="field-help">Keep sandboxd open until this finishes.</p>
        </div>
      ) : (
        <form
          className="settings-form"
          onSubmit={(e) => {
            e.preventDefault()
            void submit(false)
          }}
        >
          <p className="field-help">
            Use the address and password from your server provider’s email. The password is used once to set
            things up, then forgotten. It is never saved.
          </p>
          <label className="form-field">
            Server address
            <Input
              autoFocus
              required
              value={host}
              onChange={(e) => setHost(e.target.value)}
              placeholder="203.0.113.10"
              maxLength={253}
              data-testid="server-host"
            />
          </label>
          <label className="form-field">
            Password
            <Input
              required
              type="password"
              autoComplete="off"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              data-testid="server-password"
            />
          </label>
          <details className="desktop-advanced">
            <summary>More options</summary>
            <label className="form-field">
              User name
              <Input
                value={username}
                onChange={(e) => setUsername(e.target.value)}
                placeholder="root"
                maxLength={32}
                data-testid="server-username"
              />
            </label>
            <label className="form-field">
              Port
              <Input
                value={port}
                onChange={(e) => setPort(e.target.value.replace(/\D/g, ''))}
                placeholder="22"
                inputMode="numeric"
                maxLength={5}
                data-testid="server-port"
              />
            </label>
            <label className="form-field">
              Name in your list
              <Input
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder="My server"
                maxLength={80}
                data-testid="server-name"
              />
            </label>
          </details>
          {failure && (
            <div role="alert" className="inline-error" data-testid="server-add-error">
              <p>{failure.message}</p>
              {failure.code === 'needs_docker' && (
                <Btn variant="primary" onClick={() => void submit(true)} data-testid="server-install-docker">
                  Install Docker and continue
                </Btn>
              )}
              {failure.detail && (
                <details>
                  <summary>Show details</summary>
                  <pre style={{ whiteSpace: 'pre-wrap', fontSize: 11, maxHeight: 200, overflow: 'auto' }}>
                    {failure.detail}
                  </pre>
                </details>
              )}
            </div>
          )}
          <div style={{ display: 'flex', gap: 8, justifyContent: 'flex-end' }}>
            <Btn onClick={close}>Cancel</Btn>
            <Btn
              type="submit"
              variant="primary"
              disabled={!host.trim() || !password}
              data-testid="server-add-submit"
            >
              Add server
            </Btn>
          </div>
        </form>
      )}
    </Dialog>
  )
}
