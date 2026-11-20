import { useState } from 'react'
import { api, AgentInstance, App as TApp } from './api'
import { catalogIdOf } from './catalog'
import { Btn, Input } from './design/kit'
import { Dialog } from './design/Dialog'
import { Bot } from './design/Bot'
import { Avatar, logoFor, setAsBot, useAsBot } from './design/Avatar'
import { APP_COLORS, AppTile, IconChoice, ProjectIcon, defaultColor, iconChoice, withIcon } from './design/ProjectIcon'

// Rename a project or an agent and choose its icon: its own logo, a colour
// (apps) or a Boxbot. Opened from the sidebar (pencil) or the workspace title.
export function EditLook(
  props: { onClose: () => void; onError: (m: string) => void } & (
    | { app: TApp; onSaved: (app: TApp) => void; agent?: undefined }
    | { agent: AgentInstance; onSaved: (agent: AgentInstance) => void; app?: undefined }
  ),
) {
  const { onClose, onError } = props
  const isApp = !!props.app
  const logo = props.app ? catalogIdOf(props.app.tags) : props.agent!.package
  const hasLogo = !!logoFor(logo)
  const agentAsBot = useAsBot((logo || '').toLowerCase())
  const [name, setName] = useState(props.app ? props.app.name : props.agent!.name)
  const [choice, setChoice] = useState<IconChoice>(() =>
    props.app
      ? iconChoice(props.app.tags, logo)
      : { kind: hasLogo && !agentAsBot ? 'logo' : 'bot' },
  )
  const [busy, setBusy] = useState(false)
  const shown = name.trim() || (props.app ? props.app.name : props.agent!.name)
  const save = async () => {
    setBusy(true)
    try {
      if (props.app) {
        const tags = withIcon(props.app.tags, choice)
        props.onSaved(await api.updateApp(props.app.id, { name: name.trim(), tags }))
      } else {
        const agent = props.agent!
        if (hasLogo) setAsBot(agent.package.toLowerCase(), choice.kind === 'bot')
        props.onSaved(name.trim() !== agent.name ? await api.renameAgent(agent.id, name.trim()) : agent)
      }
      onClose()
    } catch (e) {
      onError((e as Error).message)
    } finally {
      setBusy(false)
    }
  }
  const preview =
    choice.kind === 'bot' ? (
      <Bot who={props.app ? shown : props.agent!.package} size={84} mood="happy" />
    ) : choice.kind === 'logo' ? (
      <Avatar who={shown} logo={logo} size={84} />
    ) : (
      <AppTile name={shown} color={choice.color} size={84} />
    )
  const pick = (c: IconChoice, label: string, face: React.ReactNode) => {
    const on =
      c.kind === choice.kind && (c.kind !== 'color' || (choice.kind === 'color' && (choice.color || '') === c.color))
    return (
      <button
        type="button"
        key={label}
        className={on ? 'look-option on' : 'look-option'}
        aria-pressed={on}
        aria-label={label}
        title={label}
        onClick={() => setChoice(c)}
      >
        {face}
      </button>
    )
  }
  return (
    <Dialog title={isApp ? 'Rename & icon' : 'Rename agent & icon'} close={() => !busy && onClose()}>
      <form
        className="edit-look"
        onSubmit={(e) => {
          e.preventDefault()
          if (name.trim()) void save()
        }}
      >
        <div className="edit-look-preview">{preview}</div>
        <label className="form-field">
          Name
          <Input
            autoFocus
            required
            maxLength={isApp ? 80 : 60}
            value={name}
            onChange={(e) => setName(e.target.value)}
            data-testid="look-name"
          />
        </label>
        <div className="form-field">
          Icon
          <div className="look-options" data-testid="look-options">
            {hasLogo && pick({ kind: 'logo' }, 'Its own logo', <Avatar who={shown} logo={logo} size={34} />)}
            {isApp &&
              APP_COLORS.map((color) =>
                pick(
                  { kind: 'color', color: color === defaultColor(props.app!.name) && !hasLogo ? '' : color },
                  `Colour ${color}`,
                  <AppTile name={shown} color={color} size={34} />,
                ),
              )}
            {pick({ kind: 'bot' }, 'A sandboxd bot', <Bot who={props.app ? shown : props.agent!.package} size={34} still />)}
          </div>
        </div>
        <div className="dialog-actions">
          <Btn type="button" onClick={onClose} disabled={busy}>
            Cancel
          </Btn>
          <Btn type="submit" variant="primary" disabled={busy || !name.trim()} data-testid="look-save">
            {busy ? 'Saving…' : 'Save'}
          </Btn>
        </div>
      </form>
    </Dialog>
  )
}

