import * as Dialog from "@radix-ui/react-dialog"
import {
  AppleLogo,
  CheckCircle,
  CircleNotch,
  CloudArrowDown,
  CloudArrowUp,
  PlugsConnected,
  X,
} from "@phosphor-icons/react"
import { type FormEvent, useMemo, useRef, useState } from "react"

import {
  APIError,
  type CreateSyncAccountInput,
  type SyncConnectionTestResult,
  type TestSyncConnectionInput,
} from "../api/client"
import type { SyncAccount, SyncCredentials, SyncProvider, SyncProviderID } from "../api/types"
import { useTranslation } from "../lib/i18n"

interface SyncAccountDialogProps {
  open: boolean
  providers: SyncProvider[]
  account?: SyncAccount
  initialProvider?: SyncProviderID
  pending: boolean
  error: Error | null
  onOpenChange: (open: boolean) => void
  onSave: (input: CreateSyncAccountInput) => void
  onTest: (input: TestSyncConnectionInput) => Promise<SyncConnectionTestResult>
}

const defaultEndpoints: Partial<Record<SyncProviderID, string>> = {
  feedbin: "https://api.feedbin.com",
}

type ConnectionState =
  | { status: "idle" }
  | { status: "pending" }
  | { status: "success"; endpoint: string }
  | { status: "error"; message: string }

export function SyncAccountDialog(props: SyncAccountDialogProps) {
  const { t } = useTranslation()
  const editing = props.account !== undefined
  const fallbackProvider =
    props.account?.provider ?? props.initialProvider ?? props.providers[0]?.id ?? "webdav"
  const [provider, setProvider] = useState<SyncProviderID>(fallbackProvider)
  const [name, setName] = useState(props.account?.name ?? "")
  const [endpoint, setEndpoint] = useState(
    props.account?.endpoint ?? defaultEndpoints[fallbackProvider] ?? "",
  )
  const [username, setUsername] = useState("")
  const [password, setPassword] = useState("")
  const [token, setToken] = useState("")
  const [apiKey, setAPIKey] = useState("")
  const [interval, setInterval] = useState(props.account?.sync_interval_minutes ?? 30)
  const [allowPrivate, setAllowPrivate] = useState(props.account?.allow_private_network ?? false)
  const [connection, setConnection] = useState<ConnectionState>({ status: "idle" })
  const testGeneration = useRef(0)
  const invalidateConnectionTest = () => {
    testGeneration.current++
    setConnection({ status: "idle" })
  }
  const providerName = useMemo(
    () => props.providers.find((item) => item.id === provider)?.name ?? provider,
    [props.providers, provider],
  )
  const usesAPIKey = provider === "fever"
  const isWebDAV = provider === "webdav"
  const isICloud = provider === "icloud"
  const isLibraryCloud = isWebDAV || isICloud
  const usesBasicAuth = provider === "feedbin" || provider === "nextcloud_news" || isWebDAV

  const changeProvider = (value: SyncProviderID) => {
    setProvider(value)
    setEndpoint(defaultEndpoints[value] ?? "")
    setName("")
    setUsername("")
    setPassword("")
    setToken("")
    setAPIKey("")
    invalidateConnectionTest()
  }
  const useNutstore = () => {
    setName(t("nutstore"))
    setEndpoint("https://dav.jianguoyun.com/dav/ReFlow/")
    invalidateConnectionTest()
  }
  const credentials = (): SyncCredentials => ({
    username: username.trim() || undefined,
    password: password || undefined,
    token: token.trim() || undefined,
    api_key: apiKey.trim() || undefined,
  })
  const credentialsReady =
    editing || isLibraryCloud
      ? true
      : usesAPIKey
        ? apiKey.trim() !== ""
        : usesBasicAuth
          ? username.trim() !== ""
          : token.trim() !== "" || username.trim() !== ""
  const endpointReady = isICloud || endpoint.trim() !== ""
  const submit = (event: FormEvent) => {
    event.preventDefault()
    if (!endpointReady || !credentialsReady) return
    props.onSave({
      provider,
      name: name.trim() || providerName,
      endpoint: endpoint.trim(),
      credentials: credentials(),
      allow_private_network: allowPrivate,
      sync_interval_minutes: interval,
    })
  }
  const testConnection = async () => {
    if (!isWebDAV || !endpointReady || connection.status === "pending") return
    const generation = ++testGeneration.current
    setConnection({ status: "pending" })
    try {
      const result = await props.onTest({
        account_id: props.account?.id,
        provider,
        endpoint: endpoint.trim(),
        credentials: credentials(),
        allow_private_network: allowPrivate,
      })
      if (testGeneration.current !== generation) return
      setConnection({ status: "success", endpoint: result.endpoint })
    } catch (error) {
      if (testGeneration.current !== generation) return
      const message =
        error instanceof APIError && error.code === "authentication_error"
          ? t("webdavAuthenticationFailed")
          : error instanceof Error
            ? error.message
            : t("connectionTestFailed")
      setConnection({ status: "error", message })
    }
  }

  return (
    <Dialog.Root open={props.open} onOpenChange={props.onOpenChange}>
      <Dialog.Portal>
        <Dialog.Overlay className="dialog-overlay" />
        <Dialog.Content className="dialog-content sync-account-dialog" aria-describedby={undefined}>
          <div className="dialog-header">
            <Dialog.Title>{editing ? t("editSyncAccount") : t("addSyncAccount")}</Dialog.Title>
            <Dialog.Close asChild>
              <button
                className="icon-button"
                type="button"
                aria-label={t("close")}
                title={t("close")}
              >
                <X />
              </button>
            </Dialog.Close>
          </div>
          <form className="dialog-form sync-dialog-form" onSubmit={submit}>
            <label className="field-label" htmlFor="sync-provider">
              {t("provider")}
            </label>
            <select
              id="sync-provider"
              className="select-input"
              value={provider}
              disabled={editing}
              onChange={(event) => changeProvider(event.target.value as SyncProviderID)}
            >
              <optgroup label={t("libraryCloudSync")}>
                {props.providers
                  .filter((item) => item.id === "webdav" || item.id === "icloud")
                  .map((item) => (
                    <option value={item.id} key={item.id}>
                      {item.name}
                    </option>
                  ))}
              </optgroup>
              <optgroup label={t("readingServiceSync")}>
                {props.providers
                  .filter((item) => item.id !== "webdav" && item.id !== "icloud")
                  .map((item) => (
                    <option value={item.id} key={item.id}>
                      {item.name}
                    </option>
                  ))}
              </optgroup>
            </select>
            {isLibraryCloud && (
              <div className="sync-provider-note">
                {isICloud ? <AppleLogo /> : <CloudArrowUp />}
                <span>
                  <strong>{isICloud ? t("icloudDrive") : "WebDAV"}</strong>
                  <small>
                    {isICloud ? t("icloudSyncDescription") : t("webdavSyncDescription")}
                  </small>
                </span>
                {isWebDAV && (
                  <button
                    className="button button--secondary sync-provider-note__action"
                    type="button"
                    onClick={useNutstore}
                  >
                    {t("useNutstore")}
                  </button>
                )}
              </div>
            )}
            <label className="field-label" htmlFor="sync-name">
              {t("accountName")}
            </label>
            <input
              id="sync-name"
              className="text-input"
              value={name}
              placeholder={providerName}
              maxLength={120}
              onChange={(event) => setName(event.target.value)}
            />
            <label className="field-label" htmlFor="sync-endpoint">
              {isICloud ? t("icloudPath") : isWebDAV ? t("snapshotFileURL") : t("serverURL")}
            </label>
            <input
              id="sync-endpoint"
              className="text-input"
              type={isICloud ? "text" : "url"}
              inputMode={isICloud ? undefined : "url"}
              autoComplete={isICloud ? "off" : "url"}
              placeholder={
                isICloud
                  ? t("icloudPathPlaceholder")
                  : isWebDAV
                    ? "https://dav.jianguoyun.com/dav/ReFlow/"
                    : "https://reader.example.com"
              }
              value={endpoint}
              onChange={(event) => {
                setEndpoint(event.target.value)
                invalidateConnectionTest()
              }}
            />
            {isICloud && <p className="field-hint">{t("icloudDefaultPathHint")}</p>}
            {isICloud && <p className="field-hint">{t("icloudOtherDevicesHint")}</p>}
            {isWebDAV && <p className="field-hint">{t("nutstoreWebDAVHint")}</p>}
            {usesAPIKey ? (
              <>
                <label className="field-label" htmlFor="sync-api-key">
                  {t("apiKey")}
                </label>
                <input
                  id="sync-api-key"
                  className="text-input"
                  type="password"
                  autoComplete="off"
                  value={apiKey}
                  placeholder={editing ? t("leaveBlankToKeep") : undefined}
                  onChange={(event) => {
                    setAPIKey(event.target.value)
                    invalidateConnectionTest()
                  }}
                />
              </>
            ) : usesBasicAuth ? (
              <>
                <label className="field-label" htmlFor="sync-username">
                  {t("username")}
                </label>
                <input
                  id="sync-username"
                  className="text-input"
                  autoComplete="username"
                  value={username}
                  placeholder={editing ? t("leaveBlankToKeep") : undefined}
                  onChange={(event) => {
                    setUsername(event.target.value)
                    invalidateConnectionTest()
                  }}
                />
                <label className="field-label" htmlFor="sync-password">
                  {isWebDAV ? t("passwordOrAppPassword") : t("password")}
                </label>
                <input
                  id="sync-password"
                  className="text-input"
                  type="password"
                  autoComplete="current-password"
                  value={password}
                  placeholder={editing ? t("leaveBlankToKeep") : undefined}
                  onChange={(event) => {
                    setPassword(event.target.value)
                    invalidateConnectionTest()
                  }}
                />
              </>
            ) : isICloud ? null : (
              <>
                <label className="field-label" htmlFor="sync-token">
                  {t("accessToken")}
                </label>
                <input
                  id="sync-token"
                  className="text-input"
                  type="password"
                  autoComplete="off"
                  value={token}
                  placeholder={editing ? t("leaveBlankToKeep") : undefined}
                  onChange={(event) => {
                    setToken(event.target.value)
                    invalidateConnectionTest()
                  }}
                />
                <label className="field-label" htmlFor="sync-username">
                  {t("username")}
                </label>
                <input
                  id="sync-username"
                  className="text-input"
                  autoComplete="username"
                  value={username}
                  placeholder={editing ? t("leaveBlankToKeep") : undefined}
                  onChange={(event) => {
                    setUsername(event.target.value)
                    invalidateConnectionTest()
                  }}
                />
                <label className="field-label" htmlFor="sync-password">
                  {t("password")}
                </label>
                <input
                  id="sync-password"
                  className="text-input"
                  type="password"
                  autoComplete="current-password"
                  value={password}
                  placeholder={editing ? t("leaveBlankToKeep") : undefined}
                  onChange={(event) => {
                    setPassword(event.target.value)
                    invalidateConnectionTest()
                  }}
                />
              </>
            )}
            {editing && !isICloud && <p className="field-hint">{t("savedCredentialsHint")}</p>}
            <label className="field-label" htmlFor="sync-interval">
              {t("syncInterval")}
            </label>
            <select
              id="sync-interval"
              className="select-input"
              value={interval}
              onChange={(event) => setInterval(Number(event.target.value))}
            >
              <option value={15}>{t("minutes15")}</option>
              <option value={30}>{t("minutes30")}</option>
              <option value={60}>{t("hour1")}</option>
              <option value={180}>{t("hours3")}</option>
              <option value={720}>{t("hours12")}</option>
              <option value={1440}>{t("day1")}</option>
            </select>
            {!isICloud && (
              <label className="checkbox-row" htmlFor="sync-private-network">
                <input
                  id="sync-private-network"
                  type="checkbox"
                  checked={allowPrivate}
                  onChange={(event) => {
                    setAllowPrivate(event.target.checked)
                    invalidateConnectionTest()
                  }}
                />
                <span>{t("allowPrivateEndpoint")}</span>
              </label>
            )}
            {connection.status === "success" && (
              <p className="form-status form-status--success" role="status">
                <CheckCircle />
                {t("connectionSuccessful")}
              </p>
            )}
            {connection.status === "error" && (
              <p className="form-error" role="alert">
                {connection.message}
              </p>
            )}
            {props.error && (
              <p className="form-error" role="alert">
                {props.error.message}
              </p>
            )}
            <div className="dialog-actions dialog-actions--split">
              {isWebDAV && (
                <button
                  className="button button--secondary"
                  type="button"
                  disabled={!endpointReady || connection.status === "pending"}
                  onClick={() => void testConnection()}
                >
                  {connection.status === "pending" ? (
                    <CircleNotch className="spin" />
                  ) : (
                    <PlugsConnected />
                  )}
                  {connection.status === "pending" ? t("testingConnection") : t("testConnection")}
                </button>
              )}
              <button
                className="button button--primary"
                type="submit"
                disabled={props.pending || !endpointReady || !credentialsReady}
              >
                {props.pending ? <CircleNotch className="spin" /> : <CloudArrowDown />}
                {editing ? t("saveChanges") : t("addAccount")}
              </button>
            </div>
          </form>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  )
}
