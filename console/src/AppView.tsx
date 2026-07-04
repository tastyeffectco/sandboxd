import { Fragment, useCallback, useEffect, useRef, useState } from 'react'
import { api, App as TApp, Sandbox, Process, ConfigItem, Snapshot, AppEvent, GitStatus, GitFile } from './api'
import { c, font, mono, Card, H, Btn, Pill, StatusPill, statusTone, Input, tab } from './design/kit'

type Msg = { role: 'user' | 'agent'; text: string; taskId?: string; done?: boolean }
const TABS = ['overview', 'git', 'config', 'snapshots', 'activity'] as const
type Tab = (typeof TABS)[number]

export function AppView({
  appId, initialTab, onError, toast, goApps, goSettings,
}: { appId: string; initialTab?: string; onError: (m: string) => void; toast: (m: string) => void; goApps: () => void; goSettings: () => void }) {
  const [app, setApp] = useState<TApp | null>(null)
  const [sb, setSb] = useState<Sandbox | null>(null)
  const [tabName, setTabName] = useState<Tab>((initialTab as Tab) || 'overview')
  const [busy, setBusy] = useState(false)
  const [menu, setMenu] = useState(false)

  const refresh = useCallback(async () => {
    try {
      const a = await api.getApp(appId)
      setApp(a)
      setSb(a.current_sandbox_id ? await api.getSandbox(a.current_sandbox_id) : null)
    } catch (e) { onError((e as Error).message) }
  }, [appId, onError])
  useEffect(() => { refresh() }, [refresh])
  useEffect(() => { const t = setInterval(refresh, 4000); return () => clearInterval(t) }, [refresh])

  if (!app) return <div style={{ padding: 40, color: c.muted2 }}>Loading…</div>
  const status = sb?.status
  const previewURL = sb?.preview?.url

  const act = async (fn: () => Promise<unknown>, ok?: string) => {
    setBusy(true)
    try { await fn(); await refresh(); if (ok) toast(ok) } catch (e) { onError((e as Error).message) } finally { setBusy(false) }
  }
  const snapshot = () => { if (sb) act(() => api.createSnapshot(sb.id, `${app.name}-${Date.now()}`), 'Snapshot captured') }

  const tabBadge: Record<Tab, string> = { overview: '', git: '', config: '', snapshots: '', activity: '' }

  return (
    <div style={{ maxWidth: 1320, margin: '0 auto', padding: '28px 40px 80px' }}>
      <div style={{ fontSize: 12, color: c.muted2, marginBottom: 10 }}>
        <a onClick={goApps} className="dc-hoverink" style={{ color: c.muted, cursor: 'pointer', textDecoration: 'none' }}>Apps</a>
        <span style={{ margin: '0 4px' }}>/</span><span style={{ color: c.fg }}>{app.name}</span>
      </div>

      {/* header */}
      <div style={{ display: 'flex', alignItems: 'flex-start', gap: 14, marginBottom: 20, flexWrap: 'wrap' }}>
        <div>
          <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
            <h1 style={{ fontFamily: font.display, fontSize: 26, fontWeight: 700, margin: 0 }}>{app.name}</h1>
            {sb ? <StatusPill status={status} /> : <Pill tone="neutral">no sandbox</Pill>}
          </div>
          <div onClick={() => { if (sb) { navigator.clipboard?.writeText(sb.id); toast('Sandbox ID copied') } }} title="Copy sandbox ID" style={{ ...mono, fontSize: 11, color: c.muted2, marginTop: 2, cursor: 'pointer' }}>
            {sb ? `sb:${sb.id}` : app.id} <span style={{ fontSize: 10, color: c.faint }}>⧉</span>
          </div>
        </div>
        <div style={{ marginLeft: 'auto', display: 'flex', gap: 8, alignItems: 'center', position: 'relative' }}>
          {!sb && <Btn variant="primary" disabled={busy} onClick={() => act(() => api.createAppSandbox(appId))} sm>Create sandbox</Btn>}
          {sb && status === 'stopped' && <Btn disabled={busy} onClick={() => act(() => api.startSandbox(sb.id))}>Start</Btn>}
          {sb && status === 'running' && <Btn disabled={busy} onClick={() => act(() => api.stopSandbox(sb.id))}>Stop</Btn>}
          {previewURL && status === 'running' && <Btn onClick={() => window.open(previewURL, '_blank')}>Open ↗</Btn>}
          <Btn onClick={() => setMenu((m) => !m)} style={{ letterSpacing: 1 }}>⋯</Btn>
          {menu && (
            <Card style={{ position: 'absolute', top: 38, right: 0, padding: 4, minWidth: 180, zIndex: 40, boxShadow: '0 8px 24px rgba(0,0,0,.08)' }}>
              {sb && <MenuItem onClick={() => { setMenu(false); snapshot() }}>Take snapshot</MenuItem>}
              {previewURL && <MenuItem onClick={() => { navigator.clipboard?.writeText(previewURL); toast('Preview URL copied'); setMenu(false) }}>Copy preview URL</MenuItem>}
              <div style={{ height: 1, background: c.panel2, margin: '4px 0' }} />
              <MenuItem danger onClick={async () => { setMenu(false); if (!window.confirm(`Delete “${app.name}” and everything it owns? This cannot be undone.`)) return; try { await api.deleteApp(appId); toast('App deleted'); goApps() } catch (e) { onError((e as Error).message) } }}>Delete app…</MenuItem>
            </Card>
          )}
        </div>
      </div>

      {/* tabs */}
      <div style={{ display: 'flex', gap: 2, borderBottom: `1px solid ${c.border}`, marginBottom: 24 }}>
        {TABS.map((t) => (
          <div key={t} data-testid={`tab-${t}`} className="dc-hoverink" onClick={() => setTabName(t)} style={{ ...tab(tabName === t), textTransform: 'capitalize' }}>
            {t}{tabBadge[t] && <span style={{ ...mono, marginLeft: 6, fontSize: 10, color: c.muted2 }}>{tabBadge[t]}</span>}
          </div>
        ))}
      </div>

      {tabName === 'overview' && <Overview app={app} sb={sb} previewURL={previewURL} onError={onError} refresh={refresh} />}
      {tabName === 'git' && <GitTab appId={appId} onError={onError} toast={toast} goSettings={goSettings} />}
      {tabName === 'config' && <ConfigTab appId={appId} onError={onError} />}
      {tabName === 'snapshots' && <SnapshotsTab appId={appId} appName={app.name} onError={onError} toast={toast} refresh={refresh} sb={sb} />}
      {tabName === 'activity' && <ActivityTab appId={appId} onError={onError} />}
    </div>
  )
}

function MenuItem({ children, onClick, danger }: { children: React.ReactNode; onClick: () => void; danger?: boolean }) {
  return <div onClick={onClick} style={{ padding: '7px 11px', borderRadius: 6, fontSize: 12.5, cursor: 'pointer', color: danger ? c.bad : c.fg }} onMouseEnter={(e) => (e.currentTarget.style.background = danger ? 'rgba(220,38,38,.06)' : c.panel2)} onMouseLeave={(e) => (e.currentTarget.style.background = 'transparent')}>{children}</div>
}

// ---------- OVERVIEW (preview + processes + runtime + AGENT CHAT) ----------
function Overview({ app, sb, previewURL, onError, refresh }: { app: TApp; sb: Sandbox | null; previewURL?: string; onError: (m: string) => void; refresh: () => void }) {
  const running = sb?.status === 'running'
  const procs: Process[] = sb?.processes || []
  return (
    <div style={{ display: 'grid', gridTemplateColumns: 'minmax(0,1fr) 400px', gap: 16, alignItems: 'start' }}>
      <div style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
        {/* preview */}
        <Card style={{ overflow: 'hidden' }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: 10, padding: '10px 14px', borderBottom: `1px solid ${c.border}`, background: c.panel2 }}>
            <span onClick={() => { if (previewURL) { navigator.clipboard?.writeText(previewURL) } }} title="Copy URL" style={{ ...mono, fontSize: 11.5, color: c.muted, flex: 1, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', cursor: 'pointer' }}>
              {previewURL || 'no preview'}{previewURL ? ' ⧉' : ''}
            </span>
            {previewURL && running && <a onClick={() => window.open(previewURL, '_blank')} style={{ color: c.link, fontSize: 12, cursor: 'pointer' }}>Open ↗</a>}
          </div>
          <div style={{ height: 420, position: 'relative', background: running && previewURL ? '#fff' : 'repeating-linear-gradient(45deg,#f4f4f5,#f4f4f5 12px,#eeeef0 12px,#eeeef0 24px)', display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
            {running && previewURL ? (
              <iframe src={previewURL} title="preview" data-testid="preview" style={{ width: '100%', height: '100%', border: 'none' }} />
            ) : (
              <div style={{ ...mono, fontSize: 11.5, color: c.muted2, background: '#ffffffcc', border: `1px dashed ${c.border2}`, borderRadius: 7, padding: '8px 14px' }} data-testid="preview-empty">
                {!sb ? 'No sandbox yet — create one from the header' : sb.preview?.status === 'none' ? 'No public endpoint — worker running' : 'Sandbox not running'}
              </div>
            )}
          </div>
        </Card>

        {/* processes */}
        <ProcessesCard sb={sb} running={running} procs={procs} onError={onError} />

        <RuntimeCard appId={app.id} />
      </div>

      <AgentChat sb={sb} onError={onError} refresh={refresh} />
    </div>
  )
}

function ProcessesCard({ sb, running, procs, onError }: { sb: Sandbox | null; running: boolean; procs: Process[]; onError: (m: string) => void }) {
  const [logsFor, setLogsFor] = useState<string | null>(null)
  const [lines, setLines] = useState<string[]>([])
  const [loading, setLoading] = useState(false)
  const logRef = useRef<HTMLDivElement>(null)
  useEffect(() => { if (logRef.current) logRef.current.scrollTop = logRef.current.scrollHeight }, [lines])

  const viewLogs = async (name: string) => {
    if (!sb) return
    if (logsFor === name) { setLogsFor(null); return }
    setLogsFor(name); setLines([]); setLoading(true)
    try { const r = await api.getProcessLogs(sb.id, name, 200); setLines(r.lines) }
    catch (e) { onError((e as Error).message) } finally { setLoading(false) }
  }

  return (
    <Card style={{ padding: 16 }}>
      <div style={{ display: 'flex', alignItems: 'baseline', marginBottom: 10 }}>
        <H>Processes</H>
        <span style={{ marginLeft: 'auto', ...mono, fontSize: 10.5, color: c.muted2 }}>supervised · auto-restart</span>
      </div>
      {procs.length === 0 ? (
        <div style={{ color: c.muted2, fontSize: 12.5 }} data-testid="processes-empty">No processes {running ? 'reported' : '— sandbox not running'}.</div>
      ) : (
        <div style={{ display: 'grid', gridTemplateColumns: '1.2fr .9fr .9fr .6fr .7fr auto', gap: '0 12px', fontSize: 12.5, alignItems: 'center' }} data-testid="processes-list">
          {['Name', 'Kind', 'Status', 'PID', 'Restarts', ''].map((h, i) => (
            <div key={i} style={{ color: c.muted2, fontSize: 11, textTransform: 'uppercase', letterSpacing: '.7px', padding: '6px 0', borderBottom: `1px solid ${c.border}` }}>{h}</div>
          ))}
          {procs.map((p) => (
            <Fragment key={p.name}>
              <div style={{ ...mono, padding: '9px 0' }}>{p.name}</div>
              <div style={{ color: c.muted, padding: '9px 0' }}>{p.kind}</div>
              <div style={{ padding: '9px 0' }}><Pill tone={p.running ? 'good' : 'neutral'} dot>{p.running ? 'running' : 'stopped'}</Pill></div>
              <div style={{ ...mono, color: c.muted, padding: '9px 0' }}>{p.pid || '—'}</div>
              <div style={{ ...mono, color: c.muted, padding: '9px 0' }}>{p.restarts ?? 0}</div>
              <div style={{ padding: '9px 0', textAlign: 'right' }}>
                <a onClick={() => viewLogs(p.name)} className="dc-hoverink" data-testid={`process-logs-${p.name}`} style={{ fontSize: 12, color: logsFor === p.name ? c.fg : c.muted2, cursor: 'pointer' }}>{logsFor === p.name ? 'Hide' : 'Logs'}</a>
              </div>
            </Fragment>
          ))}
        </div>
      )}
      {logsFor && (
        <div style={{ marginTop: 12 }}>
          <div style={{ display: 'flex', alignItems: 'center', marginBottom: 6 }}>
            <span style={{ ...mono, fontSize: 11.5, color: c.muted }}>{logsFor} — last 200 lines</span>
            <a onClick={() => viewLogs(logsFor)} className="dc-hoverink" style={{ marginLeft: 'auto', fontSize: 11.5, color: c.link, cursor: 'pointer' }}>Refresh</a>
          </div>
          <div ref={logRef} data-testid="process-log-output" style={{ background: '#0a0a0a', border: `1px solid ${c.border2}`, borderRadius: 7, padding: '10px 12px', ...mono, fontSize: 11.5, color: '#d4d4d8', lineHeight: 1.6, maxHeight: 260, overflow: 'auto', whiteSpace: 'pre-wrap' }}>
            {loading ? 'loading…' : lines.length === 0 ? '(no output)' : lines.join('\n')}
          </div>
        </div>
      )}
    </Card>
  )
}

function RuntimeCard({ appId }: { appId: string }) {
  const [eff, setEff] = useState<{ command?: string; port?: number; health?: string } | null>(null)
  const [valid, setValid] = useState<boolean | null>(null)
  useEffect(() => {
    api.appManifest(appId).then((m) => {
      const w = m.effective?.web
      if (w) setEff({ command: w.command, port: w.port, health: w.health_path })
      setValid(m.present ? m.validation?.valid ?? null : null)
    }).catch(() => {})
  }, [appId])
  return (
    <Card style={{ padding: 16 }}>
      <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 4 }}>
        <H>Runtime</H>
        {valid !== null && <Pill tone={valid ? 'good' : 'bad'}>{valid ? 'sandbox.yaml valid' : 'sandbox.yaml invalid'}</Pill>}
        <span style={{ marginLeft: 'auto', fontSize: 11.5, color: c.muted2 }}>Advisory — nothing applied automatically</span>
      </div>
      <pre style={{ background: c.bg, border: `1px solid ${c.border}`, borderRadius: 7, padding: '12px 14px', ...mono, fontSize: 12, color: c.fg2, margin: '10px 0 0', overflowX: 'auto', lineHeight: 1.6 }}>
{eff ? `command: ${eff.command || '(default)'}\nport: ${eff.port ?? 3000}\nhealth: ${eff.health || '/'}` : 'no sandbox.yaml — using defaults'}
      </pre>
    </Card>
  )
}

function AgentChat({ sb, onError, refresh }: { sb: Sandbox | null; onError: (m: string) => void; refresh: () => void }) {
  const [agent, setAgent] = useState('claude-code')
  const [model, setModel] = useState('')
  const [text, setText] = useState('')
  const [msgs, setMsgs] = useState<Msg[]>([])
  const [running, setRunning] = useState(false)
  const [resolved, setResolved] = useState('')
  const esRef = useRef<EventSource | null>(null)
  const scrollRef = useRef<HTMLDivElement>(null)
  useEffect(() => () => esRef.current?.close(), [])
  useEffect(() => { if (scrollRef.current) scrollRef.current.scrollTop = scrollRef.current.scrollHeight }, [msgs, running])

  const send = async () => {
    if (!sb || sb.status !== 'running' || !text.trim() || running) return
    const prompt = text.trim()
    setText('')
    setMsgs((m) => [...m, { role: 'user', text: prompt }])
    setResolved(''); setRunning(true)
    try {
      const t = await api.submitTask(sb.id, prompt, agent, model || undefined)
      let agentText = ''
      const es = new EventSource(api.taskEventsURL(sb.id, t.id))
      esRef.current = es
      es.addEventListener('status', (m) => { try { const j = JSON.parse((m as MessageEvent).data); if (j.model) setResolved(j.model) } catch { /* */ } })
      es.addEventListener('message', (m) => {
        try { const j = JSON.parse((m as MessageEvent).data); if (j.role === 'agent' && j.text) { agentText += j.text; setMsgs((cur) => { const copy = [...cur]; const last = copy[copy.length - 1]; if (last && last.role === 'agent' && !last.done) last.text = agentText; else copy.push({ role: 'agent', text: agentText, taskId: t.id }); return copy }) } } catch { /* */ }
      })
      es.addEventListener('done', () => {
        es.close(); setRunning(false)
        setMsgs((cur) => { const copy = [...cur]; const last = copy[copy.length - 1]; if (last && last.role === 'agent') last.done = true; else copy.push({ role: 'agent', text: '(done)', taskId: t.id, done: true }); return copy })
        refresh()
      })
      es.onerror = () => { es.close(); setRunning(false) }
    } catch (e) { setRunning(false); onError((e as Error).message) }
  }

  return (
    <Card style={{ display: 'flex', flexDirection: 'column', height: 640, overflow: 'hidden' }}>
      <div style={{ display: 'flex', alignItems: 'center', gap: 8, padding: '10px 14px', borderBottom: `1px solid ${c.border}`, background: c.panel3 }}>
        <H size={14}>Agent</H>
        <div style={{ marginLeft: 'auto', display: 'flex', gap: 6 }}>
          <select value={agent} onChange={(e) => { setAgent(e.target.value); setModel('') }} data-testid="task-agent" style={selStyle}>
            <option value="claude-code">Claude Code</option><option value="opencode">OpenCode</option>
          </select>
          <select value={model} onChange={(e) => setModel(e.target.value)} data-testid="task-model" style={selStyle}>
            <option value="">Default model</option>
            {agent === 'claude-code' ? <><option value="sonnet">Sonnet</option><option value="opus">Opus</option><option value="haiku">Haiku</option></> : <><option value="opencode/claude-opus-4-5">Zen Opus</option><option value="opencode/claude-haiku-4-5">Zen Haiku</option></>}
          </select>
        </div>
      </div>
      <div ref={scrollRef} style={{ flex: 1, overflowY: 'auto', padding: 14, display: 'flex', flexDirection: 'column', gap: 10 }}>
        {msgs.length === 0 && !running && (
          <div style={{ margin: 'auto', textAlign: 'center', color: c.muted2, fontSize: 12.5, maxWidth: 220 }}>Ask the agent to change this app — it works inside the sandbox and nothing is committed until you approve.</div>
        )}
        {msgs.map((m, i) => (
          <div key={i} style={{ display: 'flex', justifyContent: m.role === 'user' ? 'flex-end' : 'flex-start' }}>
            <div style={{ maxWidth: '85%', fontSize: 12.5, borderRadius: 9, padding: '8px 11px', whiteSpace: 'pre-wrap', background: m.role === 'user' ? c.ink : c.panel2, color: m.role === 'user' ? '#fff' : c.fg, border: m.role === 'user' ? 'none' : `1px solid ${c.border}` }}>
              {m.text}
            </div>
          </div>
        ))}
        {resolved && <div style={{ ...mono, fontSize: 10.5, color: c.muted2 }}>▸ {resolved}</div>}
        {running && <div style={{ ...mono, fontSize: 11.5, color: c.muted2, animation: 'pulse 1.4s ease-in-out infinite' }}>▍ working…</div>}
      </div>
      <div style={{ display: 'flex', gap: 8, padding: 10, borderTop: `1px solid ${c.border}`, background: c.panel3 }}>
        <textarea value={text} onChange={(e) => setText(e.target.value)} onKeyDown={(e) => { if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); send() } }} placeholder={sb?.status === 'running' ? 'Message the agent…' : 'Start the sandbox to run tasks'} data-testid="task-prompt" rows={1} style={{ flex: 1, background: '#fff', border: `1px solid ${c.border2}`, borderRadius: 7, padding: '8px 11px', color: c.fg, fontSize: 12.5, fontFamily: font.sans, resize: 'none' }} />
        <Btn variant="primary" onClick={send} disabled={!sb || sb.status !== 'running' || running} data-testid="run-task">Send</Btn>
      </div>
    </Card>
  )
}
const selStyle: React.CSSProperties = { background: c.bg, border: `1px solid ${c.border2}`, borderRadius: 7, padding: '5px 7px', color: c.fg, fontSize: 11.5, fontFamily: font.sans }

// ---------- GIT ----------
function GitTab({ appId, onError, toast, goSettings }: { appId: string; onError: (m: string) => void; toast: (m: string) => void; goSettings: () => void }) {
  const [st, setSt] = useState<GitStatus | null>(null)
  const [sel, setSel] = useState<Record<string, boolean>>({})
  const [msg, setMsg] = useState('')
  const [busy, setBusy] = useState(false)
  const load = useCallback(() => api.gitStatus(appId).then((s) => { setSt(s); const init: Record<string, boolean> = {}; (s.files || []).forEach((f) => (init[f.path] = true)); setSel(init) }).catch((e) => onError((e as Error).message)), [appId, onError])
  useEffect(() => { load() }, [load])
  const files: GitFile[] = st?.files || []
  const commit = async () => {
    if (!msg.trim()) return
    setBusy(true)
    try { const r = await api.gitCommit(appId, { message: msg.trim(), paths: files.filter((f) => sel[f.path]).map((f) => f.path) }); if (r.committed) { toast(`Committed ${r.sha?.slice(0, 7)}`); setMsg(''); load() } else onError(r.reason || 'nothing committed') } catch (e) { onError((e as Error).message) } finally { setBusy(false) }
  }
  if (st && !st.available) return <Card style={{ padding: 16, color: c.muted2, fontSize: 13 }}>{st.reason === 'not_a_git_repo' ? 'This app is not a Git repository.' : 'Git is unavailable — start the sandbox.'}</Card>
  return (
    <div style={{ display: 'grid', gridTemplateColumns: 'minmax(0,1fr) 320px', gap: 16, alignItems: 'start' }}>
      <Card style={{ padding: 16 }} data-testid="git-panel">
        <div style={{ display: 'flex', alignItems: 'center', gap: 10, marginBottom: 12 }}>
          <H>Changes</H>
          <span style={{ ...mono, fontSize: 11, color: c.muted, background: c.panel2, border: `1px solid ${c.border}`, borderRadius: 5, padding: '2px 8px' }}>branch {st?.branch || 'main'}</span>
          <span style={{ fontSize: 12, color: c.muted2 }}>{files.length ? `${files.length} changed` : 'clean'}</span>
        </div>
        {files.length === 0 ? <div style={{ color: c.muted2, fontSize: 12.5 }}>No changes.</div> : files.map((f) => (
          <div key={f.path} onClick={() => setSel((s) => ({ ...s, [f.path]: !s[f.path] }))} className="dc-hoverborder" style={{ display: 'flex', alignItems: 'center', gap: 10, padding: '8px 10px', border: `1px solid ${c.border}`, borderRadius: 7, marginBottom: 6, cursor: 'pointer', background: c.panel2 }}>
            <span style={{ width: 15, height: 15, borderRadius: 4, border: `1px solid ${c.border2}`, display: 'flex', alignItems: 'center', justifyContent: 'center', fontSize: 10, background: sel[f.path] ? c.ink : '#fff', color: '#fff' }}>{sel[f.path] ? '✓' : ''}</span>
            <span style={{ ...mono, fontSize: 11, color: c.muted }}>{f.status}</span>
            <span style={{ ...mono, fontSize: 12.5 }}>{f.path}</span>
          </div>
        ))}
        <Input value={msg} onChange={(e) => setMsg(e.target.value)} placeholder="Commit message…" style={{ width: '100%', margin: '10px 0', fontSize: 13, fontFamily: font.sans }} />
        <Btn variant="primary" disabled={busy || !msg.trim() || files.length === 0} onClick={commit} style={{ padding: '9px 16px', fontSize: 13 }}>Commit</Btn>
      </Card>
      <div style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
        <Card style={{ padding: 16 }}>
          <H style={{ marginBottom: 6 }}>Remote</H>
          <div style={{ color: c.muted, fontSize: 12.5, marginBottom: 10 }}>Add a Git credential in Settings to push, or import private repos.</div>
          <Btn onClick={goSettings}>Git credentials →</Btn>
        </Card>
        <Card style={{ padding: 16 }}>
          <H style={{ marginBottom: 6 }}>Git vs Snapshots</H>
          <div style={{ color: c.muted, fontSize: 12.5 }}>Use <b style={{ color: c.fg }}>Git</b> to version and ship your source code. A <b style={{ color: c.fg }}>Snapshot</b> freezes the entire workspace — code plus installed packages, build output and data.</div>
        </Card>
      </div>
    </div>
  )
}

// ---------- CONFIG ----------
function ConfigTab({ appId, onError }: { appId: string; onError: (m: string) => void }) {
  const [items, setItems] = useState<ConfigItem[]>([])
  const [k, setK] = useState(''); const [v, setV] = useState(''); const [secret, setSecret] = useState(true)
  const load = useCallback(() => api.listConfig(appId).then(setItems).catch((e) => onError((e as Error).message)), [appId, onError])
  useEffect(() => { load() }, [load])
  const add = async () => { if (!k.trim()) return; try { await api.createConfig(appId, { key: k.trim(), value: v, sensitive: secret, access_policy: 'control_plane_only' }); setK(''); setV(''); load() } catch (e) { onError((e as Error).message) } }
  return (
    <Card style={{ padding: 16, maxWidth: 760 }} data-testid="config-panel">
      <div style={{ display: 'flex', alignItems: 'baseline', marginBottom: 6 }}>
        <H>Config &amp; Secrets</H>
        <span style={{ marginLeft: 'auto', fontSize: 11.5, color: c.muted2 }}>Secrets are encrypted at rest and never shown again</span>
      </div>
      {items.length === 0 ? <div style={{ color: c.muted2, fontSize: 12.5, padding: '10px 0' }}>No config yet. Add a key below.</div> : items.map((it) => (
        <div key={it.key} style={{ display: 'flex', alignItems: 'center', gap: 10, padding: '9px 12px', border: `1px solid ${c.border}`, borderRadius: 7, marginBottom: 6, background: c.panel2 }}>
          <span style={{ ...mono, fontSize: 12.5, fontWeight: 500, minWidth: 160 }}>{it.key}</span>
          <span style={{ ...mono, fontSize: 12, color: c.muted }}>{it.sensitive ? '••••••' : it.value}</span>
          <a onClick={() => api.deleteConfig(appId, it.key).then(load)} className="dc-hoverink" style={{ marginLeft: 'auto', color: c.muted2, fontSize: 12, cursor: 'pointer' }}>Remove</a>
        </div>
      ))}
      <div style={{ display: 'flex', gap: 8, marginTop: 12, alignItems: 'center' }}>
        <Input mono value={k} onChange={(e) => setK(e.target.value)} placeholder="KEY" style={{ width: 180 }} />
        <Input mono value={v} onChange={(e) => setV(e.target.value)} placeholder="value" type={secret ? 'password' : 'text'} style={{ flex: 1 }} />
        <label onClick={() => setSecret((s) => !s)} style={{ display: 'flex', alignItems: 'center', gap: 6, cursor: 'pointer', fontSize: 12.5, color: c.muted }}>
          <span style={{ width: 15, height: 15, borderRadius: 4, border: `1px solid ${c.border2}`, background: secret ? c.ink : '#fff', color: '#fff', display: 'flex', alignItems: 'center', justifyContent: 'center', fontSize: 10 }}>{secret ? '✓' : ''}</span>secret
        </label>
        <Btn variant="primary" onClick={add}>Add</Btn>
      </div>
    </Card>
  )
}

// ---------- SNAPSHOTS ----------
function SnapshotsTab({ appId, appName, onError, toast, refresh, sb }: { appId: string; appName: string; onError: (m: string) => void; toast: (m: string) => void; refresh: () => void; sb: Sandbox | null }) {
  const [snaps, setSnaps] = useState<Snapshot[]>([])
  const load = useCallback(() => api.listAppSnapshots(appId).then(setSnaps).catch((e) => onError((e as Error).message)), [appId, onError])
  useEffect(() => { load() }, [load])
  return (
    <div style={{ maxWidth: 760 }}>
      <Card style={{ padding: 16, marginBottom: 16 }}>
        <H style={{ marginBottom: 6 }}>Snapshots — save the whole environment</H>
        <div style={{ color: c.muted, fontSize: 12.5 }}>A snapshot freezes the <b style={{ color: c.fg }}>entire workspace</b> — your code plus installed packages, build output, and data — so you can roll back or clone the exact running setup in seconds.</div>
      </Card>
      {snaps.length === 0 ? (
        <div style={{ border: `1px dashed ${c.border2}`, borderRadius: 10, padding: 28, textAlign: 'center', color: c.muted2, fontSize: 12.5 }}>
          No snapshots yet. <a onClick={() => { if (sb) api.createSnapshot(sb.id, `${appName}-${Date.now()}`).then(() => { toast('Snapshot captured'); load() }).catch((e) => onError((e as Error).message)) }} style={{ color: c.link, cursor: 'pointer' }}>Take one now</a> — stop the sandbox first for a consistent capture.
        </div>
      ) : snaps.map((s) => (
        <Card key={s.id} style={{ display: 'flex', alignItems: 'center', gap: 12, padding: '13px 16px', marginBottom: 8 }}>
          <div>
            <div style={{ ...mono, fontSize: 12.5 }}>{s.name}</div>
            <div style={{ color: c.muted2, fontSize: 11.5 }}>{new Date(s.created_at).toLocaleString()} · {s.size_bytes ? `${Math.round(s.size_bytes / 1024)} KB` : '—'}</div>
          </div>
          <div style={{ marginLeft: 'auto', display: 'flex', gap: 6 }}>
            <Btn sm onClick={() => { if (window.confirm(`Roll back to "${s.name}"? Replaces the current sandbox.`)) api.restoreApp(appId, s.id).then(() => { toast('Rolled back'); refresh() }).catch((e) => onError((e as Error).message)) }}>Roll back</Btn>
            <Btn sm onClick={() => { const name = window.prompt('Duplicate into a new app named:', `${appName} copy`); if (name) api.forkApp(appId, s.id, name.trim()).then(() => toast('Duplicated')).catch((e) => onError((e as Error).message)) }}>Duplicate</Btn>
          </div>
        </Card>
      ))}
    </div>
  )
}

// ---------- ACTIVITY ----------
function ActivityTab({ appId, onError }: { appId: string; onError: (m: string) => void }) {
  const [events, setEvents] = useState<AppEvent[]>([])
  useEffect(() => { api.listAppEvents(appId).then(setEvents).catch((e) => onError((e as Error).message)) }, [appId, onError])
  return (
    <Card style={{ padding: 16, maxWidth: 860 }} data-testid="activity-panel">
      <div style={{ display: 'flex', alignItems: 'baseline', marginBottom: 10 }}>
        <H>Activity</H>
        <span style={{ marginLeft: 'auto', fontSize: 11.5, color: c.muted2 }}>Durable timeline — survives restarts</span>
      </div>
      {events.length === 0 ? <div style={{ color: c.muted2, fontSize: 12.5 }}>No activity yet.</div> : events.map((ev) => (
        <div key={ev.id} style={{ display: 'grid', gridTemplateColumns: '74px 160px 1fr', gap: 12, alignItems: 'baseline', padding: '6px 0', borderBottom: `1px solid ${c.panel2}`, fontSize: 12.5 }}>
          <span style={{ ...mono, fontSize: 11, color: c.muted2 }}>{new Date(ev.created_at).toLocaleTimeString()}</span>
          <span><Pill tone={ev.severity === 'error' ? 'bad' : ev.severity === 'warning' ? 'warn' : 'neutral'}>{ev.type}</Pill></span>
          <span style={{ color: c.fg }}>{ev.message}</span>
        </div>
      ))}
    </Card>
  )
}
