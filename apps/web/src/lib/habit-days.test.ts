import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { activePreset, clearDraft, draftKey, loadDraft, sameDraft, saveDraft } from "@/lib/habit-days"

describe("activePreset", () => {
  it("recognises each preset, in any order", () => {
    expect(activePreset([])).toBe("every")
    expect(activePreset(["0", "1", "2", "3", "4", "5", "6"])).toBe("every")
    expect(activePreset(["5", "3", "1", "2", "4"])).toBe("weekdays")
    expect(activePreset(["6", "0"])).toBe("weekends")
  })

  it("returns nothing for a custom pick", () => {
    expect(activePreset(["1", "3"])).toBeUndefined()
  })
})

describe("sameDraft", () => {
  const base = { name: "Read", source: "manual" as const, days: ["1", "2"] }
  it("ignores the order of the days", () => {
    expect(sameDraft(base, { ...base, days: ["2", "1"] })).toBe(true)
  })
  it("notices a different name, source or days", () => {
    expect(sameDraft(base, { ...base, name: "Run" })).toBe(false)
    expect(sameDraft(base, { ...base, source: "github" })).toBe(false)
    expect(sameDraft(base, { ...base, days: ["1"] })).toBe(false)
  })
})

describe("drafts", () => {
  const store = new Map<string, string>()
  beforeEach(() => {
    store.clear()
    vi.stubGlobal("localStorage", {
      getItem: (k: string) => store.get(k) ?? null,
      setItem: (k: string, v: string) => void store.set(k, v),
      removeItem: (k: string) => void store.delete(k),
    })
  })
  afterEach(() => vi.unstubAllGlobals())

  it("keys new and edited habits apart", () => {
    expect(draftKey()).not.toBe(draftKey({ id: 3 }))
    expect(draftKey({ id: 3 })).not.toBe(draftKey({ id: 4 }))
  })

  it("saves, loads and clears a draft", () => {
    const key = draftKey()
    const draft = { name: "Read 20 pages", source: "manual" as const, days: ["1", "2"] }
    expect(loadDraft(key)).toBeNull()
    saveDraft(key, draft)
    expect(loadDraft(key)).toEqual(draft)
    clearDraft(key)
    expect(loadDraft(key)).toBeNull()
  })

  it("ignores saved text that isn't a draft", () => {
    for (const bad of ["not json", "42", '{"name":1}', '{"name":"a","source":"x","days":[]}', '{"name":"a","source":"manual","days":[1]}']) {
      store.set("k", bad)
      expect(loadDraft("k"), bad).toBeNull()
    }
  })

  it("survives storage that throws", () => {
    vi.stubGlobal("localStorage", {
      getItem: () => {
        throw new Error("blocked")
      },
      setItem: () => {
        throw new Error("blocked")
      },
      removeItem: () => {
        throw new Error("blocked")
      },
    })
    expect(loadDraft("k")).toBeNull()
    expect(() => saveDraft("k", { name: "a", source: "manual", days: [] })).not.toThrow()
    expect(() => clearDraft("k")).not.toThrow()
  })
})
