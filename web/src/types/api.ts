export type Site = 'domestic' | 'global';
export type Product = 'codebuddy' | 'workbuddy';
export type SystemPromptMode = 'auto' | 'keep' | 'fold' | 'strip';
export type CheckinMode = 'human' | 'allDay' | 'window' | 'fixed';
export type RouteMode = 'always' | 'onExhausted';

export interface CreditPackage {
  packageName?: string;
  remaining?: number | null;
  total?: number | null;
  used?: number | null;
  expiresAt?: number;
}

export interface CreditsInfo {
  unlimited?: boolean;
  remaining?: number | null;
  total?: number | null;
  used?: number | null;
  display?: string;
  label?: string;
  packages?: CreditPackage[];
}

export interface AccountUsageItem {
  accountId: string;
  ok?: boolean;
  error?: string;
  credits?: CreditsInfo;
  officialUsageUrl?: string;
  notify?: { level?: 'good' | 'warn' | 'bad'; message?: string };
  quota?: { checkedAt?: number };
}

export interface AccountSummary {
  id: string;
  label?: string;
  site?: string;
  enabled: boolean;
  hasCredentials: boolean;
  loggedIn?: boolean;
  tokenExpired?: boolean;
  userId?: string;
  userName?: string;
  userNickname?: string;
  authType?: string;
  proxy?: string;
  successRequests?: number;
  failedRequests?: number;
  lastError?: string;
  quotaExhausted?: boolean;
  quotaResetAt?: number;
  quotaRemaining?: number | null;
  quotaUnlimited?: boolean;
  quotaCheckedAt?: number;
  cooldownUntil?: number;
  cooldownSource?: string;
  modelCooldowns?: Record<string, number>;
  checkinToday?: 'done' | 'already' | 'failed' | 'none' | string;
  checkinCount48h?: number;
  checkinNextAt?: number;
  checkinLastError?: string;
}

export interface AccountsPayload {
  accounts?: AccountSummary[];
  primary?: AccountSummary | null;
  loggedIn?: boolean;
  enabledCount?: number;
  activeEnabledCount?: number;
  activeAvailableCount?: number;
  domesticCount?: number;
  globalCount?: number;
  activeSite?: string;
}

export interface ConcurrencyAccountView {
  accountId: string;
  label?: string;
  inFlight: number;
  rpm1: number;
  rpm5: number;
  rpm60: number;
  peakInFlight: number;
  rateLimits60: number;
  avgLatencyMs?: number;
}

export interface ConcurrencyPayload {
  generatedAt?: number;
  globalInFlight?: number;
  accounts?: ConcurrencyAccountView[] | number;
  rateLimits?: unknown[] | number;
}

export interface WatchPayload {
  enabled?: boolean;
  ok?: boolean;
  reason?: string;
  metrics?: {
    errorRate?: number | null;
    reasoningAsked?: number;
    reasoningMissed?: number;
    reasoningMissRate?: number;
    latencyP50?: number;
    samples?: number;
  };
}

export interface StatusPayload {
  ok: boolean;
  version?: string;
  build?: { version?: string };
  transport?: string;
  poolSite?: string;
  poolProduct?: string;
  product?: string;
  note?: string;
  creditsRefreshed?: boolean;
  config?: {
    site?: string;
    poolSite?: string;
    product?: string;
    poolProduct?: string;
  };
  upstream?: {
    probed?: boolean;
    ok?: boolean;
    message?: string;
  };
  stats?: {
    successRequests?: number;
    failedRequests?: number;
    totalTokens?: number;
    totalCachedTokens?: number;
  };
  accounts?: AccountsPayload;
  accountUsages?: AccountUsageItem[];
  watch?: WatchPayload;
  concurrency?: ConcurrencyPayload;
}

export interface UpdateState {
  phase?: 'idle' | 'checking' | 'downloading' | 'restarting' | 'error' | string;
  current?: string;
  latest?: string;
  available?: boolean;
  message?: string;
}

export interface BoundApiKey {
  key?: string;
  preview?: string;
  site?: string;
  accountId?: string;
}

export interface ClientConfigPayload {
  ok?: boolean;
  baseUrl?: string;
  apiBase?: string;
  chatCompletionsUrl?: string;
  responsesUrl?: string;
  recommendedModel?: string;
  apiKey?: string;
  apiKeyPreview?: string;
  apiKeyConfigured?: boolean;
  apiKeys?: BoundApiKey[];
  note?: string;
}

export interface CheckinRule {
  accountId: string;
  enabled: boolean;
  mode: CheckinMode | string;
  start: string;
  end: string;
  offset: number;
}

export interface CheckinConfigPayload {
  ok?: boolean;
  auto: boolean;
  rules: CheckinRule[];
  allowDirectFallback: boolean;
  checkinRetryMin: number;
  checkinRetryMax: number;
  path?: string;
}

export interface CheckinLogEntry {
  ranAt?: number;
  at?: number;
  accountId?: string;
  ok?: boolean;
  already?: boolean;
  degraded?: boolean;
  credits?: number;
  egress?: string;
  message?: string;
}

export interface PoolCheckinResult {
  ok?: boolean;
  note?: string;
  summary?: {
    checkedIn?: number;
    alreadyDone?: number;
    skipped?: number;
    unsupported?: number;
    failed?: number;
  };
  results?: Array<{
    accountId?: string;
    label?: string;
    ok?: boolean;
    supported?: boolean;
    alreadyCheckedIn?: boolean;
    rewardCredits?: number;
    message?: string;
  }>;
}

export interface PoolChatTestResult {
  ok?: boolean;
  model?: string;
  note?: string;
  summary?: {
    passed?: number;
    failed?: number;
    skipped?: number;
  };
  results?: Array<{
    accountId?: string;
    label?: string;
    ok?: boolean;
    latencyMs?: number;
    model?: string;
    message?: string;
  }>;
}

export interface ProxyCandidate {
  id: string;
  url: string;
  label?: string;
  addedAt?: number;
}

export interface ProxiesPayload {
  ok?: boolean;
  candidates: ProxyCandidate[];
  used: Array<{ id: string; proxy: string; count: number }>;
  direct: number;
  total: number;
}

export interface ProxyCheckResult {
  ok: boolean;
  proxy?: string;
  ip?: string;
  elapsedMs?: number;
  error?: string;
  direct?: { ok?: boolean; ip?: string; elapsedMs?: number };
}

export interface ModelItem {
  id?: string;
  modelId?: string;
  upstreamId?: string;
  name?: string;
  displayName?: string;
  model?: string;
  credits?: string;
  free?: boolean;
  creditMultiplier?: number | null;
  description?: string;
  sites?: Site[];
}

export interface ModelRouteRule {
  fromSite: Site;
  match: string;
  site: Site;
  target: string;
  mode: RouteMode;
  note: string;
}

export interface TitleRoute {
  enabled?: boolean;
  domestic?: string;
  global?: string;
}

export interface ModelRoutesPayload {
  ok?: boolean;
  rules?: ModelRouteRule[];
  catalog?: { domestic?: string[]; global?: string[] };
  titleRoute?: TitleRoute;
  freeModels?: Record<string, string[]>;
}

export interface SystemPromptEvent {
  at: number;
  requestId?: string;
  accountId?: string;
  site?: string;
  model?: string;
  mode?: string;
  form?: string;
  degraded?: boolean;
  recovered?: boolean;
  fingerprint?: string;
  systemLen?: number;
  clientHint?: string;
  sessionSource?: string;
  stripped?: boolean;
  strippedBytes?: number;
  status?: number;
  errorCode?: string;
  durationMs?: number;
}

export interface SystemPromptStats {
  total?: number;
  keep?: number;
  fold?: number;
  /** 首试前剥离（或剥离后直传）发生的事件数。后端一直返回，旧面板有展示。 */
  stripped?: number;
  degraded?: number;
  blocked?: number;
  failed?: number;
  recovered?: number | null;
}

export interface SystemPromptOverview {
  ok?: boolean;
  mode: SystemPromptMode;
  /**
   * 实时生效的剥离开关。面板用它给「system 预览」的勾选框做默认显示，
   * 试算请求不指定时后端也会按这三个值执行。
   */
  strip?: boolean;
  stripAssistant?: boolean;
  echoUser?: boolean;
  stats: SystemPromptStats;
  statsScope?: string;
  recent: SystemPromptEvent[];
  dates: string[];
}

export interface SystemPromptHistoryResponse {
  ok?: boolean;
  date?: string;
  total?: number;
  hasMore?: boolean;
  events: SystemPromptEvent[];
}

/** 一次剥离的实测结果（对应后端 provider.StripReport）。 */
export interface SystemDryRunStripReport {
  Stripped: boolean;
  Bytes: number;
  SystemBytes: number;
  AssistantBytes: number;
}

/**
 * POST /direct-admin/api/system-dryrun 的 200 响应。
 * 失败时后端回 {"ok":false,"error":"..."}（400），由 apiRequest 转成 Error 抛出。
 */
export interface SystemDryRunResponse {
  ok?: boolean;
  site?: string;
  mode?: string;
  modeLabel?: string;
  keepSystem?: boolean;
  stripEnabled?: boolean;
  echoEnabled?: boolean;
  outbound?: Array<Record<string, unknown>>;
  stripReport?: SystemDryRunStripReport | null;
  inMessages?: number;
  outMessages?: number;
  inSystemLen?: number;
  outSystemLen?: number;
  notes?: string[] | null;
  warnings?: string[] | null;
}

/** 试算请求体：mode 为空表示跟随当前生效档位；strip / echo 不传即跟随配置。 */
export interface SystemDryRunRequest {
  system?: string;
  messages?: Array<{ role: string; content: string }>;
  mode?: string;
  strip?: boolean;
  echo?: boolean;
}

export interface UsageEntry {
  at: number;
  proxyRequestId?: string;
  upstreamConversationRequestId?: string;
  sessionKey?: string;
  sessionLabel?: string;
  model?: string;
  /** 实际发给上游的模型名（被模型路由 / 标题改写改过时才有值） */
  routedModel?: string;
  /** 上游 SSE 帧里回显的 model 字段（空 = 未观测或旧数据） */
  upstreamModel?: string;
  /** 上游回显与请求不一致且不是 auto 一族（后端 internal/gateway/modelMismatch 判定） */
  modelMismatch?: boolean;
  ok: boolean;
  error?: string;
  accountId?: string;
  accountLabel?: string;
  promptTokens?: number;
  completionTokens?: number;
  totalTokens?: number;
  cachedTokens?: number;
  credit?: number | null;
  durationMs?: number;
  systemForm?: string;
  systemFp?: string;
  systemDegraded?: boolean;
  /** 分词器指纹：入站内容的字符成分（未观测时为 0 / 缺省） */
  tokChars?: number;
  tokCJK?: number;
  tokDigits?: number;
  tokASCII?: number;
  /** 分桶：cjk / digits / ascii / tiny / none */
  tokBucket?: string;
  /** prompt_tokens ÷ 入站字符数（保留 4 位小数；0 = 未观测） */
  tokRatio?: number;
}

export interface UsageChartPoint {
  at: number;
  label: string;
  totalTokens: number;
  cacheHitRate: number;
}

export interface UsageModelStat {
  model: string;
  requests: number;
  promptTokens: number;
  totalTokens: number;
  cachedTokens: number;
  cacheHitRate: number;
}

export interface UsageViewPayload {
  ok?: boolean;
  summary: {
    requests?: number;
    failed?: number;
    totalTokens?: number;
    cacheHitRate?: number;
    credits?: number;
    creditRows?: number;
  };
  series: UsageChartPoint[];
  byModel: UsageModelStat[];
  accounts: string[];
  models: string[];
  requests: UsageEntry[];
  requestsTotal: number;
  limit: number;
  offset: number;
}

export interface OAuthResponse {
  ok?: boolean;
  session?: { status?: string; label?: string; site?: string; error?: string; url?: string; launchUrl?: string };
  login?: { status?: string; message?: string; url?: string; launchUrl?: string };
}

export interface ImportAccountsResult {
  ok?: boolean;
  added?: number;
  updated?: number;
  failed?: number;
  results?: Array<{
    index?: number;
    id?: string;
    label?: string;
    site?: string;
    status?: 'added' | 'updated' | 'failed' | string;
    error?: string;
  }>;
}
