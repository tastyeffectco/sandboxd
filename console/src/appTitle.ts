// A short project name from the idea someone typed, when they didn't name it:
// "A simple shopping list app: add items, tick them off" → "Shopping List App".

const LEAD =
  /^(?:(?:please|hey|hi|ok|okay)[,!\s]+)*(?:(?:can|could|would) you\s+)?(?:(?:i\s+(?:want|need|would like)|i'd like|let's|lets)\s+(?:to\s+)?)?(?:(?:build|create|make|write|generate|design|code|develop|set up|setup)\s+(?:me\s+|us\s+)?)?/i
const FILLER = new Set(['a', 'an', 'the', 'my', 'our', 'some', 'simple', 'small', 'little', 'basic', 'quick', 'tiny', 'new', 'nice', 'beautiful', 'modern', 'clean', 'minimal'])
// Where the name ends and the details begin.
const DETAILS = /[:,;!?()–—]|\.(?=\s|$)|\s-\s|\s(?:that|which|where|so|to|with|for|using|in|on|who|and then)\s/i

export function appTitle(prompt: string): string {
  let text = prompt.trim().replace(/\s+/g, ' ').replace(LEAD, '')
  const cut = text.search(DETAILS)
  if (cut > 0) text = text.slice(0, cut)
  const words = text.split(' ').filter(Boolean)
  while (words.length && FILLER.has(words[0].toLowerCase())) words.shift()
  const title = words
    .slice(0, 5)
    .map((w) => (/[A-Z]/.test(w.slice(1)) ? w : w[0].toUpperCase() + w.slice(1)))
    .join(' ')
  return title || 'Untitled app'
}
