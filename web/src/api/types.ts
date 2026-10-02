export interface ServerStatus {
  status: "ready" | "degraded"
  version: string
  api_version: string
  database_ready: boolean
  capabilities: string[]
  device_auth_required: boolean
  device_authenticated: boolean
}

export interface Device {
  id: string
  name: string
  platform: "web" | "ipad" | "windows" | "macos" | "ios" | "android"
  last_seen_at: string | null
  created_at: string
  revoked_at: string | null
}

export type SyncProviderID =
  | "freshrss"
  | "google_reader"
  | "miniflux"
  | "fever"
  | "feedbin"
  | "nextcloud_news"
  | "webdav"
  | "icloud"

export interface SyncProvider {
  id: SyncProviderID
  name: string
}

export interface SyncCredentials {
  username?: string
  password?: string
  api_key?: string
  token?: string
}

export interface SyncAccount {
  id: string
  provider: SyncProviderID
  name: string
  endpoint: string
  enabled: boolean
  allow_private_network: boolean
  sync_interval_minutes: number
  last_sync_at: string | null
  next_sync_at: string | null
  last_attempt_at: string | null
  last_error_code: string | null
  last_error_message: string | null
  created_at: string
  updated_at: string
}

export type AIProviderID = "openai_compatible" | "ollama"

export interface AIProvider {
  id: AIProviderID
  name: string
}

export interface AIProfile {
  id: string
  provider: AIProviderID
  name: string
  endpoint: string
  model: string
  enabled: boolean
  allow_private_network: boolean
  remote_content_approved: boolean
  is_default: boolean
  last_used_at: string | null
  last_error_code: string | null
  last_error_message: string | null
  created_at: string
  updated_at: string
}

export interface AIUsage {
  input_tokens: number
  output_tokens: number
  total_tokens: number
}

export type AIOperation =
  "summary" | "title_translation" | "translation" | "key_points" | "academic_tags"

export interface AIResult {
  id: string
  ai_profile_id: string | null
  entry_id: string
  operation: AIOperation
  language: string
  input_hash: string
  result_text: string
  usage: AIUsage
  created_at: string
}

/**
 * Both paper-context AI endpoints (`/ai/paper-chat`, `/ai/daily-digest`) answer
 * 202 with the same envelope the chat already uses: a job to poll and the
 * session the assistant message lands in.
 */
export interface AIResearchStartResponse {
  job: Job
  session: AIChatSession
}

export interface AIChatMessage {
  id: string
  role: "user" | "assistant"
  content: string
  /** Only an assistant row carries these (`internal/storage/ai.go`
   *  SaveAIChatAssistantAndUsage); every other row is stored as `{}`. */
  metadata?: { provider?: string; model?: string }
  status: "pending" | "streaming" | "completed" | "failed"
  usage: Partial<AIUsage>
  created_at: string
}

export interface AIChatSession {
  id: string
  ai_profile_id: string | null
  entry_id: string | null
  title: string
  messages: AIChatMessage[]
  created_at: string
  updated_at: string
}

export interface ZoteroStatus {
  available: boolean
  editable: boolean
  library_id?: string
  library_name?: string
  collection_id?: string
  collection_name?: string
  error_message?: string
}

export interface ZoteroExport {
  entry_id: string
  zotero_item_key?: string
  library_id?: string
  library_name?: string
  collection_id?: string
  collection_name?: string
  metadata_fingerprint: string
  exported_at: string
  updated_at: string
}

export interface EntryZoteroStatus {
  saved: boolean
  export?: ZoteroExport
}

export interface ZoteroSaveResult {
  saved: boolean
  duplicate: boolean
  doi?: string
  target: ZoteroStatus
  export: ZoteroExport
}

export type ViewMode = "compact" | "standard" | "card" | "magazine" | "image"

export interface Feed {
  id: string
  url: string
  canonical_url: string
  site_url: string | null
  title: string
  description: string | null
  icon_url: string | null
  format: "rss" | "atom" | "json" | null
  content_kind: "general" | "literature" | "video" | "social"
  last_checked_at: string | null
  last_success_at: string | null
  next_check_at: string | null
  failure_count: number
  last_error_code: string | null
  last_error_message: string | null
  created_at: string
  updated_at: string
}

export interface Subscription {
  id: string
  feed_id: string
  folder_id: string | null
  position: number
  title: string
  icon_url: string | null
  feed_url: string
  site_url: string | null
  unread_count: number
  failure_count: number
  last_error_code: string | null
  last_error_message: string | null
  last_success_at: string | null
  view_mode: ViewMode
  refresh_policy: "inherit" | "fixed" | "intelligent" | "never"
  refresh_interval_minutes: number
  hide_from_timeline: boolean
  created_at: string
  updated_at: string
}

export interface Folder {
  id: string
  parent_id: string | null
  name: string
  position: number
  created_at: string
  updated_at: string
}

export interface Tag {
  id: string
  name: string
  color: string | null
  position: number
  created_at: string
}

export interface Rule {
  id: string
  name: string
  enabled: boolean
  priority: number
  conditions: Record<string, unknown>
  actions: Record<string, unknown>
  created_at: string
  updated_at: string
}

export interface SavedFilter {
  id: string
  name: string
  query: Record<string, unknown>
  position: number
  created_at: string
  updated_at: string
}

export interface EntryState {
  is_read: boolean
  is_starred: boolean
  is_read_later: boolean
  updated_at: string
}

export interface Entry {
  id: string
  feed_id: string
  feed_title: string
  canonical_url: string | null
  title: string
  author: string | null
  summary: string | null
  published_at: string
  discovered_at: string
  lead_image_url: string | null
  audio_url?: string | null
  video_url?: string | null
  language?: string | null
  doi?: string | null
  ai_translated_title?: string | null
  ai_summary?: string | null
  tag_ids: string[]
  state: EntryState
}

export interface EntryDetail extends Entry {
  sanitized_html: string
  readability_html: string | null
}

export interface EntryPage {
  items: Entry[]
  next_cursor: string | null
}

export interface Job {
  id: string
  kind: string
  state: "queued" | "running" | "succeeded" | "failed" | "cancelled"
  progress_current: number
  progress_total: number
  scheduled_at: string
  started_at: string | null
  finished_at: string | null
  error_code: string | null
  error_message: string | null
  created_at: string
  updated_at: string
}

export interface ListResponse<T> {
  items: T[]
}

export type ResearchKind = "research" | "submitted" | "published"

export interface ResearchStage {
  name: string
  done: boolean
  children: ResearchStage[]
}

export interface SubmissionRecord {
  journal: string
  date: string
  status: string
}

/**
 * Research-workbench tag palette entry. Distinct from the library `Tag`:
 * these live under `/research/tags`; a paper wears several of them at once.
 * The list endpoint returns them ordered by `position` ascending, which is the
 * user's own priority order (drag/edit happens in the preferences "tags" pane).
 */
export interface ResearchTag {
  id: string
  name: string
  position: number
}

export interface ResearchPaper {
  id: string
  kind: ResearchKind
  position: number
  title: string
  authors: string[]
  keywords: string[]
  file_path: string
  next_action: string
  notes: string
  research_area: string
  status: string
  // 指派顺序（不是调色板顺序），无标签恒为 []。
  tag_ids: string[]
  target_journal: string
  stages: ResearchStage[]
  current_journal: string
  submission_date: string
  manuscript_id: string
  submission_count: number
  target_level: string
  editor: string
  deadline: string
  history: SubmissionRecord[]
  abstract: string
  journal: string
  language: string
  year: string
  volume: string
  issue: string
  pages: string
  doi: string
  citations: number | null
  citation_source: string
  citation_updated_at: string
  last_updated: string
  created_at: string
  updated_at: string
}

export interface ResearchPaperPatch {
  title?: string
  authors?: string[]
  keywords?: string[]
  file_path?: string
  next_action?: string
  notes?: string
  research_area?: string
  status?: string
  // 缺省＝不碰标签，[]＝清空；未知 id 服务端照收，前端只负责把它们渲染成占位。
  tag_ids?: string[]
  target_journal?: string
  stages?: ResearchStage[]
  current_journal?: string
  submission_date?: string
  manuscript_id?: string
  submission_count?: number
  target_level?: string
  editor?: string
  deadline?: string
  history?: SubmissionRecord[]
  abstract?: string
  journal?: string
  language?: string
  year?: string
  volume?: string
  issue?: string
  pages?: string
  doi?: string
  citations?: number | null
  citation_source?: string
}

export type LibraryScope =
  | { kind: "today"; title: "Today" }
  | { kind: "unread"; title: "Unread" }
  | { kind: "literature"; title: "Literature" }
  | { kind: "saved"; title: "Saved" }
  | { kind: "all"; title: "All feeds" }
  | { kind: "feed"; id: string; title: string }
  | { kind: "folder"; id: string; title: string }
  | { kind: "tag"; id: string; title: string }
  | { kind: "filter"; id: string; title: string; query: Record<string, unknown> }
