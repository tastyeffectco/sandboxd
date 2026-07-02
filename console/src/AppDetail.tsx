import { useCallback, useEffect, useRef, useState, type CSSProperties } from 'react'
import {
  api,
  App as TApp,
  Sandbox,
  ConfigItem,
  AccessPolicy,
  Snapshot,
  AppEvent,
  RuntimeInspect,
  AppManifest,
  ConfigSnippet,
  GitStatus,
  GitDiff,
  GitFile,
  GitPushResult,
} from './api'
import { StatusBadge } from './ui'

const ACCESS_POLICIES: AccessPolicy[] = ['control_plane_only', 'agent_access', 'runtime_access', 'both']
// Only control_plane_only is enforced today. The others describe delivery to
// agents/app runtimes that the secrets broker (Slice 2) has not implemented
// yet, so they are shown but not selectable — picking one must not imply a
// secret is being delivered anywhere.
const ACTIVE_POLICIES: AccessPolicy[] = ['control_plane_only']
const policyReserved = (p: AccessPolicy) => !ACTIVE_POLICIES.includes(p)
const policyLabel = (p: AccessPolicy) => (policyReserved(p) ? `${p} — reserved (broker)` : p)

export function AppDetail({
  appId,
  onError,
  onInfo,
  onDeleted,
}: {
  appId: string
  onError: (m: string) => void
  onInfo: (m: string) => void
  onDeleted?: () => void
}) {
  const [app, setApp] = useState<TApp | null>(null)
  const [sb, setSb] = useState<Sandbox | null>(null)
  const [busy, setBusy] = useState(false)
  const [snapReload, setSnapReload] = useState(0) // bump to refresh snapshot history

  const refresh = useCallback(async () => {
    try {
      const a = await api.getApp(appId)
      setApp(a)
      setSb(a.current_sandbox_id ? await api.getSandbox(a.current_sandbox_id) : null)
    } catch (e) {
      onError((e as Error).message)
    }
  }, [appId, onError])

  useEffect(() => {
    refresh()
  }, [refresh])
  useEffect(() => {
    const t = setInterval(refresh, 4000) // reflect status/preview changes
    return () => clearInterval(t)
  }, [refresh])

  const act = async (fn: () => Promise<unknown>) => {
    setBusy(true)
    try {
      await fn()
      await refresh()
    } catch (e) {
      onError((e as Error).message)
    } finally {
      setBusy(false)
    }
  }

  // Capture a snapshot with explicit feedback. A running source returns 409.
  // On success, refresh the history list below.
  const snapshot = async () => {
    if (!sb || !app) return
    setBusy(true)
    try {
      await api.createSnapshot(sb.id, `${app.name}-${Date.now()}`)
      onInfo('Snapshot captured.')
      setSnapReload((n) => n + 1)
    } catch (e) {
      const err = e as Error & { status?: number }
      onError(err.status === 409 ? 'Stop the sandbox before capturing a snapshot.' : err.message)
    } finally {
      setBusy(false)
    }
  }

  if (!app) return <p className="muted">Loading…</p>

  const status = sb?.status
  const previewURL = sb?.preview?.url

  return (
    <div className="stack">
      <div className="row">
        <div>
          <h1>{app.name}</h1>
          <div className="muted mono" style={{ fontSize: 12, marginTop: 4 }}>{app.id}</div>
        </div>
        <div className="spacer" />
        {sb && <StatusBadge status={status} />}
      </div>

      <div className="row" data-testid="controls">
        {!sb && (
          <button
            className="btn btn-primary"
            disabled={busy}
            data-testid="create-sandbox"
            onClick={() => act(() => api.createAppSandbox(appId))}
          >
            Create sandbox
          </button>
        )}
        {sb && status === 'stopped' && (
          <button className="btn btn-primary" disabled={busy} data-testid="start" onClick={() => act(() => api.startSandbox(sb.id))}>
            Start
          </button>
        )}
        {sb && status === 'running' && (
          <button className="btn btn-outline" disabled={busy} data-testid="stop" onClick={() => act(() => api.stopSandbox(sb.id))}>
            Stop
          </button>
        )}
        {sb && (
          <button
            className="btn btn-outline"
            disabled={busy}
            data-testid="snapshot"
            title="Capture a snapshot (stop the sandbox first)"
            onClick={snapshot}
          >
            Snapshot
          </button>
        )}
        {sb && (
          <button
            className="btn btn-ghost"
            disabled={busy}
            data-testid="delete-sandbox"
            onClick={() => {
              if (
                !window.confirm(
                  'Delete this sandbox AND its workspace?\n\nThis permanently removes the workspace — your code, installed dependencies, and generated files. Snapshot first if you want to keep it. Continue?',
                )
              ) {
                return
              }
              act(() => api.deleteSandbox(sb.id))
            }}
          >
            Delete sandbox and workspace
          </button>
        )}
        <div className="spacer" />
        <button
          className="btn btn-danger"
          disabled={busy}
          data-testid="delete-app"
          title="Delete the app and everything it owns"
          onClick={async () => {
            if (
              !window.confirm(
                `Delete the app “${app?.name ?? appId}” and EVERYTHING it owns?\n\n` +
                  'This stops and removes its sandbox container, deletes the workspace image, ' +
                  'all snapshots, config, and history. This cannot be undone. Continue?',
              )
            ) {
              return
            }
            setBusy(true)
            try {
              await api.deleteApp(appId)
              onInfo('App and all its resources deleted.')
              onDeleted?.()
            } catch (e) {
              onError((e as Error).message)
              setBusy(false)
            }
          }}
        >
          Delete app (everything)
        </button>
      </div>

      <div className="detail">
        <div>
          <h2>Preview / endpoint</h2>
          {previewURL && status === 'running' ? (
            <iframe className="preview-frame" src={previewURL} title="preview" data-testid="preview" />
          ) : (
            <div className="preview-empty" data-testid="preview-empty">
              {!sb
                ? 'No sandbox yet'
                : sb.preview?.status === 'none'
                  ? 'No public endpoint — worker process running'
                  : 'Sandbox not running'}
            </div>
          )}
          {previewURL && (
            <div className="mono" style={{ fontSize: 12, marginTop: 8 }}>
              <a href={previewURL} target="_blank" rel="noreferrer" className="linklike">
                {previewURL} ↗
              </a>
            </div>
          )}
        </div>

        <TaskPanel sandboxId={sb?.id} running={status === 'running'} onError={onError} />
      </div>

      <ProcessesPanel sandbox={sb} onError={onError} />

      <RuntimeInspectPanel appId={appId} sandboxId={sb?.id} onError={onError} />

      <GitPanel appId={appId} repoURL={app.git?.repo_url} onError={onError} />

      <SnapshotsPanel
        appId={appId}
        appName={app.name}
        reloadKey={snapReload}
        onError={onError}
        onInfo={onInfo}
        onChanged={refresh}
      />

      <ConfigPanel appId={appId} onError={onError} />

      <ActivityPanel appId={appId} reloadKey={snapReload} onError={onError} />
    </div>
  )
}

// RuntimeInspectPanel renders ADVISORY runtime detection (GET
// /v1/apps/{id}/runtime-inspect). The console only renders the server's result —
// it owns no detection logic, and nothing here is applied automatically: the
// user always overrides manually (the preset dropdown on app/sandbox create).
// askAgentPrompt builds a paste-ready prompt explaining the sandbox.yaml schema +
// the suggested fix. It is NOT submitted — the user pastes it into the task box.
function askAgentPrompt(suggested: string, notes?: string[], snippets?: ConfigSnippet[]): string {
  return [
    'Please create or fix `sandbox.yaml` in the workspace root so the preview works.',
    '',
    'sandbox.yaml schema:',
    '  version: 1',
    '  web:        # the previewed process',
    '    command: "<start command — must bind 0.0.0.0 and pin port 3000>"',
    '    port: <number>',
    '    health_path: "/"',
    '  workers:    # optional background processes',
    '    - { name: <name>, command: "<cmd>" }',
    '',
    'Suggested starting point:',
    '',
    suggested.trim(),
    ...(snippets && snippets.length
      ? ['', 'Also edit these config files (NOT sandbox.yaml):', ...snippets.map((s) => `- ${s.file}: ${s.note}`)]
      : []),
    ...(notes && notes.length ? ['', 'Notes:', ...notes.map((n) => `- ${n}`)] : []),
    '',
    'Only write sandbox.yaml (and the config edits above); do not run anything destructive.',
  ].join('\n')
}

function RuntimeInspectPanel({
  appId,
  sandboxId,
  onError,
}: {
  appId: string
  sandboxId?: string
  onError: (m: string) => void
}) {
  const [ins, setIns] = useState<RuntimeInspect | null>(null)
  const [man, setMan] = useState<AppManifest | null>(null)
  const [loaded, setLoaded] = useState(false)
  const [copied, setCopied] = useState('') // transient "copied" feedback key
  // explicit Apply: write the suggested sandbox.yaml via the generic PUT /files
  // endpoint (validate-first). Not auto-apply — user clicks + confirms.
  const [applyConfirm, setApplyConfirm] = useState('') // preset id awaiting confirm
  const [applying, setApplying] = useState(false)
  const [applied, setApplied] = useState('') // preset id whose YAML was written

  const load = () => {
    Promise.all([api.runtimeInspect(appId), api.appManifest(appId)])
      .then(([r, m]) => {
        setIns(r)
        setMan(m)
        setLoaded(true)
      })
      .catch((e) => onError((e as Error).message))
  }
  useEffect(load, [appId]) // eslint-disable-line react-hooks/exhaustive-deps

  const copy = (key: string, text: string) => {
    navigator.clipboard?.writeText(text).then(
      () => {
        setCopied(key)
        setTimeout(() => setCopied(''), 2000)
      },
      () => onError('Could not copy to clipboard'),
    )
  }

  // apply writes the suggested sandbox.yaml to the workspace — validate FIRST, then
  // the generic PUT /files endpoint. Explicit (confirmed) adoption; not auto-apply.
  const apply = (presetID: string, yaml: string) => {
    if (!sandboxId || applying) return
    setApplying(true)
    api
      .validateManifest(yaml)
      .then((v) => {
        if (!v.valid) {
          onError('Suggested manifest did not validate: ' + (v.errors[0] || 'invalid'))
          return
        }
        // NOTE: the files API roots at the workspace MOUNT (/home/sandbox), but the
        // app — and the sandbox.yaml runtimed reads — lives at
        // /home/sandbox/workspace/app. So the write path MUST be
        // "workspace/app/sandbox.yaml". Writing bare "sandbox.yaml" lands at
        // /home/sandbox/sandbox.yaml (200, but runtimed never sees it). Do NOT
        // "simplify" this back to "sandbox.yaml".
        return api.putWorkspaceFile(sandboxId, 'workspace/app/sandbox.yaml', yaml).then(() => {
          setApplied(presetID)
          setApplyConfirm('')
          load() // refresh the manifest status
        })
      })
      .catch((e) => onError((e as Error).message))
      .finally(() => setApplying(false))
  }

  if (!loaded) return null

  const v = man?.validation
  const statusLabel = !man
    ? 'unknown'
    : man.present
      ? v?.valid
        ? 'valid'
        : 'invalid'
      : man.source === 'preset'
        ? 'missing — the selected preset applies on first boot'
        : 'missing'
  const eff = man?.effective || v?.effective
  const suggestions = ins?.suggestions || []

  return (
    <div className="card" data-testid="runtime-inspect">
      <h2>Runtime</h2>
      <p className="muted" style={{ fontSize: 12 }}>Advisory — nothing here is applied automatically.</p>

      {/* current sandbox.yaml status */}
      <div data-testid="ri-manifest-status" style={{ fontSize: 13 }}>
        sandbox.yaml: <strong>{statusLabel}</strong>
        {man?.source ? <span className="muted"> (source: {man.source})</span> : null}
      </div>

      {eff?.web && (
        <div data-testid="ri-effective" className="mono" style={{ fontSize: 12, marginTop: 4 }}>
          <div>command: {eff.web.command || '(default)'}</div>
          <div>port: {eff.web.port}</div>
          <div>health: {eff.web.health_path}</div>
        </div>
      )}

      {v && v.errors.length > 0 && (
        <ul data-testid="ri-errors" style={{ marginTop: 6 }}>
          {v.errors.map((e, i) => (
            <li key={i} className="warn" style={{ fontSize: 12 }}>✖ {e}</li>
          ))}
        </ul>
      )}
      {v && v.warnings.length > 0 && (
        <ul data-testid="ri-warnings" style={{ marginTop: 6 }}>
          {v.warnings.map((w, i) => (
            <li key={i} className="warn" style={{ fontSize: 12 }}>⚠ {w}</li>
          ))}
        </ul>
      )}

      {/* recovery hint: preview likely won't come up until a working manifest is adopted */}
      {man && !(man.present && v?.valid) && suggestions.length > 0 && (
        <div data-testid="ri-hint" className="warn" style={{ fontSize: 12, marginTop: 8 }}>
          Preview not coming up? Adopt a suggested <span className="mono">sandbox.yaml</span> below (Copy YAML or Ask
          agent), then restart the sandbox.
        </div>
      )}

      {/* detected stacks */}
      {suggestions.length > 0 && (
        <ul data-testid="ri-suggestions" style={{ marginTop: 8 }}>
          {suggestions.map((s) => (
            <li key={s.preset} style={{ marginBottom: 6 }}>
              <strong>{s.preset}</strong>{' '}
              <span className="muted">
                ({s.confidence}
                {s.preset === ins?.default_suggestion ? ', suggested' : ''}
                {s.runnable ? '' : ', detect-only'})
              </span>
              {s.reasons.length > 0 && <div className="muted" style={{ fontSize: 12 }}>{s.reasons.join('; ')}</div>}
              {s.warnings?.map((w, i) => (
                <div key={i} className="warn" style={{ fontSize: 12 }}>⚠ {w}</div>
              ))}
              {s.suggested_manifest && (
                <div style={{ marginTop: 4 }}>
                  <pre className="mono" data-testid={`ri-suggested-yaml-${s.preset}`} style={{ fontSize: 12, background: 'var(--code-bg,#f6f8fa)', padding: 8, overflow: 'auto' }}>
                    {s.suggested_manifest}
                  </pre>
                  {s.config_snippets?.map((c, i) => (
                    <div key={`snip-${i}`} data-testid={`ri-snippet-${s.preset}`} className="muted" style={{ fontSize: 12 }}>
                      ✎ also edit <span className="mono">{c.file}</span>: {c.note}
                    </div>
                  ))}
                  {s.notes?.map((n, i) => (
                    <div key={i} className="muted" style={{ fontSize: 12 }}>ℹ {n}</div>
                  ))}
                  <button
                    className="btn btn-outline"
                    data-testid={`ri-copy-${s.preset}`}
                    onClick={() => copy(`yaml-${s.preset}`, s.suggested_manifest as string)}
                    style={{ marginRight: 6, fontSize: 12 }}
                  >
                    {copied === `yaml-${s.preset}` ? 'Copied ✓' : 'Copy YAML'}
                  </button>
                  <button
                    className="btn btn-outline"
                    data-testid={`ri-ask-${s.preset}`}
                    onClick={() =>
                      copy(`ask-${s.preset}`, askAgentPrompt(s.suggested_manifest as string, s.notes, s.config_snippets))
                    }
                    style={{ marginRight: 6, fontSize: 12 }}
                    title="Copies a prompt to paste into the task box — does not run a task"
                  >
                    {copied === `ask-${s.preset}` ? 'Prompt copied ✓ — paste into the task box' : 'Ask agent'}
                  </button>
                  {/* Explicit Apply: writes sandbox.yaml to the workspace (validated). */}
                  {applyConfirm === s.preset ? (
                    <span data-testid={`ri-apply-confirm-${s.preset}`} style={{ fontSize: 12 }}>
                      Write <span className="mono">sandbox.yaml</span> to the workspace?{' '}
                      <button
                        className="btn btn-primary"
                        data-testid={`ri-apply-yes-${s.preset}`}
                        disabled={applying}
                        onClick={() => apply(s.preset, s.suggested_manifest as string)}
                      >
                        Confirm
                      </button>{' '}
                      <button className="btn btn-outline" data-testid={`ri-apply-cancel-${s.preset}`} onClick={() => setApplyConfirm('')}>
                        Cancel
                      </button>
                    </span>
                  ) : (
                    <button
                      className="btn btn-primary"
                      data-testid={`ri-apply-${s.preset}`}
                      disabled={!sandboxId}
                      onClick={() => setApplyConfirm(s.preset)}
                      style={{ fontSize: 12 }}
                      title={sandboxId ? 'Writes sandbox.yaml to the workspace' : 'Create a sandbox first'}
                    >
                      Apply sandbox.yaml
                    </button>
                  )}
                  {applied === s.preset && (
                    <div data-testid={`ri-applied-${s.preset}`} className="mono" style={{ fontSize: 12, marginTop: 4 }}>
                      ✓ wrote sandbox.yaml — <strong>restart the sandbox</strong> for it to take effect
                      {s.config_snippets && s.config_snippets.length > 0
                        ? '; also apply the config edit(s) above (Ask agent).'
                        : '.'}
                    </div>
                  )}
                </div>
              )}
            </li>
          ))}
        </ul>
      )}

      {ins?.warnings?.map((w, i) => (
        <div key={i} className="warn" data-testid="ri-warning" style={{ fontSize: 12 }}>⚠ {w}</div>
      ))}

      <div data-testid="ri-restart-note" className="muted" style={{ fontSize: 12, marginTop: 8 }}>
        When <span className="mono">sandbox.yaml</span> changes, restart the sandbox so runtimed re-reads it.
      </div>
    </div>
  )
}

// GitPanel shows READ-ONLY Git status/diff for an imported repo (A2). No
// commit/push — those come later. Status/diff run in-sandbox, so they need a
// running sandbox; otherwise we show the reason.
type DiffEntry = GitDiff | 'loading' | 'error'

// status (not-available) reasons in plain language.
const gitReasonText: Record<string, string> = {
  no_sandbox: 'No sandbox yet — create one to review changes.',
  sandbox_not_running: 'Start the sandbox to review changes.',
  not_a_git_repo: 'This app is not a Git repository.',
  git_error: 'Git could not read the workspace.',
  exec_failed: 'Could not reach the sandbox.',
}

// push (committed:false / pushed:false) reasons in plain language.
const pushReasonText: Record<string, string> = {
  no_local_commits: 'Nothing new to push.',
  branch_exists: 'That branch name already exists — pick another.',
  non_fast_forward: 'The remote already has different changes on that branch.',
  shallow_push_unsupported: "This repo's history is too shallow to push (not supported yet).",
  unsafe_repo_config: 'Unsafe Git config blocked push.',
  auth_failed: 'Authentication failed; check the credential.',
  empty_repo_unsupported: 'Make a first commit in this repo before pushing.',
  no_repo_url: 'This app has no linked repository.',
  no_credential: 'No Git credential is linked to this app.',
  credential_not_found: 'The Git credential was not found — re-add it in Settings.',
  no_workspace: 'No workspace yet.',
  not_a_git_repo: 'This app is not a Git repository.',
  push_failed: 'Push failed. Please try again.',
}

function diffLineStyle(line: string): CSSProperties {
  if (line.startsWith('+') && !line.startsWith('+++')) return { color: '#1a7f37' } // green
  if (line.startsWith('-') && !line.startsWith('---')) return { color: '#cf222e' } // red
  if (line.startsWith('@@')) return { color: '#6639ba' } // hunk header
  return {}
}

function isBinaryDiff(d: GitDiff): boolean {
  return !!d.diff && d.diff.includes('Binary files') && !d.diff.includes('@@')
}

// DiffBlock renders one file's lazily-fetched diff with basic +/- line coloring.
function DiffBlock({ entry, path }: { entry: DiffEntry; path: string }) {
  const tid = `git-filediff-${path}`
  if (entry === 'loading')
    return <div className="muted" data-testid={tid} style={{ fontSize: 12, marginTop: 4 }}>Loading diff…</div>
  if (entry === 'error')
    return <div className="warn" data-testid={tid} style={{ fontSize: 12, marginTop: 4 }}>Couldn’t load this diff — click the file again to retry.</div>
  if (!entry.available)
    return <div className="muted" data-testid={tid} style={{ fontSize: 12, marginTop: 4 }}>Diff unavailable.</div>
  if (isBinaryDiff(entry))
    return <div className="muted" data-testid={tid} style={{ fontSize: 12, marginTop: 4 }}>Binary file — no preview.</div>
  const lines = (entry.diff || '').split('\n')
  return (
    <div data-testid={tid} style={{ marginTop: 4 }}>
      {entry.truncated && (
        <div className="warn" data-testid={`git-filediff-trunc-${path}`} style={{ fontSize: 12 }}>
          Diff truncated — large file.
        </div>
      )}
      <pre className="mono" style={{ fontSize: 12, maxHeight: 280, overflow: 'auto', margin: 0 }}>
        {lines.map((l, i) => (
          <div key={i} style={diffLineStyle(l)}>
            {l || ' '}
          </div>
        ))}
      </pre>
    </div>
  )
}

// FileRow: checkbox to include + a clickable label to expand the per-file diff.
function FileRow({
  file,
  checked,
  onToggleCheck,
  open,
  onToggleDiff,
  diff,
  testidPrefix,
}: {
  file: GitFile
  checked: boolean
  onToggleCheck: () => void
  open: boolean
  onToggleDiff: () => void
  diff: DiffEntry | undefined
  testidPrefix: string
}) {
  return (
    <li className="mono" style={{ marginBottom: 2 }}>
      <div style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
        <input type="checkbox" data-testid={`${testidPrefix}-${file.path}`} checked={checked} onChange={onToggleCheck} />
        <button
          className="linklike"
          data-testid={`git-filerow-${file.path}`}
          onClick={onToggleDiff}
          style={{ background: 'none', border: 'none', padding: 0, cursor: 'pointer', textAlign: 'left', font: 'inherit' }}
        >
          <span className="muted">{file.status}</span> {file.path} <span aria-hidden>{open ? '▾' : '▸'}</span>
        </button>
      </div>
      {open && <DiffBlock entry={diff ?? 'loading'} path={file.path} />}
    </li>
  )
}

function GitPanel({
  appId,
  repoURL,
  onError,
}: {
  appId: string
  repoURL?: string
  onError: (m: string) => void
}) {
  const [st, setSt] = useState<GitStatus | null>(null)
  const [loaded, setLoaded] = useState(false)
  // per-file diffs: which rows are expanded + a persistent cache.
  const [open, setOpen] = useState<Set<string>>(new Set())
  const [diffs, setDiffs] = useState<Record<string, DiffEntry>>({})
  // selection: user files checked by default, runtime files unchecked.
  const [userSel, setUserSel] = useState<Set<string>>(new Set())
  const [rtSel, setRtSel] = useState<Set<string>>(new Set())
  const [message, setMessage] = useState('')
  const [committing, setCommitting] = useState(false)
  const [committedSha, setCommittedSha] = useState('')
  const [committedThisSession, setCommittedThisSession] = useState(false)
  // push (separate, remote write).
  const [pushBranch, setPushBranch] = useState('')
  const [pushConfirm, setPushConfirm] = useState(false)
  const [pushing, setPushing] = useState(false)
  const [pushResult, setPushResult] = useState<GitPushResult | null>(null)

  const load = useCallback(() => {
    api
      .gitStatus(appId)
      .then((s) => {
        setSt(s)
        setLoaded(true)
        setUserSel(new Set((s.files || []).map((f) => f.path))) // user files checked
        setRtSel(new Set()) // runtime files unchecked
        setOpen(new Set()) // collapse + drop stale diffs after a refresh
        setDiffs({})
      })
      .catch((e) => onError((e as Error).message))
  }, [appId, onError])
  useEffect(load, [load])

  const toggle = (set: Set<string>, setter: (s: Set<string>) => void, p: string) => {
    const next = new Set(set)
    next.has(p) ? next.delete(p) : next.add(p)
    setter(next)
  }

  const toggleDiff = (path: string) => {
    const isOpen = open.has(path)
    setOpen((prev) => {
      const n = new Set(prev)
      isOpen ? n.delete(path) : n.add(path)
      return n
    })
    // fetch on first open only; cached diffs (incl. error) are reused.
    if (!isOpen && !(path in diffs)) {
      setDiffs((prev) => ({ ...prev, [path]: 'loading' }))
      api
        .gitDiff(appId, path)
        .then((d) => setDiffs((prev) => ({ ...prev, [path]: d })))
        .catch(() => setDiffs((prev) => ({ ...prev, [path]: 'error' })))
    } else if (!isOpen && diffs[path] === 'error') {
      // allow retry: re-fetch a previously failed diff
      setDiffs((prev) => ({ ...prev, [path]: 'loading' }))
      api
        .gitDiff(appId, path)
        .then((d) => setDiffs((prev) => ({ ...prev, [path]: d })))
        .catch(() => setDiffs((prev) => ({ ...prev, [path]: 'error' })))
    }
  }

  const commit = () => {
    if (committing || pushing || !message.trim() || userSel.size + rtSel.size === 0) return
    setCommitting(true)
    setCommittedSha('')
    api
      .gitCommit(appId, { message: message.trim(), paths: [...userSel], runtime_paths: [...rtSel] })
      .then((r) => {
        if (r.committed) {
          setCommittedSha(r.sha || '')
          setCommittedThisSession(true) // reveals the push section
          setMessage('')
          load()
        } else {
          onError(`Commit not completed: ${pushReasonText[r.reason || ''] || r.reason || 'unknown'}`)
        }
      })
      .catch((e) => onError((e as Error).message))
      .finally(() => setCommitting(false))
  }

  const push = () => {
    if (pushing || committing) return
    setPushing(true)
    setPushResult(null)
    api
      .gitPush(appId, { branch: pushBranch.trim() || undefined })
      .then((r) => {
        setPushResult(r)
        setPushConfirm(false)
        if (r.pushed) load()
      })
      .catch((e) => onError((e as Error).message))
      .finally(() => setPushing(false))
  }

  const userFiles = st?.files || []
  const runtimeFiles = st?.runtime_files || []
  const selectedCount = userSel.size + rtSel.size
  const allUserChecked = userFiles.length > 0 && userFiles.every((f) => userSel.has(f.path))
  const hasChanges = userFiles.length > 0 || runtimeFiles.length > 0

  return (
    <div className="card" data-testid="git-panel">
      <h2>Git review</h2>

      {!loaded ? (
        <div className="muted" data-testid="git-loading" style={{ fontSize: 13 }}>
          Loading changes…
        </div>
      ) : !st?.available ? (
        <div className="muted" data-testid="git-unavailable" style={{ fontSize: 13 }}>
          {gitReasonText[st?.reason || ''] || 'Git status is unavailable.'}
        </div>
      ) : (
        <>
          <div className="row" style={{ alignItems: 'center', justifyContent: 'space-between' }}>
            <div className="mono" style={{ fontSize: 12 }}>
              branch <strong>{st.branch || '—'}</strong>
              {st.head_sha ? ` · ${st.head_sha.slice(0, 7)}` : ''}
              {' · '}
              {st.user_clean ? (
                <span data-testid="git-clean">no changes</span>
              ) : (
                <span data-testid="git-dirty">
                  {userFiles.length} changed file{userFiles.length === 1 ? '' : 's'}
                </span>
              )}
            </div>
            {userFiles.length > 0 && (
              <button
                className="linklike"
                data-testid="git-selectall"
                onClick={() => setUserSel(allUserChecked ? new Set() : new Set(userFiles.map((f) => f.path)))}
                style={{ background: 'none', border: 'none', cursor: 'pointer', fontSize: 12 }}
              >
                {allUserChecked ? 'Select none' : 'Select all'}
              </button>
            )}
          </div>

          {!hasChanges && (
            <div className="muted" data-testid="git-no-changes" style={{ fontSize: 13, marginTop: 6 }}>
              No changes to commit.
            </div>
          )}

          {userFiles.length > 0 && (
            <ul data-testid="git-files" style={{ marginTop: 8, fontSize: 13, listStyle: 'none', paddingLeft: 0 }}>
              {userFiles.map((f) => (
                <FileRow
                  key={f.path}
                  file={f}
                  checked={userSel.has(f.path)}
                  onToggleCheck={() => toggle(userSel, setUserSel, f.path)}
                  open={open.has(f.path)}
                  onToggleDiff={() => toggleDiff(f.path)}
                  diff={diffs[f.path]}
                  testidPrefix="git-pick"
                />
              ))}
            </ul>
          )}

          {runtimeFiles.length > 0 && (
            <details data-testid="git-runtime-files" style={{ marginTop: 8 }}>
              <summary className="muted" style={{ fontSize: 12 }}>
                {runtimeFiles.length} generated file{runtimeFiles.length === 1 ? '' : 's'} (sandbox.yaml, lockfiles, caches — not your edits)
              </summary>
              <ul style={{ fontSize: 13, listStyle: 'none', paddingLeft: 0, marginTop: 4 }}>
                {runtimeFiles.map((f) => (
                  <FileRow
                    key={f.path}
                    file={f}
                    checked={rtSel.has(f.path)}
                    onToggleCheck={() => toggle(rtSel, setRtSel, f.path)}
                    open={open.has(f.path)}
                    onToggleDiff={() => toggleDiff(f.path)}
                    diff={diffs[f.path]}
                    testidPrefix="git-rtpick"
                  />
                ))}
              </ul>
            </details>
          )}

          {/* Commit — uses the selected files only. */}
          {hasChanges && (
            <div data-testid="git-commit-box" style={{ marginTop: 12, borderTop: '1px solid var(--border, #ddd)', paddingTop: 8 }}>
              <input
                className="input"
                placeholder="Commit message…"
                value={message}
                onChange={(e) => setMessage(e.target.value)}
                data-testid="git-commit-message"
                style={{ width: '100%' }}
              />
              <button
                className="btn btn-primary"
                data-testid="git-commit"
                disabled={committing || pushing || !message.trim() || selectedCount === 0}
                onClick={commit}
                style={{ marginTop: 8 }}
              >
                Commit {selectedCount} selected file{selectedCount === 1 ? '' : 's'}
              </button>
              {committedSha && (
                <div className="mono" data-testid="git-committed-sha" style={{ fontSize: 12, marginTop: 8 }}>
                  ✓ committed {committedSha.slice(0, 7)}
                </div>
              )}
            </div>
          )}

          {/* Push — separate; only after a commit lands this session. */}
          {repoURL &&
            (committedThisSession ? (
              <div data-testid="git-push-box" style={{ marginTop: 12, borderTop: '2px solid var(--warn, #e0a800)', paddingTop: 8 }}>
                <div style={{ fontSize: 13, fontWeight: 600 }}>Push to remote</div>
                <div className="muted" data-testid="git-push-explain" style={{ fontSize: 12, marginTop: 2 }}>
                  Creates a <strong>new branch</strong> on <span className="mono">{repoURL}</span> (auto-named if blank).
                  Your main / import branch is untouched. No force. Open a pull request afterward.
                </div>
                <input
                  className="input"
                  placeholder="new branch (auto)"
                  value={pushBranch}
                  onChange={(e) => setPushBranch(e.target.value)}
                  data-testid="git-push-branch"
                  style={{ width: '100%', marginTop: 8 }}
                />
                {!pushConfirm ? (
                  <button
                    className="btn btn-outline"
                    data-testid="git-push-start"
                    disabled={committing || pushing}
                    onClick={() => setPushConfirm(true)}
                    style={{ marginTop: 8 }}
                  >
                    Push to a new branch…
                  </button>
                ) : (
                  <div data-testid="git-push-confirm" style={{ marginTop: 8 }}>
                    <span style={{ fontSize: 13 }}>This writes a new branch to the remote. Continue?</span>{' '}
                    <button className="btn btn-primary" data-testid="git-push-confirm-yes" disabled={pushing || committing} onClick={push}>
                      Confirm push
                    </button>{' '}
                    <button className="btn btn-outline" data-testid="git-push-cancel" onClick={() => setPushConfirm(false)}>
                      Cancel
                    </button>
                  </div>
                )}
                {pushResult &&
                  (pushResult.pushed ? (
                    <div className="mono" data-testid="git-push-result" style={{ fontSize: 12, marginTop: 8 }}>
                      ✓ pushed {pushResult.commits} commit{pushResult.commits === 1 ? '' : 's'} to{' '}
                      <strong>{pushResult.branch}</strong> — open a pull request on your Git host.
                    </div>
                  ) : (
                    <div className="warn" data-testid="git-push-reason" style={{ fontSize: 12, marginTop: 8 }}>
                      {pushReasonText[pushResult.reason || ''] || `Push not completed: ${pushResult.reason}`}
                    </div>
                  ))}
              </div>
            ) : (
              <div className="muted" data-testid="git-push-help" style={{ fontSize: 12, marginTop: 12 }}>
                Push appears after you commit changes in this session.
              </div>
            ))}
        </>
      )}
    </div>
  )
}

// ProcessesPanel shows the sandbox's supervised processes (web + workers) from
// the runtime manifest, and lets you tail each process's recent logs. A
// worker-only app (no web) is valid here — its worker simply shows as running
// with no public endpoint.
function ProcessesPanel({ sandbox, onError }: { sandbox: Sandbox | null; onError: (m: string) => void }) {
  const [logsFor, setLogsFor] = useState<string | null>(null)
  const [logLines, setLogLines] = useState<string[]>([])
  const [busy, setBusy] = useState(false)
  if (!sandbox) return null
  const procs = sandbox.processes ?? []

  const viewLogs = async (name: string) => {
    setBusy(true)
    setLogsFor(name)
    setLogLines([])
    try {
      const r = await api.getProcessLogs(sandbox.id, name, 200)
      setLogLines(r.lines)
    } catch (e) {
      onError((e as Error).message)
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="card" data-testid="processes-panel">
      <h2 className="card-title">Processes</h2>
      {procs.length === 0 ? (
        <p className="muted" data-testid="processes-empty">
          No processes reported (sandbox stopped or still starting).
        </p>
      ) : (
        <table className="config-table" data-testid="processes-list">
          <thead>
            <tr>
              <th>Name</th>
              <th>Kind</th>
              <th>Status</th>
              <th>PID</th>
              <th>Restarts</th>
              <th />
            </tr>
          </thead>
          <tbody>
            {procs.map((p) => (
              <tr key={p.name} data-testid={`process-${p.name}`}>
                <td className="mono">{p.name}</td>
                <td>{p.kind}</td>
                <td>
                  <span className={`badge ${p.running ? 'running' : 'stopped'}`}>
                    {p.running ? 'running' : 'stopped'}
                  </span>
                </td>
                <td className="muted mono">{p.pid || '—'}</td>
                <td className="muted">{p.restarts}</td>
                <td>
                  <button
                    className="btn btn-ghost btn-sm"
                    disabled={busy}
                    data-testid={`process-logs-${p.name}`}
                    onClick={() => viewLogs(p.name)}
                  >
                    Logs
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
      {logsFor && (
        <div className="log mono" data-testid="process-log-output" style={{ marginTop: 10 }}>
          <div className="row">
            <strong>{logsFor} — recent logs</strong>
            <div className="spacer" />
            <button className="btn btn-ghost btn-sm" onClick={() => setLogsFor(null)}>
              close
            </button>
          </div>
          {logLines.length === 0 ? (
            <div className="muted">{busy ? 'Loading…' : '(no output)'}</div>
          ) : (
            logLines.map((l, i) => (
              <div key={i} className="ev">
                {l}
              </div>
            ))
          )}
        </div>
      )}
    </div>
  )
}

// ActivityPanel renders the durable event timeline (newest-first). It is
// backed by SQLite, so it survives page refresh and server restart. Failed
// events are flagged by severity. Read-only.
function ActivityPanel({
  appId,
  reloadKey,
  onError,
}: {
  appId: string
  reloadKey: number
  onError: (m: string) => void
}) {
  const [evts, setEvts] = useState<AppEvent[] | null>(null)

  const load = useCallback(() => {
    api
      .listAppEvents(appId)
      .then(setEvts)
      .catch((e) => onError((e as Error).message))
  }, [appId, onError])
  useEffect(load, [load, reloadKey])
  useEffect(() => {
    const t = setInterval(load, 6000) // reflect new activity
    return () => clearInterval(t)
  }, [load])

  const sev = (s: string) => (s === 'error' ? 'ev-error' : s === 'warning' ? 'ev-warn' : 'ev-info')

  return (
    <div className="card" data-testid="activity-panel">
      <div className="row">
        <h2 className="card-title">Activity</h2>
        <div className="spacer" />
        <span className="muted" style={{ fontSize: 12 }}>Durable timeline — survives restarts.</span>
      </div>
      {evts === null ? (
        <p className="muted">Loading…</p>
      ) : evts.length === 0 ? (
        <p className="muted" data-testid="activity-empty">
          No activity yet.
        </p>
      ) : (
        <div className="timeline" data-testid="activity-list">
          {evts.map((e) => (
            <div key={e.id} className={`tl-row ${sev(e.severity)}`} data-testid={`event-${e.type}`}>
              <span className="tl-time muted mono">{new Date(e.created_at).toLocaleString()}</span>
              <span className="tl-type mono">{e.type}</span>
              <span className="tl-msg">{e.message}</span>
              {(e.task_id || e.sandbox_id || e.snapshot_id) && (
                <span className="tl-ids muted mono">
                  {e.task_id ? `task:${e.task_id.slice(0, 8)} ` : ''}
                  {e.sandbox_id ? `sb:${e.sandbox_id.slice(0, 8)} ` : ''}
                  {e.snapshot_id ? `snap:${e.snapshot_id.slice(0, 8)}` : ''}
                </span>
              )}
            </div>
          ))}
        </div>
      )}
    </div>
  )
}

// SnapshotsPanel shows an app's snapshot history and the two recovery
// actions. Restore is destructive (replaces the current sandbox), so it
// confirms first. Fork spins a brand-new app from the snapshot.
function SnapshotsPanel({
  appId,
  appName,
  reloadKey,
  onError,
  onInfo,
  onChanged,
}: {
  appId: string
  appName: string
  reloadKey: number
  onError: (m: string) => void
  onInfo: (m: string) => void
  onChanged: () => void
}) {
  const [snaps, setSnaps] = useState<Snapshot[] | null>(null)
  const [busy, setBusy] = useState(false)

  const load = useCallback(() => {
    api
      .listAppSnapshots(appId)
      .then(setSnaps)
      .catch((e) => onError((e as Error).message))
  }, [appId, onError])
  useEffect(load, [load, reloadKey])

  const restore = async (snap: Snapshot) => {
    if (
      !window.confirm(
        `Restore "${snap.name}"?\n\nThis REPLACES the app's current sandbox and permanently discards any work that has not been snapshotted. Continue?`,
      )
    ) {
      return
    }
    setBusy(true)
    try {
      await api.restoreApp(appId, snap.id)
      onInfo('Restored from snapshot — a fresh sandbox was created.')
      onChanged()
    } catch (e) {
      onError((e as Error).message)
    } finally {
      setBusy(false)
    }
  }

  const fork = async (snap: Snapshot) => {
    const name = window.prompt('Fork into a new app named:', `${appName} fork`)
    if (name === null || !name.trim()) return
    setBusy(true)
    try {
      const res = await api.forkApp(appId, snap.id, name.trim())
      onInfo(`Forked into new app "${res.app?.name ?? name.trim()}".`)
    } catch (e) {
      onError((e as Error).message)
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="card" data-testid="snapshots-panel">
      <div className="row">
        <h2 className="card-title">Snapshots</h2>
        <div className="spacer" />
        <span className="muted" style={{ fontSize: 12 }}>
          Capture from the controls above (stop the sandbox first). Restore replaces the
          current sandbox; fork creates a new app.
        </span>
      </div>
      {snaps === null ? (
        <p className="muted">Loading…</p>
      ) : snaps.length === 0 ? (
        <p className="muted" data-testid="snapshots-empty">
          No snapshots yet.
        </p>
      ) : (
        <table className="config-table" data-testid="snapshots-list">
          <thead>
            <tr>
              <th>Name</th>
              <th>Captured</th>
              <th>Size</th>
              <th />
            </tr>
          </thead>
          <tbody>
            {snaps.map((s) => (
              <tr key={s.id} data-testid={`snapshot-row-${s.id}`}>
                <td className="mono">{s.name}</td>
                <td className="muted">{new Date(s.created_at).toLocaleString()}</td>
                <td className="muted">{s.size_bytes ? `${Math.round(s.size_bytes / 1024)} KB` : '—'}</td>
                <td className="row" style={{ gap: 4 }}>
                  <button
                    className="btn btn-outline btn-sm"
                    disabled={busy || s.status !== 'ready'}
                    data-testid={`snapshot-restore-${s.id}`}
                    onClick={() => restore(s)}
                  >
                    Restore
                  </button>
                  <button
                    className="btn btn-ghost btn-sm"
                    disabled={busy || s.status !== 'ready'}
                    data-testid={`snapshot-fork-${s.id}`}
                    onClick={() => fork(s)}
                  >
                    Fork
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  )
}

// ConfigPanel manages an app's config & secrets. Secrets are write-only:
// the API never returns a sensitive value, so a stored secret shows as
// "•••• set" and can only be replaced, never read back.
function ConfigPanel({ appId, onError }: { appId: string; onError: (m: string) => void }) {
  const [items, setItems] = useState<ConfigItem[] | null>(null)
  const [busy, setBusy] = useState(false)
  const [key, setKey] = useState('')
  const [value, setValue] = useState('')
  const [sensitive, setSensitive] = useState(true)
  const [policy, setPolicy] = useState<AccessPolicy>('control_plane_only')
  const [editKey, setEditKey] = useState<string | null>(null) // row whose value is being replaced
  const [editValue, setEditValue] = useState('')

  const load = useCallback(() => {
    api
      .listConfig(appId)
      .then(setItems)
      .catch((e) => onError((e as Error).message))
  }, [appId, onError])
  useEffect(load, [load])

  const act = async (fn: () => Promise<unknown>) => {
    setBusy(true)
    try {
      await fn()
      load()
    } catch (e) {
      onError((e as Error).message)
    } finally {
      setBusy(false)
    }
  }

  const add = async () => {
    if (!key.trim()) return
    await act(async () => {
      await api.createConfig(appId, { key: key.trim(), value, sensitive, access_policy: policy })
      setKey('')
      setValue('')
    })
  }

  return (
    <div className="card" data-testid="config-panel">
      <div className="row">
        <h2 className="card-title">Config &amp; Secrets</h2>
        <div className="spacer" />
        <span className="muted" style={{ fontSize: 12 }}>
          Secrets are encrypted at rest and never shown again.
        </span>
      </div>
      <p className="muted" style={{ fontSize: 12, marginTop: 4 }} data-testid="config-broker-note">
        Stored in sandboxd only. <code>control_plane_only</code> is the one policy
        enforced today — delivery to agents and app runtimes arrives with the
        secrets broker (not yet implemented), so the other policies are reserved.
      </p>

      {items === null ? (
        <p className="muted">Loading…</p>
      ) : items.length === 0 ? (
        <p className="muted" data-testid="config-empty">
          No config yet. Add a key below.
        </p>
      ) : (
        <table className="config-table" data-testid="config-list">
          <thead>
            <tr>
              <th>Key</th>
              <th>Value</th>
              <th>Access policy</th>
              <th />
            </tr>
          </thead>
          <tbody>
            {items.map((it) => (
              <tr key={it.key} data-testid={`config-row-${it.key}`}>
                <td className="mono">{it.key}</td>
                <td className="mono">
                  {editKey === it.key ? (
                    <span className="row" style={{ gap: 6, flexWrap: 'nowrap' }}>
                      <input
                        className="input mono"
                        type={it.sensitive ? 'password' : 'text'}
                        placeholder={it.sensitive ? 'new secret value' : 'new value'}
                        value={editValue}
                        autoFocus
                        disabled={busy}
                        onChange={(e) => setEditValue(e.target.value)}
                        data-testid={`config-edit-value-${it.key}`}
                      />
                      <button
                        className="btn btn-primary btn-sm"
                        disabled={busy}
                        data-testid={`config-save-${it.key}`}
                        onClick={() =>
                          act(async () => {
                            await api.patchConfig(appId, it.key, { value: editValue })
                            setEditKey(null)
                            setEditValue('')
                          })
                        }
                      >
                        Save
                      </button>
                      <button
                        className="btn btn-ghost btn-sm"
                        disabled={busy}
                        onClick={() => {
                          setEditKey(null)
                          setEditValue('')
                        }}
                      >
                        Cancel
                      </button>
                    </span>
                  ) : it.sensitive ? (
                    <span className="tag" title="Encrypted at rest; write-only">
                      •••• {it.value_set ? 'set' : 'empty'}
                    </span>
                  ) : (
                    <span>{it.value}</span>
                  )}
                </td>
                <td>
                  <select
                    className="input"
                    value={it.access_policy}
                    disabled={busy}
                    data-testid={`config-policy-${it.key}`}
                    onChange={(e) =>
                      act(() => api.patchConfig(appId, it.key, { access_policy: e.target.value as AccessPolicy }))
                    }
                  >
                    {ACCESS_POLICIES.map((p) => (
                      <option key={p} value={p} disabled={policyReserved(p) && it.access_policy !== p}>
                        {policyLabel(p)}
                      </option>
                    ))}
                  </select>
                </td>
                <td className="row" style={{ gap: 4 }}>
                  <button
                    className="btn btn-ghost btn-sm"
                    disabled={busy || editKey === it.key}
                    data-testid={`config-replace-${it.key}`}
                    onClick={() => {
                      setEditKey(it.key)
                      setEditValue('')
                    }}
                  >
                    {it.sensitive ? 'Replace' : 'Edit'}
                  </button>
                  <button
                    className="btn btn-ghost btn-sm"
                    disabled={busy}
                    data-testid={`config-delete-${it.key}`}
                    onClick={() => act(() => api.deleteConfig(appId, it.key))}
                  >
                    Delete
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}

      <div className="row config-add" style={{ marginTop: 12, flexWrap: 'wrap', gap: 8 }}>
        <input
          className="input mono"
          placeholder="KEY"
          value={key}
          onChange={(e) => setKey(e.target.value)}
          data-testid="config-key"
        />
        <input
          className="input mono"
          type={sensitive ? 'password' : 'text'}
          placeholder={sensitive ? 'secret value' : 'value'}
          value={value}
          onChange={(e) => setValue(e.target.value)}
          data-testid="config-value"
        />
        <select
          className="input"
          value={policy}
          onChange={(e) => setPolicy(e.target.value as AccessPolicy)}
          data-testid="config-new-policy"
        >
          {ACCESS_POLICIES.map((p) => (
            <option key={p} value={p} disabled={policyReserved(p)}>
              {policyLabel(p)}
            </option>
          ))}
        </select>
        <label className="muted" style={{ display: 'flex', alignItems: 'center', gap: 6, fontSize: 13 }}>
          <input
            type="checkbox"
            checked={sensitive}
            onChange={(e) => setSensitive(e.target.checked)}
            data-testid="config-sensitive"
          />
          secret
        </label>
        <button
          className="btn btn-primary"
          disabled={busy || !key.trim()}
          onClick={add}
          data-testid="config-add"
        >
          Add
        </button>
      </div>
    </div>
  )
}

function TaskPanel({
  sandboxId,
  running,
  onError,
}: {
  sandboxId?: string
  running: boolean
  onError: (m: string) => void
}) {
  const [prompt, setPrompt] = useState('')
  const [agent, setAgent] = useState('opencode')
  const [status, setStatus] = useState<string | null>(null)
  const [log, setLog] = useState<string[]>([])
  const esRef = useRef<EventSource | null>(null)
  const logRef = useRef<HTMLDivElement | null>(null)

  useEffect(() => () => esRef.current?.close(), [])
  useEffect(() => {
    if (logRef.current) logRef.current.scrollTop = logRef.current.scrollHeight
  }, [log])

  const run = async () => {
    if (!sandboxId || !prompt.trim()) return
    setLog([])
    setStatus('running')
    try {
      const t = await api.submitTask(sandboxId, prompt.trim(), agent)
      esRef.current?.close()
      const es = new EventSource(api.taskEventsURL(sandboxId, t.id))
      esRef.current = es
      for (const type of ['status', 'message', 'tool', 'build']) {
        es.addEventListener(type, (m) => setLog((l) => [...l, `[${type}] ${(m as MessageEvent).data}`]))
      }
      es.addEventListener('done', () => {
        es.close()
        api
          .getTask(sandboxId, t.id)
          .then((r) => {
            // Honest build label: a skipped build (e.g. Next.js) is not "passed".
            const build =
              r.build_status === 'passed'
                ? ' · build passed'
                : r.build_status === 'failed'
                  ? ' · build failed'
                  : r.build_status === 'skipped'
                    ? ' · build skipped'
                    : ''
            const health = r.app_healthy === false ? ' · unhealthy' : ''
            setStatus(r.status + build + health)
          })
          .catch(() => setStatus('done'))
      })
      es.onerror = () => es.close()
    } catch (e) {
      onError((e as Error).message)
      setStatus(null)
    }
  }

  return (
    <div className="card">
      <h2>Task</h2>
      <textarea
        className="textarea"
        placeholder="Describe a change — e.g. “add a dark-mode toggle”"
        value={prompt}
        onChange={(e) => setPrompt(e.target.value)}
        data-testid="task-prompt"
      />
      <div className="row" style={{ marginTop: 10 }}>
        <select
          className="select"
          value={agent}
          onChange={(e) => setAgent(e.target.value)}
          data-testid="task-agent"
          title="Which coding agent runs this task"
        >
          <option value="opencode">OpenCode</option>
          <option value="claude-code">Claude Code (your subscription)</option>
        </select>
        <button className="btn btn-primary" disabled={!running || !prompt.trim()} onClick={run} data-testid="run-task">
          Run task
        </button>
        <div className="spacer" />
        {status && (
          <span className="badge" data-testid="task-status">
            {status}
          </span>
        )}
      </div>
      {!running && <div className="muted" style={{ fontSize: 12, marginTop: 8 }}>Start the sandbox to run a task.</div>}
      {log.length > 0 && (
        <div className="log mono" ref={logRef} data-testid="task-log" style={{ marginTop: 12 }}>
          {log.map((l, i) => (
            <div key={i} className="ev">
              {l}
            </div>
          ))}
        </div>
      )}
    </div>
  )
}
