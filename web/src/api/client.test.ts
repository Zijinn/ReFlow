import { afterEach, describe, expect, it, vi } from "vitest"

import {
  createResearchTag,
  deleteResearchTag,
  importOPML,
  listResearchTags,
  markEntriesRead,
  restoreBackup,
  updateEntryState,
  updateResearchTag,
} from "./client"

afterEach(() => {
  vi.restoreAllMocks()
  localStorage.clear()
})

describe("research tags", () => {
  const tag = { id: "t-1", name: "Fieldwork", position: 3 }

  it("lists the palette in the server-provided order", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(
      new Response(JSON.stringify({ tags: [tag] }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
    )

    await expect(listResearchTags()).resolves.toEqual({ tags: [tag] })
    expect(fetchMock.mock.calls[0]?.[0]).toBe("/api/v1/research/tags")
  })

  it("unwraps the tag envelope on create and sends the name", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(
      new Response(JSON.stringify({ tag }), {
        status: 201,
        headers: { "Content-Type": "application/json" },
      }),
    )

    await expect(createResearchTag("Fieldwork")).resolves.toEqual(tag)
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/v1/research/tags",
      expect.objectContaining({ method: "POST", body: JSON.stringify({ name: "Fieldwork" }) }),
    )
  })

  it("surfaces a duplicate name as an APIError", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(
      new Response(JSON.stringify({ title: "Conflict", detail: "duplicate tag" }), {
        status: 409,
        headers: { "Content-Type": "application/json" },
      }),
    )

    await expect(createResearchTag("Fieldwork")).rejects.toMatchObject({
      name: "APIError",
      status: 409,
    })
  })

  it("patches a rename through the tag envelope", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(
      new Response(JSON.stringify({ tag: { ...tag, name: "Placebo" } }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
    )

    await expect(updateResearchTag("t-1", "Placebo")).resolves.toEqual({
      ...tag,
      name: "Placebo",
    })
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/v1/research/tags/t-1",
      expect.objectContaining({ method: "PATCH", body: JSON.stringify({ name: "Placebo" }) }),
    )
  })

  it("deletes with a 204 and no body", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(
      new Response(null, { status: 204 }),
    )

    await expect(deleteResearchTag("t-1")).resolves.toBeUndefined()
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/v1/research/tags/t-1",
      expect.objectContaining({ method: "DELETE" }),
    )
  })
})

describe("updateEntryState", () => {
  it("uses the caller-provided mutation timestamp", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(
      new Response(
        JSON.stringify({
          is_read: true,
          is_starred: false,
          is_read_later: false,
          updated_at: "2026-09-11T00:00:00Z",
        }),
        { status: 200, headers: { "Content-Type": "application/json" } },
      ),
    )

    await updateEntryState("entry-1", { is_read: true }, "mutation-1", "2026-09-11T12:34:56.789Z")

    const body = JSON.parse(fetchMock.mock.calls[0]?.[1]?.body as string) as Record<string, unknown>
    expect(body).toEqual({
      mutation_id: "mutation-1",
      device_time: "2026-09-11T12:34:56.789Z",
      is_read: true,
    })
  })
})

describe("markEntriesRead", () => {
  it("passes the trimmed search query through to the backend filter", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(
      new Response(JSON.stringify({ updated: 3 }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
    )

    await markEntriesRead({ kind: "unread", title: "Unread" }, "  electron  ")

    expect(fetchMock).toHaveBeenCalledWith(
      "/api/v1/entries/mark-read",
      expect.objectContaining({
        body: JSON.stringify({ state: "unread", query: "electron" }),
      }),
    )
  })

  it("omits the query filter when the search box is empty", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(
      new Response(JSON.stringify({ updated: 1 }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
    )

    await markEntriesRead({ kind: "today", title: "Today" }, "   ")

    expect(fetchMock).toHaveBeenCalledOnce()
    const body = JSON.parse(fetchMock.mock.calls[0]?.[1]?.body as string) as Record<string, string>
    expect(body.query).toBeUndefined()
    expect(body.since).toBeDefined()
  })
})

describe("desktop file uploads", () => {
  it("sends OPML as text instead of a File body", async () => {
    const source = `<?xml version="1.0"?><opml version="2.0"><body><outline text="Feed" xmlUrl="https://example.com/feed.xml" /></body></opml>`
    const readText = vi.fn().mockResolvedValue(source)
    const file = { text: readText } as unknown as File
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(
      new Response(
        JSON.stringify({
          id: "job-1",
          kind: "opml.import",
          state: "queued",
          progress_current: 0,
          progress_total: 0,
        }),
        { status: 202, headers: { "Content-Type": "application/json" } },
      ),
    )

    await importOPML(file)

    expect(readText).toHaveBeenCalledOnce()
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/v1/imports/opml",
      expect.objectContaining({ body: source }),
    )
  })

  it("sends a backup as text instead of a File body", async () => {
    const source = JSON.stringify({ format: "reflow-backup" })
    const readText = vi.fn().mockResolvedValue(source)
    const file = { text: readText } as unknown as File
    const fetchMock = vi
      .spyOn(globalThis, "fetch")
      .mockResolvedValue(new Response(null, { status: 204 }))

    await restoreBackup(file)

    expect(readText).toHaveBeenCalledOnce()
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/v1/restore",
      expect.objectContaining({ body: source }),
    )
  })
})
