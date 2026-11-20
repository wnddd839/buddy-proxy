import type { Product, Site } from '@/types/api';

export function normalizeSite(site?: string): Site {
  const s = String(site || '').toLowerCase().trim();
  if (s === 'domestic' || s === 'cn' || s === 'china' || s === 'internal') {
    return 'domestic';
  }
  return 'global';
}

export function siteLabel(site?: string): string {
  return normalizeSite(site) === 'domestic' ? '国内' : '国际';
}

export function normalizeProduct(product?: string): Product {
  const p = String(product || '').toLowerCase().trim();
  if (p === 'workbuddy' || p === 'wb' || p === 'ide') {
    return 'workbuddy';
  }
  return 'codebuddy';
}

export function productLabel(product?: string): string {
  return normalizeProduct(product) === 'workbuddy' ? 'WorkBuddy' : 'CodeBuddy';
}

export function bareModelId(raw?: string): string {
  const id = String(raw || '').trim();
  if (!id) return 'auto';
  return id.replace(/^codebuddy[/:]/i, '') || 'auto';
}

/** 只放行 http(s) 协议链接，避免 javascript: 等危险协议 */
export function safeHttpUrl(raw: unknown, fallback: string): string {
  const s = String(raw || '').trim();
  return /^https?:\/\//i.test(s) ? s : fallback;
}

export function proxyHost(url?: string): string {
  const s = String(url || '');
  const at = s.lastIndexOf('@');
  const rest = at >= 0 ? s.slice(at + 1) : s.replace(/^[a-z0-9+.-]+:\/\//i, '');
  return rest.replace(/\/.*$/, '');
}

export function shortId(id?: string): string {
  const s = String(id || '');
  if (s.length <= 10) return s || '—';
  return `${s.slice(0, 8)}…`;
}

export function formatTime(ms?: number): string {
  if (!ms) return '—';
  try {
    return new Date(ms).toLocaleString();
  } catch {
    return String(ms);
  }
}

export function formatShortClock(ms?: number): string {
  if (!ms) return '—';
  const d = new Date(ms);
  if (Number.isNaN(d.getTime())) return '—';
  const hh = String(d.getHours()).padStart(2, '0');
  const mm = String(d.getMinutes()).padStart(2, '0');
  const ss = String(d.getSeconds()).padStart(2, '0');
  return `${hh}:${mm}:${ss}`;
}

export function formatMonthDayTime(ms?: number): string {
  if (!ms) return '—';
  const d = new Date(ms);
  if (Number.isNaN(d.getTime())) return '—';
  const mo = d.getMonth() + 1;
  const da = d.getDate();
  const hh = String(d.getHours()).padStart(2, '0');
  const mm = String(d.getMinutes()).padStart(2, '0');
  return `${mo}/${da} ${hh}:${mm}`;
}

export function formatResetAt(ms?: number): string {
  if (!ms) return '-';
  try {
    return new Date(ms).toLocaleString();
  } catch {
    return String(ms);
  }
}

export function formatNextCheckin(ms?: number, nowMs = Date.now()): string {
  const n = Number(ms || 0);
  if (!n) return '';
  const at = new Date(n);
  if (Number.isNaN(at.getTime())) return '';
  const hh = String(at.getHours()).padStart(2, '0');
  const mm = String(at.getMinutes()).padStart(2, '0');
  const now = new Date(nowMs);
  const sameDay =
    at.getFullYear() === now.getFullYear() &&
    at.getMonth() === now.getMonth() &&
    at.getDate() === now.getDate();
  return sameDay ? `${hh}:${mm}` : `明天 ${hh}:${mm}`;
}

export function formatHitRate(rate?: number | null): string {
  if (rate == null || rate < 0) return '—';
  return `${rate.toFixed(1)}%`;
}

export function formatPercent(ratio?: number | null): string {
  if (ratio == null) return '—';
  return `${(Number(ratio) * 100).toFixed(1)}%`;
}

export function formatCompactNumber(value?: number | null): string {
  if (value == null || !Number.isFinite(value)) return '0';
  const abs = Math.abs(value);
  if (abs >= 1_000_000_000) return `${(value / 1_000_000_000).toFixed(2)}B`;
  if (abs >= 1_000_000) return `${(value / 1_000_000).toFixed(2)}M`;
  if (abs >= 10_000) return `${(value / 1_000).toFixed(1)}K`;
  return value.toLocaleString('en-US');
}

export function formatBytes(bytes?: number | null): string {
  if (!bytes || bytes <= 0) return '—';
  if (bytes >= 1024 * 1024) return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
  if (bytes >= 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${bytes} B`;
}
