// Picks a ready-made stack from the user's idea when "Choose stack" is left on
// Automatic, and explains the choice in plain words. Frameworks without a
// ready-made stack still start from the closest one (the agent sets them up);
// languages the workspace does not have are called out before anything is built.

export type StackId = 'react-vite' | 'nextjs' | 'node-express' | 'fastapi' | 'worker'

export interface StackGuess {
  preset: StackId
  // Shown under the composer; empty when the idea mentions no stack.
  note: string
  // warn: the idea asks for something the user will not get exactly.
  tone: 'info' | 'warn'
}

export const STACK_LABELS: Record<StackId, string> = {
  'react-vite': 'React / Vite',
  nextjs: 'Next.js',
  'node-express': 'Node / Express API',
  fastapi: 'Python / FastAPI',
  worker: 'Background worker',
}

const has = (text: string, pattern: RegExp) => pattern.test(text)

// JavaScript frameworks the agent sets up from the React / Vite starter.
const JS_FRAMEWORKS: [RegExp, string][] = [
  [/\bsvelte ?kit\b/, 'SvelteKit'],
  [/\bsvelte\b/, 'Svelte'],
  [/\bnuxt\b/, 'Nuxt'],
  [/\bvue(\.?js)?\b/, 'Vue'],
  [/\bastro\b/, 'Astro'],
  [/\bangular\b/, 'Angular'],
  [/\bsolid(\.?js| ?start)\b/, 'Solid'],
  [/\bpreact\b/, 'Preact'],
  [/\bremix\b/, 'Remix'],
  [/\bgatsby\b/, 'Gatsby'],
  [/\bqwik\b/, 'Qwik'],
  [/\beleventy\b|\b11ty\b/, 'Eleventy'],
]

// Python frameworks the agent sets up from the FastAPI starter.
const PY_FRAMEWORKS: [RegExp, string][] = [
  [/\bdjango\b/, 'Django'],
  [/\bflask\b/, 'Flask'],
  [/\bstreamlit\b/, 'Streamlit'],
  [/\bgradio\b/, 'Gradio'],
]

// Languages the workspace does not include.
const UNAVAILABLE: [RegExp, string][] = [
  [/\blaravel\b|\bphp\b|\bwordpress\b|\bsymfony\b/, 'PHP'],
  [/\bruby on rails\b|\brails\b|\bruby\b/, 'Ruby'],
  [/\bspring ?boot\b|\bjava\b(?!\s*script)/, 'Java'],
  [/\.net\b|\basp\.net\b|\bc#|\bblazor\b/, '.NET'],
  [/\bgolang\b|\bgo (api|server|backend|app)\b/, 'Go'],
  [/\brust\b|\bactix\b|\baxum\b/, 'Rust'],
  [/\bflutter\b|\bswift ?ui\b|\bkotlin\b|\breact native\b|\bios app\b|\bandroid app\b/, 'Native mobile'],
]

const UI_WORDS =
  /\b(page|pages|website|site|ui|frontend|front-end|dashboard|landing|interface|screen|form|portfolio|blog|shop|store)\b/

export function stackFromIdea(idea: string): StackGuess {
  const text = ` ${idea.toLowerCase()} `
  const ready = (preset: StackId, note = ''): StackGuess => ({ preset, note, tone: 'info' })
  const because = (preset: StackId, what: string) =>
    ready(preset, `Starting with ${STACK_LABELS[preset]} because your idea mentions ${what}.`)

  for (const [pattern, name] of UNAVAILABLE)
    if (has(text, pattern)) {
      const preset: StackId = has(text, UI_WORDS) || !has(text, /\bapi\b/) ? 'react-vite' : 'node-express'
      return {
        preset,
        note: `${name} isn't available in your workspace yet, so your app will be built with ${STACK_LABELS[preset]} instead. You can change this in Choose stack.`,
        tone: 'warn',
      }
    }
  if (has(text, /\bnext(\.| )?js\b/)) return because('nextjs', 'Next.js')
  for (const [pattern, name] of JS_FRAMEWORKS)
    if (has(text, pattern))
      return {
        preset: 'react-vite',
        note: `${name} isn't a ready-made stack yet. Your agent will set it up from the React / Vite starter, which can take a little longer.`,
        tone: 'warn',
      }
  if (has(text, /\bfastapi\b/)) return because('fastapi', 'FastAPI')
  for (const [pattern, name] of PY_FRAMEWORKS)
    if (has(text, pattern))
      return {
        preset: 'fastapi',
        note: `${name} isn't a ready-made stack yet. Your agent will set it up in a Python workspace, which can take a little longer.`,
        tone: 'warn',
      }
  if (has(text, /\bpython\b/)) return because('fastapi', 'Python')
  if (has(text, /\bexpress\.?js\b|\bexpress (api|server|backend|app)\b|\b(with|in|using|on) express\b/))
    return because('node-express', 'Express')
  if (
    has(text, /\b(background worker|cron job|scheduled (job|task)|runs? every \d+ ?(seconds?|minutes?|hours?))\b/) &&
    !has(text, UI_WORDS)
  )
    return {
      preset: 'worker',
      note: 'Starting with a background worker because your idea runs on its own. It has no preview page; you follow it in the logs.',
      tone: 'info',
    }
  if (has(text, /\b(rest(ful)? api|json api|api server|backend api|node(\.?js)? (api|server|backend))\b/) && !has(text, UI_WORDS))
    return because('node-express', 'an API')
  if (has(text, /\breact\b/)) return because('react-vite', 'React')
  return ready('react-vite')
}
