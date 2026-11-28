import type { ModelRouteRule, TitleRoute } from '@/types/api';
import { normalizeRouteMode } from '@/lib/domain';
import { normalizeSite } from '@/lib/format';

/**
 * 模型页草稿的会话级持久化（sessionStorage）。
 *
 * 只解决一件事：切换侧边栏标签会让 ModelsPage 卸载，未保存的草稿不该随之消失。
 * 不做跨浏览器标签页持久化，也不把草稿写到服务端。
 *
 * 三条不变量（任何一条破了都会让旧草稿盖掉更新的服务端值）：
 *
 * 1. **只存脏草稿**：与写入时的服务端快照一致就立刻清空。否则下次挂载会把
 *    服务端早已生效的旧值当成「未保存草稿」恢复回来。
 * 2. **记录基线**：每条记录带 base = 写入时服务端快照的指纹。恢复前先和
 *    **当前**服务端载荷的指纹比：一致 ⇒ 草稿已过时（别人已保存同一份），丢弃。
 * 3. **载荷未就绪不动作**：服务端指纹为空时既不恢复也不写盘，避免拿空基线
 *    去覆盖一个还没读到的服务端值。
 *
 * 版本号进 key 也进信封：schema 一变，旧 key 自然失效，不必写迁移代码。
 */
export const DRAFTS_SCHEMA_VERSION = 1;
export const ROUTES_DRAFT_KEY = 'cbp.models.routesDraft.v1';
export const TITLE_DRAFT_KEY = 'cbp.models.titleDraft.v1';

/** sessionStorage 的最小接口：node 测试里用假实现即可，无需 jsdom。 */
export interface DraftStorageLike {
  getItem(key: string): string | null;
  setItem(key: string, value: string): void;
  removeItem(key: string): void;
}

export interface StoredDraft<T> {
  draft: T;
  /** 写入时服务端快照的指纹（JSON 字符串）。空串 = 基线未知。 */
  base: string;
  at: number;
}

export type DraftNormalizer<T> = (value: unknown) => T | null;

/**
 * 取 sessionStorage。隐私模式 / 配额满 / 被策略禁用时返回 null，
 * 页面退化为纯内存草稿，绝不能因为存不了而崩。
 */
export function getSessionStorage(): DraftStorageLike | null {
  try {
    if (typeof window === 'undefined') return null;
    const store = window.sessionStorage;
    if (!store) return null;
    const probe = '__cbp_draft_probe__';
    store.setItem(probe, '1');
    store.removeItem(probe);
    return store;
  } catch {
    return null;
  }
}

/**
 * 路由规则归一化。同时用于「服务端载荷」与「存量草稿」，两边指纹才可比。
 * 返回 null = 结构不对（损坏 / schema 漂移），调用方必须当没有草稿处理。
 */
export function normalizeRulesDraft(value: unknown): ModelRouteRule[] | null {
  if (!Array.isArray(value)) return null;
  const out: ModelRouteRule[] = [];
  for (const raw of value) {
    if (!raw || typeof raw !== 'object' || Array.isArray(raw)) return null;
    const r = raw as Record<string, unknown>;
    if (typeof r.match !== 'string' || typeof r.target !== 'string') return null;
    // 未知字段一律丢弃：存下来的旧 schema 字段不会污染页面状态。
    out.push({
      fromSite: normalizeSite(typeof r.fromSite === 'string' ? r.fromSite : ''),
      match: r.match,
      site: normalizeSite(typeof r.site === 'string' ? r.site : ''),
      target: r.target,
      mode: normalizeRouteMode(typeof r.mode === 'string' ? r.mode : ''),
      note: typeof r.note === 'string' ? r.note : '',
    });
  }
  return out;
}

/** 服务端 titleRoute → 页面使用的三元组。所有比较都走这一个形状。 */
export function titleFromPayload(route?: TitleRoute | null): TitleRoute {
  return {
    enabled: route?.enabled === true,
    domestic: route?.domestic || '',
    global: route?.global || '',
  };
}

/** 标题草稿归一化；返回 null = 结构不对，当没有草稿处理。 */
export function normalizeTitleDraft(value: unknown): TitleRoute | null {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return null;
  const t = value as Record<string, unknown>;
  if (t.enabled !== undefined && typeof t.enabled !== 'boolean') return null;
  for (const field of ['domestic', 'global'] as const) {
    if (t[field] !== undefined && typeof t[field] !== 'string') return null;
  }
  return titleFromPayload({
    enabled: t.enabled === true,
    domestic: typeof t.domestic === 'string' ? t.domestic : '',
    global: typeof t.global === 'string' ? t.global : '',
  });
}

/** 指纹必须是稳定字符串：数组顺序即优先级，不能排序。 */
export function rulesFingerprint(rules: ModelRouteRule[]): string {
  return JSON.stringify(rules);
}

export function titleFingerprint(route: TitleRoute): string {
  return JSON.stringify(titleFromPayload(route));
}

/**
 * 解析会话里的信封。任何一处不符（JSON 坏、版本不符、draft 结构不对）
 * 都返回 null —— 由 readDraft 负责删除，绝不把半个对象喂给页面。
 */
export function parseStoredDraft<T>(
  raw: string | null,
  normalize: DraftNormalizer<T>,
): StoredDraft<T> | null {
  if (!raw) return null;
  let parsed: unknown;
  try {
    parsed = JSON.parse(raw);
  } catch {
    return null;
  }
  if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) return null;
  const env = parsed as Record<string, unknown>;
  if (env.v !== DRAFTS_SCHEMA_VERSION) return null;
  if (typeof env.base !== 'string') return null;
  const draft = normalize(env.draft);
  if (draft === null) return null;
  const at = typeof env.at === 'number' && Number.isFinite(env.at) ? env.at : 0;
  return { draft, base: env.base, at };
}

/** 读草稿；损坏的条目就地删除（"忽略且移除"，不是留着反复踩）。 */
export function readDraft<T>(
  storage: DraftStorageLike | null,
  key: string,
  normalize: DraftNormalizer<T>,
): StoredDraft<T> | null {
  if (!storage) return null;
  let raw: string | null = null;
  try {
    raw = storage.getItem(key);
  } catch {
    return null;
  }
  if (raw == null) return null;
  const record = parseStoredDraft(raw, normalize);
  if (!record) {
    try {
      storage.removeItem(key);
    } catch {
      // 删不掉也只能算了：下一次读取还会走到这里，不会把坏数据当草稿用。
    }
  }
  return record;
}

/** 写草稿。写失败（配额满）返回 false，页面不应因此报错。 */
export function writeDraft<T>(
  storage: DraftStorageLike | null,
  key: string,
  draft: T,
  base: string,
  at: number = Date.now(),
): boolean {
  if (!storage) return false;
  try {
    storage.setItem(key, JSON.stringify({ v: DRAFTS_SCHEMA_VERSION, base, draft, at }));
    return true;
  } catch {
    return false;
  }
}

export function clearDraft(storage: DraftStorageLike | null, key: string): void {
  if (!storage) return;
  try {
    storage.removeItem(key);
  } catch {
    // 同上：清不掉不影响正确性，下次恢复前还会再比一次服务端指纹。
  }
}

/**
 * 是否用会话草稿覆盖（恢复）当前页面值。
 *
 * - 没有草稿 → 不恢复。
 * - 服务端指纹为空（载荷还没加载）→ 不恢复：没有基线时任何恢复都是瞎猜，
 *   宁可让草稿留在 storage 里等下一次挂载。
 * - 草稿与**当前**服务端一致 → 不恢复并清空：这份改动早已生效，恢复只会
 *   平白标脏，甚至把之后的服务端改动当成「用户草稿」再存一遍。
 * - 草稿与写入基线一致 → 不恢复：当时就没改（旧版本留下的干净条目）。
 * - 其余（确实与当前服务端不同）→ 恢复。页面因此必然显示未保存标记，
 *   由用户显式点保存，不存在"静默覆盖"。
 */
export function shouldRestoreDraft<T>(
  stored: StoredDraft<T> | null,
  serverFingerprint: string,
  fingerprint: (draft: T) => string,
): boolean {
  if (!stored) return false;
  if (!serverFingerprint) return false;
  const fp = fingerprint(stored.draft);
  if (fp === serverFingerprint) return false;
  if (stored.base && fp === stored.base) return false;
  return true;
}
