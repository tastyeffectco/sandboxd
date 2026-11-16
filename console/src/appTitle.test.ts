import { describe, expect, it } from 'vitest'
import { appTitle } from './appTitle'

describe('appTitle', () => {
  it.each([
    ['A simple shopping list app: add items, tick them off, clear done items.', 'Shopping List App'],
    ['Build me a recipe app with a shopping list', 'Recipe App'],
    ['I want to make a habit tracker that shows streaks', 'Habit Tracker'],
    ['Can you build a landing page for my bakery', 'Landing Page'],
    ['expense splitter for friends', 'Expense Splitter'],
    ['A dashboard to watch my GitHub PRs', 'Dashboard'],
    ['Build a portfolio', 'Portfolio'],
    ['A Next.js blog', 'Next.js Blog'],
    ['todo app', 'Todo App'],
    ['   ', 'Untitled app'],
  ])('%s → %s', (prompt, title) => {
    expect(appTitle(prompt)).toBe(title)
  })

  it('keeps names short and keeps words people capitalised', () => {
    expect(appTitle('An AI-powered invoice generator for freelancers').split(' ').length).toBeLessThanOrEqual(5)
    expect(appTitle('My iPhone wallpaper picker')).toBe('iPhone Wallpaper Picker')
    expect(appTitle('An AI-powered invoice generator for freelancers')).toBe('AI-powered Invoice Generator')
  })
})
