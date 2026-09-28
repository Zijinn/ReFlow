import { vi } from "vitest"

interface FakeQuery {
  matches: boolean
  listeners: Set<() => void>
}

export interface MediaQueryStub {
  /** Flips a query and fires the `change` listeners, like the OS would. */
  set(media: string, matches: boolean): void
}

// jsdom reports every media query as not matching and never fires `change`, so
// anything that tracks the OS appearance is untestable without this.
export function stubMediaQueries(initial: Record<string, boolean> = {}): MediaQueryStub {
  const queries = new Map<string, FakeQuery>()
  for (const [media, matches] of Object.entries(initial)) {
    queries.set(media, { matches, listeners: new Set() })
  }
  const matchMedia = (media: string) => {
    const query = queries.get(media) ?? { matches: false, listeners: new Set<() => void>() }
    return {
      media,
      get matches() {
        return query.matches
      },
      addEventListener: (_type: string, cb: () => void) => query.listeners.add(cb),
      removeEventListener: (_type: string, cb: () => void) => query.listeners.delete(cb),
    }
  }
  vi.stubGlobal("matchMedia", matchMedia)
  return {
    set(media, matches) {
      const query = queries.get(media)
      if (!query || query.matches === matches) return
      query.matches = matches
      for (const cb of [...query.listeners]) cb()
    },
  }
}
