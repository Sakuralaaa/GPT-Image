import { httpRequest } from "@/lib/request";

export type ApiKeyItem = {
  id: string;
  name: string;
  key_prefix: string;
  enabled: boolean;
  expires_at: string;
  expired: boolean;
  total_limit: number;
  daily_limit: number;
  image_limit: number;
  rpm_limit: number;
  concurrency_limit: number;
  models: string[];
  used_requests: number;
  used_images: number;
  today_requests: number;
  today_images: number;
  last_used_at: string;
  created_at: string;
  running: number;
  source?: string;
};

export type ApiKeyInput = Partial<{
  name: string;
  key: string;
  enabled: boolean;
  expires_at: string;
  total_limit: number;
  daily_limit: number;
  image_limit: number;
  rpm_limit: number;
  concurrency_limit: number;
  models: string[] | string;
}>;

export type UsageSummary = {
  keys: number;
  enabled_keys: number;
  today_requests: number;
  today_images: number;
  total_requests: number;
  total_images: number;
};

export function fetchApiKeys() {
  return httpRequest<{ items: ApiKeyItem[]; summary: UsageSummary }>("/api/keys");
}

export function createApiKey(input: ApiKeyInput) {
  return httpRequest<{ item: ApiKeyItem; key: string }>("/api/keys", { method: "POST", body: input });
}

export function updateApiKey(id: string, input: ApiKeyInput) {
  return httpRequest<{ item: ApiKeyItem }>(`/api/keys/${encodeURIComponent(id)}`, { method: "POST", body: input });
}

export function deleteApiKey(id: string) {
  return httpRequest<{ ok: boolean }>(`/api/keys/${encodeURIComponent(id)}`, { method: "DELETE" });
}

export function resetApiKeyUsage(id: string) {
  return httpRequest<{ item: ApiKeyItem }>(`/api/keys/${encodeURIComponent(id)}/reset-usage`, { method: "POST", body: {} });
}

export type DashboardData = {
  accounts: {
    total: number;
    enabled: number;
    quota: number;
    by_status: Record<string, number>;
    by_type: Record<string, number>;
  };
  keys: UsageSummary;
  key_list: Array<Pick<ApiKeyItem, "id" | "name" | "key_prefix" | "enabled" | "today_requests" | "today_images" | "used_requests" | "used_images" | "daily_limit" | "total_limit" | "image_limit" | "last_used_at">>;
  recovery: { jobs: number; counts: Record<string, number>; auto_enabled: boolean };
};

export function fetchDashboard() {
  return httpRequest<DashboardData>("/api/dashboard");
}

export type RecoveryStatus = "queued" | "running" | "awaiting_code" | "succeeded" | "failed" | "cancelled";

export type RecoveryItem = {
  id: string;
  email: string;
  status: RecoveryStatus;
  stage: string;
  error: string;
  method: string;
  channel: string;
  attempts: number;
  token_preview: string;
  steps: string[];
  updated_at: string;
  finished_at: string;
};

export type RecoveryJob = {
  id: string;
  name: string;
  source: "manual" | "auto";
  concurrency: number;
  created_at: string;
  updated_at: string;
  items: RecoveryItem[];
  counts: Record<string, number>;
  total: number;
  status: "running" | "awaiting_code" | "done";
};

export type RecoverySettings = {
  default_concurrency: number;
  global_concurrency: number;
  otp_wait_seconds: number;
  auto_enabled: boolean;
  auto_concurrency: number;
  auto_interval_minutes: number;
  auto_cooldown_minutes: number;
  auto_daily_attempt_cap: number;
  auto_include_expired: boolean;
  auto_skip_without_creds: boolean;
  outlook_base_url: string;
  outlook_api_key: string;
  outlook_domains: string[];
};

export function fetchRecoveryJobs() {
  return httpRequest<{ items: RecoveryJob[] }>("/api/recovery/jobs");
}

export function createRecoveryJob(input: { name?: string; emails_text?: string; account_ids?: string[]; concurrency?: number }) {
  return httpRequest<{ item: RecoveryJob }>("/api/recovery/jobs", { method: "POST", body: input });
}

export function deleteRecoveryJob(id: string) {
  return httpRequest<{ ok: boolean }>(`/api/recovery/jobs/${encodeURIComponent(id)}`, { method: "DELETE" });
}

export function retryRecoveryJob(id: string, itemIds: string[] = []) {
  return httpRequest<{ retried: number; item: RecoveryJob }>(`/api/recovery/jobs/${encodeURIComponent(id)}/retry`, { method: "POST", body: { item_ids: itemIds } });
}

export function cancelRecoveryJob(id: string, itemIds: string[] = []) {
  return httpRequest<{ cancelled: number; item: RecoveryJob }>(`/api/recovery/jobs/${encodeURIComponent(id)}/cancel`, { method: "POST", body: { item_ids: itemIds } });
}

export function submitRecoveryCodes(id: string, codes: Record<string, string>) {
  return httpRequest<{ accepted: number; errors: Record<string, string>; item: RecoveryJob }>(`/api/recovery/jobs/${encodeURIComponent(id)}/codes`, { method: "POST", body: { codes } });
}

export function fetchRecoverySettings() {
  return httpRequest<{ settings: RecoverySettings }>("/api/recovery/settings");
}

export function updateRecoverySettings(settings: Partial<RecoverySettings>) {
  return httpRequest<{ settings: RecoverySettings }>("/api/recovery/settings", { method: "POST", body: settings });
}

export function runAutoHealNow() {
  return httpRequest<{ queued: number }>("/api/recovery/auto-run", { method: "POST", body: {} });
}

export type MailRoute = {
  channel: string;
  entry_ref: string;
  override: boolean;
  ready: boolean;
  reason: string;
};

export type AccountDetail = {
  id: string;
  email: string;
  password: string;
  mail_provider: string;
  mail_ref: string;
  mail_token: string;
  mail_channel: string;
  session_token: string;
  note: string;
  status: string;
  type: string;
  token_preview: string;
  created_at?: string | null;
  last_refresh_at?: string | null;
  last_error?: string | null;
  refresh_failures: number;
  relogin_failures: number;
  refresh_next_at?: string | null;
  mail_route?: MailRoute;
};

export type AccountCredentialUpdate = Partial<Pick<AccountDetail, "email" | "password" | "mail_provider" | "mail_ref" | "mail_token" | "mail_channel" | "session_token" | "note">>;

const detailPath = (id: string) => `/api/accounts/detail/${encodeURIComponent(id)}`;

export function fetchAccountDetail(id: string) {
  return httpRequest<{ item: AccountDetail }>(detailPath(id));
}

export function updateAccountCredentials(id: string, updates: AccountCredentialUpdate) {
  return httpRequest<{ item: AccountDetail }>(detailPath(id), { method: "POST", body: updates });
}

export type LatestMail = {
  channel: string;
  entry_ref: string;
  empty: boolean;
  subject?: string;
  code?: string;
  text?: string;
  received_at?: string;
};

export function readAccountLatestMail(id: string) {
  return httpRequest<{ mail: LatestMail }>(`${detailPath(id)}/mail`, { method: "POST", body: {} });
}

export function refreshAccountToken(id: string) {
  return httpRequest<{ item: AccountDetail }>(`${detailPath(id)}/refresh`, { method: "POST", body: {} });
}

export function replaceAccountToken(id: string, input: { access_token?: string; session_token?: string; session_json?: string }) {
  return httpRequest<{ item: AccountDetail }>(`${detailPath(id)}/token`, { method: "POST", body: input });
}

export function startAccountLogin(id: string, mode: "relogin" | "otp-login") {
  return httpRequest<{ job: RecoveryJob }>(`${detailPath(id)}/${mode}`, { method: "POST", body: {} });
}

export type ExportedAccount = Record<string, string | boolean | number | null>;

export function exportAccountCredentials(format: "json" | "csv", accountIds: string[] = []) {
  return httpRequest<{ format: string; items?: ExportedAccount[]; content?: string; count: number }>("/api/accounts/export", {
    method: "POST",
    body: { confirm: true, format, account_ids: accountIds },
  });
}

export type ImportCredentialsResult = {
  added: number;
  updated: number;
  skipped: number;
  errors: Array<{ row: string; email?: string; error: string }>;
};

export function importAccountCredentials(content: string, format?: "json" | "csv") {
  return httpRequest<ImportCredentialsResult>("/api/accounts/import-credentials", { method: "POST", body: { content, format } });
}

export function downloadTextFile(filename: string, content: string, mime = "text/plain") {
  const blob = new Blob([content], { type: `${mime};charset=utf-8` });
  const url = URL.createObjectURL(blob);
  const link = document.createElement("a");
  link.href = url;
  link.download = filename;
  document.body.appendChild(link);
  link.click();
  link.remove();
  URL.revokeObjectURL(url);
}

export const MAIL_CHANNEL_LABELS: Record<string, string> = {
  cloudflare_temp_email: "CF 临时邮箱",
  outlook: "outlookEmail",
  gptmail: "GPTMail",
};

export const RECOVERY_STATUS_LABELS: Record<RecoveryStatus, string> = {
  queued: "排队中",
  running: "进行中",
  awaiting_code: "待填码",
  succeeded: "成功",
  failed: "失败",
  cancelled: "已取消",
};
