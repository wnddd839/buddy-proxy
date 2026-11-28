import type {
  AccountSummary,
  AccountUsageItem,
  CheckinMode,
  CheckinRule,
  ConcurrencyAccountView,
  ConcurrencyPayload,
  ModelItem,
  ModelRouteRule,
  RouteMode,
  Site,
  SystemPromptEvent,
  UsageEntry,
} from '@/types/api';
import { bareModelId, formatResetAt, normalizeSite } from './format';

export type Tone = 'ok' | 'warn' | 'err' | 'info' | 'neutral';

export interface ConcurrencySummary {
  globalInFlight: number;
  peakInFlight: number;
  rateLimits: number;
  byAccount: Record<string, ConcurrencyAccountView>;
  /**
   * false = 从未成功取到并发快照。此时四个数字都是 0，但那是「不知道」而不是
   * 「没有请求」—— 调用方必须显示 '—'，不能把 0 当成事实。
   */
  known: boolean;
}

/**
 * 兼容两种响应形态：
 * 1) /api/status 中的数字汇总（accounts/rateLimits 为数字）
 * 2) /api/concurrency 中的逐账号快照（accounts 为数组）
 */
export function summarizeConcurrency(c?: ConcurrencyPayload | null): ConcurrencySummary {
  if (!c) {
    return { globalInFlight: 0, peakInFlight: 0, rateLimits: 0, byAccount: {}, known: false };
  }
  const views = Array.isArray(c.accounts) ? c.accounts : [];
  const globalInFlight = c.globalInFlight || 0;
  let peakInFlight = 0;
  let rateLimits = 0;
  const byAccount: Record<string, ConcurrencyAccountView> = {};

  for (const a of views) {
    if (!a) continue;
    byAccount[a.accountId] = a;
    if ((a.peakInFlight || 0) > peakInFlight) peakInFlight = a.peakInFlight;
    rateLimits += a.rateLimits60 || 0;
  }
  if (!Array.isArray(c.accounts)) {
    rateLimits = typeof c.rateLimits === 'number' ? c.rateLimits : 0;
  }

  return { globalInFlight, peakInFlight, rateLimits, byAccount, known: true };
}

export interface ExpiryPackageRow {
  packageName: string;
  expiresAt: number;
  statusLabel: string;
  tone: Tone;
  remainingText: string;
  totalText: string;
  usedText: string;
}

export interface ExpirySummary {
  totalPackages: number;
  remainingTotal: number;
  expiringCount7d: number;
  expiringAmount7d: number;
  checkedAt: number | null;
  rows: ExpiryPackageRow[];
}

function formatPackageNumber(value?: number | null): string {
  if (value == null) return '未知';
  if (Number(value) === -1) return '不限量';
  return String(value);
}

/** 计算单个账号的积分包到期明细（不修改入参原数组） */
export function computeExpirySummary(
  usage?: AccountUsageItem | null,
  now = Date.now(),
): ExpirySummary {
  const packages = ((usage?.credits?.packages) || []).slice().sort((a, b) => {
    return (a.expiresAt || Infinity) - (b.expiresAt || Infinity);
  });

  let expiringCount7d = 0;
  let expiringAmount7d = 0;
  let remainingTotal = 0;
  const day = 86_400_000;

  const rows: ExpiryPackageRow[] = packages.map((p) => {
    const expiry = Number(p.expiresAt) || 0;
    const left = Number(p.remaining);
    if (Number.isFinite(left) && left > 0) remainingTotal += left;
    const delta = expiry - now;

    let statusLabel = '到期未知';
    let tone: Tone = 'neutral';
    if (expiry && delta <= 0) {
      statusLabel = '已过期';
      tone = 'err';
    } else if (expiry && delta <= day) {
      statusLabel = '24h 内';
      tone = 'err';
    } else if (expiry && delta <= 3 * day) {
      statusLabel = '3 天内';
      tone = 'warn';
    } else if (expiry && delta <= 7 * day) {
      statusLabel = '7 天内';
      tone = 'warn';
    } else if (expiry) {
      statusLabel = '有效';
      tone = 'ok';
    }

    if (expiry && delta > 0 && delta <= 7 * day && Number.isFinite(left) && left > 0) {
      expiringCount7d++;
      expiringAmount7d += left;
    }

    return {
      packageName: p.packageName || '未命名套餐',
      expiresAt: expiry,
      statusLabel,
      tone,
      remainingText: formatPackageNumber(p.remaining),
      totalText: formatPackageNumber(p.total),
      usedText: formatPackageNumber(p.used),
    };
  });

  return {
    totalPackages: packages.length,
    remainingTotal,
    expiringCount7d,
    expiringAmount7d,
    checkedAt: usage?.quota?.checkedAt || null,
    rows,
  };
}

export interface AccountStateItem {
  tone: Tone;
  text: string;
}

export function computeAccountStates(a: AccountSummary, now = Date.now()): AccountStateItem[] {
  const out: AccountStateItem[] = [];
  if (a.quotaExhausted && a.quotaResetAt) {
    out.push({ tone: 'err', text: `配额耗尽 · ${formatResetAt(a.quotaResetAt)} 恢复` });
  } else if (a.cooldownUntil && a.cooldownUntil > now) {
    const quotaCool = a.cooldownSource === 'quota';
    out.push({
      tone: 'warn',
      text: `${quotaCool ? '额度冷却' : '冷却中'} · ${formatResetAt(a.cooldownUntil)}${quotaCool ? ' 重探' : ' 恢复'}`,
    });
  } else if (a.quotaRemaining != null && Number(a.quotaRemaining) <= 0) {
    out.push({ tone: 'warn', text: '配额可能已耗尽' });
  }
  Object.entries(a.modelCooldowns || {}).forEach(([model, until]) => {
    if (until > now) {
      out.push({
        tone: 'warn',
        text: `模型冷却 · ${model} · ${formatResetAt(until)} 恢复`,
      });
    }
  });
  return out;
}

export function computeCreditLevel(usage?: AccountUsageItem | null): 'good' | 'warn' | 'bad' {
  const c = usage?.credits;
  let level: 'good' | 'warn' | 'bad' = 'good';
  if (c && !c.unlimited && Number(c.total) > 0) {
    const ratio = Number(c.remaining) / Number(c.total);
    if (ratio <= 0.05 || Number(c.remaining) <= 0) level = 'bad';
    else if (ratio <= 0.2) level = 'warn';
  }
  if (usage?.notify?.level === 'bad') level = 'bad';
  else if (usage?.notify?.level === 'warn' && level === 'good') level = 'warn';
  return level;
}

export interface ModelAvailability {
  ok: number;
  cool: number;
  blocked: number;
  tone: Tone;
  text: string;
  title: string;
}

export function computeModelAvailability(
  modelId: string,
  sites: Site[] | undefined,
  accounts: AccountSummary[],
  quotaFloorSaved: number,
  now = Date.now(),
): ModelAvailability {
  let ok = 0;
  let cool = 0;
  let blocked = 0;

  for (const a of accounts) {
    if (!a.enabled || !a.hasCredentials) continue;
    if (sites && sites.length && !sites.includes(normalizeSite(a.site))) continue;
    const until = (a.modelCooldowns || {})[modelId] || 0;
    const accountCool = (a.cooldownUntil || 0) > now;
    if (until > now || accountCool) {
      cool++;
      continue;
    }
    const fresh = (a.quotaCheckedAt || 0) > 0 && now - (a.quotaCheckedAt || 0) <= 6 * 3_600_000;
    const quotaBlocked =
      !a.quotaUnlimited &&
      a.quotaRemaining != null &&
      Number(a.quotaRemaining) <= 0 &&
      (a.quotaResetAt || 0) > now &&
      (!a.quotaCheckedAt || fresh);
    const belowFloor =
      !a.quotaUnlimited &&
      a.quotaRemaining != null &&
      fresh &&
      quotaFloorSaved > 0 &&
      Number(a.quotaRemaining) < quotaFloorSaved;
    if (quotaBlocked || belowFloor) blocked++;
    else ok++;
  }

  if (!ok && !cool && !blocked) {
    return {
      ok: 0,
      cool: 0,
      blocked: 0,
      tone: 'neutral',
      text: '无号可用',
      title: '当前号池没有启用账号',
    };
  }

  const tone: Tone = ok === 0 ? 'err' : cool > 0 ? 'warn' : 'ok';
  const text =
    `可用 ${ok}` +
    (cool ? ` · 冷却 ${cool}` : '') +
    (blocked ? ` · 额度不足 ${blocked}` : '');
  const title =
    `当前快照：可用 ${ok} 个号` +
    (cool ? `，${cool} 个号仍在冷却（账号级或该模型级）` : '') +
    (blocked ? `，${blocked} 个号额度耗尽或低于积分下限` : '') +
    '；不保证实时可用或支持该模型';

  return { ok, cool, blocked, tone, text, title };
}

/**
 * 合并国内 + 国际两站模型编目，保留两站元信息并标注 sites 数组。
 */
export function mergeModelLists(
  domesticData: unknown,
  globalData: unknown,
): ModelItem[] {
  const map = new Map<string, ModelItem>();

  const extract = (data: unknown): Array<ModelItem | string> => {
    if (Array.isArray(data)) return data as Array<ModelItem | string>;
    const obj = data as { models?: unknown; data?: unknown } | null;
    if (obj && Array.isArray(obj.models)) return obj.models as Array<ModelItem | string>;
    if (obj && Array.isArray(obj.data)) return obj.data as Array<ModelItem | string>;
    return [];
  };

  const push = (data: unknown, site: Site) => {
    for (const m of extract(data)) {
      const raw =
        typeof m === 'string'
          ? m
          : m.modelId || m.upstreamId || m.id || m.name || m.model || '';
      if (!raw) continue;
      const key = bareModelId(raw);
      const existing = map.get(key);
      if (!existing) {
        const obj = typeof m === 'string' ? {} : m;
        map.set(key, {
          ...obj,
          modelId: key,
          sites: [site],
        });
      } else {
        if (!existing.sites) existing.sites = [];
        if (!existing.sites.includes(site)) existing.sites.push(site);
        if (typeof m !== 'string') {
          if (!existing.credits && m.credits) existing.credits = m.credits;
          if (existing.creditMultiplier == null && m.creditMultiplier != null) {
            existing.creditMultiplier = m.creditMultiplier;
          }
          if (!existing.description && m.description) existing.description = m.description;
        }
      }
    }
  };

  push(domesticData, 'domestic');
  push(globalData, 'global');

  return Array.from(map.values()).sort((x, y) =>
    (x.modelId || '').localeCompare(y.modelId || ''),
  );
}

/**
 * System Prompt 事件结果语义判定：
 * 严格先判定 11128 / status===0 / status>=400 / errorCode，
 * 只有 recovered===true && (mode==='auto' || mode==='strip') && 2xx 才显示「降级成功」。
 */
export function spResultInfo(e: SystemPromptEvent): {
  label: string;
  tone: Tone;
  title?: string;
} {
  if (e.errorCode === '11128') {
    return { label: '被拦截 · 11128', tone: 'err', title: '已自动折叠重放' };
  }
  if (e.status === 0) {
    return {
      label: '传输失败',
      tone: 'neutral',
      title: '传输层失败：建连或首内容前断流',
    };
  }
  if ((e.status ?? 0) >= 400) {
    return { label: `失败 · ${e.status}`, tone: 'err' };
  }
  if (e.errorCode) {
    return { label: e.errorCode, tone: 'neutral' };
  }
  if (
    e.recovered === true &&
    (e.mode === 'auto' || e.mode === 'strip') &&
    (e.status ?? 0) >= 200 &&
    (e.status ?? 0) < 400
  ) {
    return {
      label: '降级成功',
      tone: 'warn',
      title: '11128 被拦截后自动折叠重试成功',
    };
  }
  return { label: 'OK', tone: 'ok' };
}

export function spFormInfo(e: SystemPromptEvent): {
  label: string;
  tone: Tone;
  isReplay: boolean;
  isBlocked: boolean;
} {
  let label = e.form || '—';
  let tone: Tone = e.form === 'keep' ? 'ok' : 'neutral';
  if (e.recovered === true) {
    label = 'fold(降级重放)';
    tone = 'warn';
  } else if (e.degraded === true && e.stripped === true) {
    label = 'keep(已剥离·被拦截)';
    tone = 'err';
  } else if (e.degraded === true) {
    label = 'keep(被拦截)';
    tone = 'err';
  } else if (e.stripped === true) {
    label = 'keep(已剥离)';
    tone = 'info';
  }
  return {
    label,
    tone,
    isReplay: e.recovered === true,
    isBlocked: e.degraded === true,
  };
}

/**
 * 后端 eventDate 使用 time.Local；无事件文件时的本地日期兜底不得使用 toISOString()（UTC）。
 */
export function resolveLocalDateList(dates: string[], currentDate = '', now = new Date()): string[] {
  const list = dates.slice();
  if (currentDate && !list.includes(currentDate)) list.push(currentDate);
  if (!list.length) {
    const mm = String(now.getMonth() + 1).padStart(2, '0');
    const dd = String(now.getDate()).padStart(2, '0');
    list.push(`${now.getFullYear()}${mm}${dd}`);
  }
  list.sort();
  return list;
}

export function inferCheckinMode(r?: Partial<CheckinRule>): CheckinMode {
  if (r?.mode === 'human' || r?.mode === 'allDay' || r?.mode === 'window' || r?.mode === 'fixed') {
    return r.mode;
  }
  if (!r?.start && !r?.end) return 'allDay';
  if (!r?.end || r.end === r.start) return 'fixed';
  return 'window';
}

export function validateCheckinRules(rules: CheckinRule[]): string | null {
  const timeRe = /^(?:[01][0-9]|2[0-3]):[0-5][0-9]$/;
  for (const r of rules) {
    const mode = inferCheckinMode(r);
    if ((mode === 'fixed' || mode === 'window') && !timeRe.test(r.start)) {
      return '请填写有效的开始时间（HH:MM）';
    }
    if (mode === 'window' && (!timeRe.test(r.end) || r.end <= r.start)) {
      return '结束时间必须晚于开始时间，不支持跨零点';
    }
    if (r.offset < -120 || r.offset > 120) {
      return '微调范围为 -120 至 120 分钟';
    }
  }
  return null;
}

export function normalizeRouteMode(mode?: string): RouteMode {
  return String(mode || '').toLowerCase() === 'onexhausted' ? 'onExhausted' : 'always';
}

export function routeRuleKey(r: Pick<ModelRouteRule, 'fromSite' | 'match'>): string {
  return `${normalizeSite(r.fromSite)}|${String(r.match || '').toLowerCase().trim()}`;
}

export function validateModelRoutes(rules: ModelRouteRule[]): string | null {
  for (let i = 0; i < rules.length; i++) {
    const r = rules[i];
    const where = `第 ${i + 1} 条：`;
    if (!r.match.trim()) return `${where}源模型不能为空`;
    if (!r.target.trim()) return `${where}目标模型不能为空`;
    if (normalizeSite(r.fromSite) === normalizeSite(r.site) && r.match.trim() === r.target.trim()) {
      return `${where}源和目标完全一样，这条规则没有意义`;
    }
  }
  return null;
}

export function usageRowTotalTokens(row: UsageEntry): number {
  // usagejournal.Entry 没有 totalTokens 字段（只有 Summary / ModelStat /
  // ChartPoint 有），读它只会得到 undefined —— 那是死分支，不是兜底。
  const p = row.promptTokens || 0;
  const c = row.completionTokens || 0;
  return p + c;
}

export function usageRowHitRate(row: UsageEntry): number {
  const p = row.promptTokens || 0;
  if (p <= 0) return -1;
  return ((row.cachedTokens || 0) / p) * 100;
}

/** 剥掉客户端显式指定的站点前缀（cn: / global:），与后端 stripModelSitePrefix 同义。 */
export function stripModelSitePrefix(model?: string): string {
  return String(model || '')
    .trim()
    .replace(/^(cn:|global:)/i, '');
}

/**
 * 与后端 internal/gateway/usage_record.go 的 isAutoFamily 同判据：
 * auto / auto-*（先剥站点前缀、去空白）。写成 `startsWith('auto')` 会把
 * autopilot 之类也当成 auto —— 那些请求的真实替换就会被静默漏计。
 */
export function isAutoFamilyModel(model?: string): boolean {
  const m = stripModelSitePrefix(model).toLowerCase();
  return m === 'auto' || m.startsWith('auto-');
}

/**
 * 与后端 modelMismatch 的归一化同判据：小写 + 去掉 - _ . 空白。
 * `hy4flash-free` 与 `hy4-flash-free` 视为同一模型，但 deepseek 与 glm 不会混。
 */
export function normalizeModelName(model?: string): string {
  return stripModelSitePrefix(model)
    .toLowerCase()
    .replace(/[-_.\s]/g, '');
}

export function parseHHMMToMinutes(hhmm?: string): number | null {
  if (!hhmm) return null;
  const m = /^([01]\d|2[0-3]):([0-5]\d)$/.exec(hhmm.trim());
  if (!m) return null;
  return Number(m[1]) * 60 + Number(m[2]);
}
