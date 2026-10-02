import * as Dialog from "@radix-ui/react-dialog"
import { Brain, CircleNotch, X } from "@phosphor-icons/react"
import { type FormEvent, useMemo, useState } from "react"

import type { CreateAIProfileInput } from "../api/client"
import type { AIProfile, AIProvider, AIProviderID } from "../api/types"
import { useTranslation } from "../lib/i18n"

/**
 * What PATCH /ai/profiles/{id} can actually change. `provider` is not updatable
 * and `is_default` stays on the row's own control, so neither is sent here;
 * `api_key` is present only when the user typed one, because an explicit empty
 * key overwrites the stored secret instead of keeping it.
 */
export interface EditAIProfileInput {
  name: string
  endpoint: string
  model: string
  allow_private_network: boolean
  remote_content_approved: boolean
  api_key?: string
}

interface AIProfileDialogProps {
  open: boolean
  providers: AIProvider[]
  /** Present means edit mode: the form is prefilled from this profile. */
  profile?: AIProfile
  pending: boolean
  error: Error | null
  onOpenChange: (open: boolean) => void
  onCreate: (input: CreateAIProfileInput) => void
  onSave: (input: EditAIProfileInput) => void
}

const defaults: Record<AIProviderID, { endpoint: string; model: string }> = {
  openai_compatible: { endpoint: "https://api.openai.com/v1", model: "gpt-4.1-mini" },
  ollama: { endpoint: "http://127.0.0.1:11434", model: "qwen3:8b" },
}

export function AIProfileDialog(props: AIProfileDialogProps) {
  const { t } = useTranslation()
  const profile = props.profile
  const editing = profile !== undefined
  const initialProvider = profile?.provider ?? "openai_compatible"
  const [provider, setProvider] = useState<AIProviderID>(initialProvider)
  const [name, setName] = useState(profile?.name ?? "")
  const [endpoint, setEndpoint] = useState(profile?.endpoint ?? defaults[initialProvider].endpoint)
  const [model, setModel] = useState(profile?.model ?? defaults[initialProvider].model)
  const [apiKey, setAPIKey] = useState("")
  const [temperature, setTemperature] = useState(0.2)
  const [allowPrivate, setAllowPrivate] = useState(profile?.allow_private_network ?? false)
  const [privacyApproved, setPrivacyApproved] = useState(profile?.remote_content_approved ?? false)
  const [isDefault, setIsDefault] = useState(!editing)
  const providerName = useMemo(() => props.providers.find((item) => item.id === provider)?.name ?? provider, [props.providers, provider])
  const remote = isRemoteEndpoint(endpoint)
  const ready = endpoint.trim() !== "" && model.trim() !== "" && (!remote || privacyApproved)
  const changeProvider = (value: AIProviderID) => {
    setProvider(value)
    setEndpoint(defaults[value].endpoint)
    setModel(defaults[value].model)
    setAPIKey("")
    setAllowPrivate(value === "ollama")
    setPrivacyApproved(false)
  }
  const submit = (event: FormEvent) => {
    event.preventDefault()
    if (!ready) return
    if (profile) {
      props.onSave({
        name: name.trim() || providerName,
        endpoint: endpoint.trim(),
        model: model.trim(),
        allow_private_network: allowPrivate,
        remote_content_approved: privacyApproved,
        ...(apiKey.trim() ? { api_key: apiKey.trim() } : {}),
      })
      return
    }
    props.onCreate({
      provider,
      name: name.trim() || providerName,
      endpoint: endpoint.trim(),
      model: model.trim(),
      api_key: apiKey.trim(),
      settings: { temperature },
      allow_private_network: allowPrivate,
      remote_content_approved: privacyApproved,
      is_default: isDefault,
    })
  }
  return (
    <Dialog.Root open={props.open} onOpenChange={props.onOpenChange}>
      <Dialog.Portal>
        <Dialog.Overlay className="dialog-overlay" />
        <Dialog.Content className="dialog-content" aria-describedby={undefined}>
          <div className="dialog-header">
            <Dialog.Title>{editing ? t("editAIProvider") : t("addAIProvider")}</Dialog.Title>
            <Dialog.Close asChild><button className="icon-button" type="button" aria-label={t("close")} title={t("close")}><X /></button></Dialog.Close>
          </div>
          <form className="dialog-form" onSubmit={submit}>
            <label className="field-label" htmlFor="ai-provider">{t("provider")}</label>
            <select id="ai-provider" className="select-input" value={provider} disabled={editing} onChange={(event) => changeProvider(event.target.value as AIProviderID)}>
              {props.providers.map((item) => <option value={item.id} key={item.id}>{item.name}</option>)}
            </select>
            {editing && <p className="field-hint">{t("aiProviderKindLockedHint")}</p>}
            <label className="field-label" htmlFor="ai-name">{t("profileName")}</label>
            <input id="ai-name" className="text-input" value={name} placeholder={providerName} maxLength={120} onChange={(event) => setName(event.target.value)} />
            <label className="field-label" htmlFor="ai-endpoint">{t("serverURL")}</label>
            <input id="ai-endpoint" className="text-input" type="url" inputMode="url" autoComplete="url" value={endpoint} onChange={(event) => setEndpoint(event.target.value)} />
            <label className="field-label" htmlFor="ai-model">{t("model")}</label>
            <input id="ai-model" className="text-input" value={model} maxLength={200} onChange={(event) => setModel(event.target.value)} />
            {provider === "openai_compatible" && <><label className="field-label" htmlFor="ai-api-key">{t("apiKey")}</label><input id="ai-api-key" className="text-input" type="password" autoComplete="off" placeholder={editing ? t("leaveBlankToKeep") : undefined} value={apiKey} onChange={(event) => setAPIKey(event.target.value)} /></>}
            {editing && provider === "openai_compatible" && <p className="field-hint">{t("apiKeyKeepHint")}</p>}
            {/* 编辑模式不放温度滑块：GET /ai/profiles 不返回 settings，回填不了，
                照着默认值发一次 PATCH 反而会把用户存好的温度覆盖掉。 */}
            {!editing && <><label className="field-label" htmlFor="ai-temperature">{t("temperature")}</label>
            <div className="range-row">
              <input id="ai-temperature" className="range-input" type="range" min="0" max="2" step="0.1" value={temperature} onChange={(event) => setTemperature(Number(event.target.value))} />
              <output className="range-output" htmlFor="ai-temperature">{temperature.toFixed(1)}</output>
            </div></>}
            <label className="checkbox-row" htmlFor="ai-private-network"><input id="ai-private-network" type="checkbox" checked={allowPrivate} onChange={(event) => setAllowPrivate(event.target.checked)} /><span>{t("allowPrivateEndpoint")}</span></label>
            <label className="checkbox-row privacy-confirmation" htmlFor="ai-privacy"><input id="ai-privacy" type="checkbox" checked={privacyApproved} onChange={(event) => setPrivacyApproved(event.target.checked)} /><span>{t("articleMayBeSent")}</span></label>
            {/* 默认提供商由列表行的 Check 按钮负责：这里再放一个开关，取消勾选会把
                唯一的默认提供商清空，而 PATCH 没有“重新指派”的语义。 */}
            {!editing && <label className="checkbox-row" htmlFor="ai-default"><input id="ai-default" type="checkbox" checked={isDefault} onChange={(event) => setIsDefault(event.target.checked)} /><span>{t("defaultAIProvider")}</span></label>}
            {props.error && <p className="form-error" role="alert">{props.error.message}</p>}
            <div className="dialog-actions dialog-actions--end">
              <button className="button button--primary" type="submit" disabled={props.pending || !ready}>{props.pending ? <CircleNotch className="spin" /> : <Brain />}{editing ? t("saveChanges") : t("addProvider")}</button>
            </div>
          </form>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  )
}

function isRemoteEndpoint(endpoint: string) {
  try {
    const host = new URL(endpoint).hostname
    return host !== "localhost" && host !== "127.0.0.1" && host !== "::1"
  } catch {
    return true
  }
}
