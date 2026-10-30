import { describe, expect, it } from 'vitest'
import { stackFromIdea } from './stackFromIdea'

describe('stackFromIdea', () => {
  const cases: [string, string, 'info' | 'warn', RegExp | ''][] = [
    ['A habit tracker with streaks', 'react-vite', 'info', ''],
    ['A React dashboard for my team', 'react-vite', 'info', /because your idea mentions React/],
    ['A blog with Next.js and two pages', 'nextjs', 'info', /Next\.js because/],
    ['build a nextjs landing page', 'nextjs', 'info', /Next\.js/],
    ['A REST API for notes with GET and POST', 'node-express', 'info', /an API/],
    ['An Express server that returns quotes', 'node-express', 'info', /Express/],
    ['A FastAPI service for todos', 'fastapi', 'info', /FastAPI/],
    ['A small Python web app that shows the weather', 'fastapi', 'info', /Python/],
    ['A Flask app with a contact form', 'fastapi', 'warn', /Flask isn't a ready-made stack/],
    ['A Django admin for my books', 'fastapi', 'warn', /Django/],
    ['A Vue todo app', 'react-vite', 'warn', /Vue isn't a ready-made stack/],
    ['A SvelteKit portfolio', 'react-vite', 'warn', /SvelteKit/],
    ['An Astro blog', 'react-vite', 'warn', /Astro/],
    ['A background worker that runs every 10 minutes and logs prices', 'worker', 'info', /no preview page/],
    ['A Laravel blog', 'react-vite', 'warn', /PHP isn't available/],
    ['A Ruby on Rails shop', 'react-vite', 'warn', /Ruby/],
    ['A Go API server for links', 'node-express', 'warn', /Go isn't available/],
    ['An iOS app for my gym', 'react-vite', 'warn', /Native mobile/],
  ]
  it.each(cases)('%s → %s', (idea, preset, tone, note) => {
    const guess = stackFromIdea(idea)
    expect(guess.preset).toBe(preset)
    expect(guess.tone).toBe(tone)
    if (note) expect(guess.note).toMatch(note)
    else expect(guess.note).toBe('')
  })

  it('does not mistake everyday words for stacks', () => {
    for (const idea of [
      'A page with the next step for each task',
      'A JavaScript quiz',
      'Express yourself: a journaling app',
      'A dashboard with a REST API behind it',
      'An app that goes to the next screen',
    ]) {
      const guess = stackFromIdea(idea)
      expect(guess.preset, idea).toBe('react-vite')
      expect(guess.tone, idea).toBe('info')
    }
  })
})
