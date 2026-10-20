import { useState } from 'react'
import { Dialog } from './design/Dialog'
import { Icon } from './design/Icon'
import { Btn, Input } from './design/kit'

export function DeployModal({
  appName,
  previewURL,
  close,
}: {
  appName?: string
  previewURL?: string
  close: () => void
}) {
  const [copied, setCopied] = useState(false)
  const [error, setError] = useState('')
  return (
    <Dialog title={`Share ${appName || 'your app'}`} close={close}>
      <div data-testid="deploy-modal" className="settings-form">
        <div className="info-banner">
          <Icon name="monitor" />
          <div>
            <strong>Share the live preview</strong>
            <p>
              Visitors need network access to your instance. Preview availability follows your workspace’s
              lifecycle.
            </p>
          </div>
        </div>
        {previewURL ? (
          <>
            <label className="form-field">
              Preview URL
              <Input readOnly value={previewURL} onFocus={(e) => e.target.select()} />
            </label>
            <div className="dialog-actions">
              <a href={previewURL} target="_blank" rel="noreferrer" className="text-button">
                Open preview <Icon name="external" size={14} />
              </a>
              <Btn
                variant="primary"
                onClick={async () => {
                  try {
                    await navigator.clipboard.writeText(previewURL)
                    setCopied(true)
                  } catch {
                    setError('Copy is unavailable. Select and copy the URL above.')
                  }
                }}
              >
                {copied ? 'Copied' : 'Copy link'}
              </Btn>
            </div>
          </>
        ) : (
          <p>Start your workspace and its web server to get a preview URL.</p>
        )}
        {error && <p role="alert">{error}</p>}
        <hr />
        <h3>Ready for production?</h3>
        <p>
          Use Git to push your project to your own repository, then deploy with your hosting provider.
          One-click production deployment is not available in this console.
        </p>
        <a
          className="text-button"
          href="https://sandboxd.io/guides/production-tls"
          target="_blank"
          rel="noreferrer"
        >
          Configure a public domain for your instance <Icon name="external" size={14} />
        </a>
      </div>
    </Dialog>
  )
}
