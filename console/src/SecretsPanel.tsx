import { useCallback, useEffect, useState } from 'react'
import { api, AccessPolicy, ConfigItem } from './api'
import { Btn, Input } from './design/kit'
import { Icon } from './design/Icon'
import { Dialog } from './design/Dialog'

const policies: Record<AccessPolicy, string> = {
  control_plane_only: 'Control plane only',
  agent_access: 'Agent access',
  runtime_access: 'Runtime access',
  both: 'Agent & runtime access',
}
export function SecretsPanel({
  appId,
  onError,
  toast,
}: {
  appId: string
  onError: (m: string) => void
  toast: (m: string) => void
}) {
  const [items, setItems] = useState<ConfigItem[]>([])
  const [loading, setLoading] = useState(true)
  const [editing, setEditing] = useState<ConfigItem | 'new' | null>(null)
  const [removing, setRemoving] = useState<ConfigItem | null>(null)
  const [key, setKey] = useState('')
  const [value, setValue] = useState('')
  const [secret, setSecret] = useState(true)
  const [policy, setPolicy] = useState<AccessPolicy>('control_plane_only')
  const [replace, setReplace] = useState(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const load = useCallback(async () => {
    try {
      setItems(await api.listConfig(appId))
    } catch (e) {
      onError((e as Error).message)
    } finally {
      setLoading(false)
    }
  }, [appId, onError])
  useEffect(() => {
    void load()
  }, [load])
  const edit = (item?: ConfigItem) => {
    setEditing(item || 'new')
    setKey(item?.key || '')
    setValue('')
    setSecret(item?.sensitive ?? true)
    setPolicy(item?.access_policy || 'control_plane_only')
    setReplace(!item)
    setError('')
  }
  const save = async () => {
    if (busy) return
    setBusy(true)
    setError('')
    try {
      if (editing === 'new')
        await api.createConfig(appId, { key: key.trim(), value, sensitive: secret, access_policy: policy })
      else await api.patchConfig(appId, key, { access_policy: policy, ...(replace ? { value } : {}) })
      setValue('')
      setEditing(null)
      toast('Configuration saved')
      await load()
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setBusy(false)
    }
  }
  const remove = async () => {
    if (!removing || busy) return
    setBusy(true)
    try {
      await api.deleteConfig(appId, removing.key)
      setRemoving(null)
      toast('Configuration removed')
      await load()
    } catch (e) {
      onError((e as Error).message)
    } finally {
      setBusy(false)
    }
  }
  return (
    <section className="workspace-settings" data-testid="config-panel">
      <div className="section-heading">
        <div>
          <div className="eyebrow">PROJECT CONFIGURATION</div>
          <h2>Secrets & variables</h2>
          <p>Keep credentials out of your source code.</p>
        </div>
        <Btn variant="primary" onClick={() => edit()}>
          <Icon name="plus" size={14} /> Add value
        </Btn>
      </div>
      <div className="info-banner">
        <Icon name="lock" />
        <div>
          <strong>Secrets are encrypted and write-only.</strong>
          <p>
            These values are stored by sandboxd. They are not automatically injected into the app’s
            environment. Access policies record the intended broker access.
          </p>
        </div>
      </div>
      {loading ? (
        <p role="status">Loading configuration…</p>
      ) : !items.length ? (
        <div className="empty-state">
          <Icon name="lock" size={30} />
          <h3>A safe place for app configuration</h3>
          <p>Add API keys as secrets and public configuration as variables.</p>
          <Btn onClick={() => edit()}>Add your first value</Btn>
        </div>
      ) : (
        <div className="resource-list">
          {items.map((item) => (
            <div className="resource-row" key={item.key}>
              <span className="resource-icon">
                <Icon name={item.sensitive ? 'lock' : 'code'} size={19} />
              </span>
              <div className="resource-main">
                <strong>{item.key}</strong>
                <span>{item.sensitive ? '•••••••• · Secret' : item.value || 'Empty variable'}</span>
                <small>{policies[item.access_policy]}</small>
              </div>
              <Btn sm onClick={() => edit(item)}>
                Edit
              </Btn>
              <button
                className="icon-button"
                aria-label={`Remove ${item.key}`}
                onClick={() => setRemoving(item)}
              >
                <Icon name="close" size={16} />
              </button>
            </div>
          ))}
        </div>
      )}
      {editing && (
        <Dialog
          title={editing === 'new' ? 'Add a secret or variable' : `Edit ${key}`}
          close={() => {
            if (!busy) {
              setEditing(null)
              setValue('')
            }
          }}
        >
          <form
            onSubmit={(e) => {
              e.preventDefault()
              void save()
            }}
            className="settings-form"
          >
            <label className="form-field">
              Name
              <Input
                autoFocus
                required
                value={key}
                disabled={editing !== 'new'}
                onChange={(e) => setKey(e.target.value)}
                placeholder="MY_API_KEY"
              />
            </label>
            {editing === 'new' && (
              <label className="form-field">
                Visibility
                <select
                  value={secret ? 'secret' : 'variable'}
                  onChange={(e) => setSecret(e.target.value === 'secret')}
                >
                  <option value="secret">Secret — encrypted, never shown again</option>
                  <option value="variable">Variable — visible to console users</option>
                </select>
              </label>
            )}
            {editing !== 'new' && (
              <label className="check-label">
                <input type="checkbox" checked={replace} onChange={(e) => setReplace(e.target.checked)} />{' '}
                Replace the saved value
              </label>
            )}
            {replace && (
              <label className="form-field">
                Value
                <Input
                  type={secret ? 'password' : 'text'}
                  autoComplete="new-password"
                  value={value}
                  onChange={(e) => setValue(e.target.value)}
                  placeholder={secret ? 'Enter the new secret' : 'Enter a value'}
                />
              </label>
            )}
            <label className="form-field">
              Access policy
              <select value={policy} onChange={(e) => setPolicy(e.target.value as AccessPolicy)}>
                {Object.entries(policies).map(([id, label]) => (
                  <option key={id} value={id}>
                    {label}
                  </option>
                ))}
              </select>
            </label>
            {error && (
              <p role="alert" className="inline-error">
                {error}
              </p>
            )}
            <div className="dialog-actions">
              <Btn
                disabled={busy}
                type="button"
                onClick={() => {
                  setEditing(null)
                  setValue('')
                }}
              >
                Cancel
              </Btn>
              <Btn disabled={busy} type="submit" variant="primary">
                {busy ? 'Saving…' : 'Save value'}
              </Btn>
            </div>
          </form>
        </Dialog>
      )}
      {removing && (
        <Dialog title={`Remove ${removing.key}?`} close={() => !busy && setRemoving(null)}>
          <p>This permanently removes the saved value. Any integration using it will need a replacement.</p>
          <div className="dialog-actions">
            <Btn disabled={busy} onClick={() => setRemoving(null)}>
              Cancel
            </Btn>
            <Btn variant="danger" disabled={busy} onClick={() => void remove()}>
              Remove value
            </Btn>
          </div>
        </Dialog>
      )}
    </section>
  )
}
