import { useCallback, useEffect, useState } from 'react'
import { api, ModelProvider, ModelProviderInput } from './api'
import { Btn, Card, H, Input, Pill, c, mono } from './design/kit'

const emptyModel = () => ({ id: '', context_window: 32768, max_tokens: 4096 })
const emptyInput = (): ModelProviderInput => ({
  name: '', protocol: 'openai-completions', base_url: '', models: [emptyModel()],
})
const fieldStyle = { display: 'grid', gap: 5, fontSize: 12.5, color: c.muted } as const
const selectStyle = { padding: '8px 10px', borderRadius: 7, border: `1px solid ${c.border2}`, background: c.bg, color: c.fg }

export function ModelProviders({ onError, toast }: { onError: (message: string) => void; toast: (message: string) => void }) {
  const [providers, setProviders] = useState<ModelProvider[]>([])
  const [loading, setLoading] = useState(true)
  const [editing, setEditing] = useState<string | null>(null)
  const [form, setForm] = useState<ModelProviderInput>(emptyInput)
  const [key, setKey] = useState('')
  const [clearKey, setClearKey] = useState(false)
  const [busy, setBusy] = useState('')
  const [confirmDelete, setConfirmDelete] = useState('')
  const [testResults, setTestResults] = useState<Record<string, string>>({})
  const [testModels, setTestModels] = useState<Record<string, string>>({})

  const load = useCallback(async () => {
    try { setProviders(await api.listModelProviders()) }
    catch (e) { onError((e as Error).message) }
    finally { setLoading(false) }
  }, [onError])
  useEffect(() => { void load() }, [load])

  const edit = (p?: ModelProvider) => {
    setEditing(p?.id || '')
    setForm(p ? { name: p.name, protocol: p.protocol, base_url: p.base_url, models: p.models.map((m) => ({ ...m })) } : emptyInput())
    setKey(''); setClearKey(false)
  }
  const save = async () => {
    setBusy('save')
    try {
      const input = { ...form, ...(key ? { api_key: key } : clearKey ? { api_key: '' } : {}) }
      await api.saveModelProvider(editing || undefined, input)
      setEditing(null); setKey(''); setClearKey(false)
      await load(); toast('Model connection saved')
    } catch (e) { onError((e as Error).message) }
    finally { setBusy('') }
  }
  const remove = async (id: string) => {
    setBusy(id)
    try {
      await api.deleteModelProvider(id); setConfirmDelete('')
      await load(); toast('Model connection removed')
    } catch (e) { onError((e as Error).message) }
    finally { setBusy('') }
  }
  const test = async (p: ModelProvider) => {
    setBusy(p.id)
    setTestResults((r) => ({ ...r, [p.id]: 'Sending a small test request…' }))
    try {
      await api.testModelProvider(p.id, testModels[p.id] || p.models[0].id)
      setTestResults((r) => ({ ...r, [p.id]: 'Model responded successfully.' }))
    } catch (e) { setTestResults((r) => ({ ...r, [p.id]: (e as Error).message })) }
    finally { setBusy('') }
  }
  const updateModel = (index: number, patch: Partial<ModelProviderInput['models'][number]>) =>
    setForm((f) => ({ ...f, models: f.models.map((m, i) => i === index ? { ...m, ...patch } : m) }))

  return <Card style={{ padding: 16, marginBottom: 12 }} data-testid="settings-model-providers">
    <div style={{ display: 'flex', alignItems: 'center', gap: 12 }}>
      <H>Model connections</H>
      {editing === null && <Btn sm style={{ marginLeft: 'auto' }} onClick={() => edit()}>Add connection</Btn>}
    </div>
    <p style={{ color: c.muted, fontSize: 12.5, lineHeight: 1.6 }}>
      Use local or remote models with <b>OpenCode</b> and <b>Pi</b>. Connect an OpenAI-compatible
      or Anthropic-compatible API, including Ollama, LM Studio, vLLM and LiteLLM gateways.
      API keys stay encrypted on the control plane.
    </p>
    {loading && <p role="status">Loading connections…</p>}
    {!loading && providers.length === 0 && editing === null &&
      <div style={{ padding: 16, background: c.panel2, borderRadius: 8, color: c.muted, fontSize: 12.5 }}>
        Add your first connection, then choose it in an app’s Agent panel.
      </div>}
    {providers.map((p) => <div key={p.id} style={{ borderTop: `1px solid ${c.border}`, padding: '14px 0' }}>
      <div style={{ display: 'flex', alignItems: 'center', gap: 8, flexWrap: 'wrap' }}>
        <b style={{ fontSize: 13 }}>{p.name}</b>
        <Pill tone="neutral">{p.protocol === 'openai-completions' ? 'OpenAI-compatible' : 'Anthropic-compatible'}</Pill>
        <span style={{ color: c.muted2, fontSize: 11.5 }}>{p.has_api_key ? 'API key saved' : 'No API key'}</span>
        <Btn sm variant="ghost" style={{ marginLeft: 'auto' }} disabled={!!busy} onClick={() => edit(p)}>Edit</Btn>
        <Btn sm variant="ghost" disabled={!!busy} onClick={() => setConfirmDelete(p.id)}>Remove</Btn>
      </div>
      <div style={{ ...mono, color: c.muted, fontSize: 11.5, margin: '7px 0', overflowWrap: 'anywhere' }}>{p.base_url}</div>
      <div style={{ display: 'flex', gap: 8, alignItems: 'center', flexWrap: 'wrap' }}>
        <select aria-label={`Test model for ${p.name}`} style={{ ...selectStyle, maxWidth: '100%' }}
          value={testModels[p.id] || p.models[0].id} onChange={(e) => setTestModels((m) => ({ ...m, [p.id]: e.target.value }))}>
          {p.models.map((m) => <option key={m.id} value={m.id}>{m.id}</option>)}
        </select>
        <Btn sm variant="ghost" disabled={!!busy} onClick={() => void test(p)}>Test model</Btn>
        <span style={{ color: c.muted2, fontSize: 11 }}>Sends a small request; provider charges may apply.</span>
      </div>
      {testResults[p.id] && <div role="status" style={{ marginTop: 8, fontSize: 12 }}>{testResults[p.id]}</div>}
      {confirmDelete === p.id && <div role="alert" style={{ padding: 10, marginTop: 8, background: c.panel2, borderRadius: 6, fontSize: 12 }}>
        Remove this connection? Active tasks using it will lose access.
        <div style={{ display: 'flex', gap: 8, marginTop: 8 }}>
          <Btn sm disabled={!!busy} onClick={() => void remove(p.id)}>Remove connection</Btn>
          <Btn sm variant="ghost" onClick={() => setConfirmDelete('')}>Cancel</Btn>
        </div>
      </div>}
    </div>)}
    {editing !== null && <form onSubmit={(e) => { e.preventDefault(); void save() }}
      style={{ display: 'grid', gap: 14, marginTop: 16, paddingTop: 14, borderTop: `1px solid ${c.border}` }}>
      <H size={13}>{editing ? 'Edit connection' : 'New connection'}</H>
      <label style={fieldStyle}>Connection name
        <Input required maxLength={100} value={form.name} placeholder="Local Ollama or team LiteLLM"
          onChange={(e) => setForm({ ...form, name: e.target.value })} />
      </label>
      <label style={fieldStyle}>API format
        <select style={selectStyle} value={form.protocol}
          onChange={(e) => setForm({ ...form, protocol: e.target.value as ModelProviderInput['protocol'] })}>
          <option value="openai-completions">OpenAI-compatible · Chat Completions</option>
          <option value="anthropic-messages">Anthropic-compatible · Messages</option>
        </select>
      </label>
      <label style={fieldStyle}>API base URL
        <Input required type="url" value={form.base_url} placeholder="http://ollama:11434/v1"
          onChange={(e) => setForm({ ...form, base_url: e.target.value })} />
        <span style={{ fontSize: 11.5 }}>Include the API prefix, usually /v1. This address must be reachable from the control plane.
          For a model on your host, use a host address reachable from its container.</span>
      </label>
      <label style={fieldStyle}>API key <span style={{ fontSize: 11.5 }}>(optional for local servers)</span>
        <Input type="password" autoComplete="new-password" value={key}
          placeholder={editing ? 'Leave blank to keep the saved key' : 'Paste an API key, if required'}
          onChange={(e) => { setKey(e.target.value); setClearKey(false) }} />
      </label>
      {editing && <label style={{ fontSize: 12, color: c.muted }}>
        <input type="checkbox" checked={clearKey} onChange={(e) => { setClearKey(e.target.checked); setKey('') }} /> Remove the saved API key
      </label>}
      <div style={{ display: 'grid', gap: 10 }}>
        <H size={13}>Models</H>
        <div style={{ fontSize: 11.5, color: c.muted }}>Use the server’s exact model ID or gateway alias. The first model is the connection default.</div>
        {form.models.map((m, i) => <div key={i} style={{ padding: 12, background: c.panel2, borderRadius: 8 }}>
          <label style={fieldStyle}>Model ID {i + 1}
            <Input required maxLength={200} value={m.id} placeholder="qwen2.5-coder:7b or team/model-alias"
              onChange={(e) => updateModel(i, { id: e.target.value })} />
          </label>
          <details style={{ marginTop: 8, fontSize: 12, color: c.muted }}>
            <summary style={{ cursor: 'pointer' }}>Token limits · {m.context_window.toLocaleString()} context / {m.max_tokens.toLocaleString()} output</summary>
            <div style={{ display: 'flex', gap: 12, flexWrap: 'wrap', marginTop: 10 }}>
              <label style={fieldStyle}>Context window
                <Input type="number" required min={512} max={10000000} value={m.context_window}
                  onChange={(e) => updateModel(i, { context_window: Number(e.target.value) })} style={{ width: 140 }} />
              </label>
              <label style={fieldStyle}>Maximum output
                <Input type="number" required min={1} max={m.context_window - 1} value={m.max_tokens}
                  onChange={(e) => updateModel(i, { max_tokens: Number(e.target.value) })} style={{ width: 140 }} />
              </label>
            </div>
          </details>
          {form.models.length > 1 && <Btn sm type="button" variant="ghost" style={{ marginTop: 8 }}
            onClick={() => setForm({ ...form, models: form.models.filter((_, n) => n !== i) })}>Remove model</Btn>}
        </div>)}
        <Btn sm type="button" variant="ghost" disabled={form.models.length >= 100}
          onClick={() => setForm({ ...form, models: [...form.models, emptyModel()] })}>Add model</Btn>
      </div>
      {editing && <div style={{ color: c.muted, fontSize: 11.5 }}>Saving changes revokes existing task access. Start a new task to use the updated connection.</div>}
      <div style={{ display: 'flex', gap: 8 }}>
        <Btn type="submit" disabled={!!busy}>{busy === 'save' ? 'Saving…' : 'Save connection'}</Btn>
        <Btn type="button" variant="ghost" disabled={!!busy} onClick={() => { setEditing(null); setKey('') }}>Cancel</Btn>
      </div>
    </form>}
  </Card>
}
