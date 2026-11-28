import { describe, expect, it } from 'vitest';
import {
  computeExpirySummary,
  computeModelAvailability,
  isAutoFamilyModel,
  mergeModelLists,
  normalizeModelName,
  stripModelSitePrefix,
  resolveLocalDateList,
  spFormInfo,
  spResultInfo,
  summarizeConcurrency,
  validateCheckinRules,
  validateModelRoutes,
} from '@/lib/domain';
import { formatCompactNumber, safeHttpUrl } from '@/lib/format';
import type { AccountSummary, CreditPackage } from '@/types/api';

describe('summarizeConcurrency', () => {
  it('accepts /api/status numeric summary without throwing', () => {
    const out = summarizeConcurrency({
      generatedAt: 1,
      globalInFlight: 7,
      accounts: 5,
      rateLimits: 8,
    });
    expect(out.globalInFlight).toBe(7);
    expect(out.peakInFlight).toBe(0);
    expect(out.rateLimits).toBe(8);
  });

  it('accepts /api/concurrency per-account snapshot array', () => {
    const out = summarizeConcurrency({
      generatedAt: 2,
      globalInFlight: 4,
      rateLimits: [],
      accounts: [
        { accountId: 'a', inFlight: 3, rpm1: 12, rpm5: 8, rpm60: 5, peakInFlight: 13, rateLimits60: 2 },
        { accountId: 'b', inFlight: 1, rpm1: 4, rpm5: 2, rpm60: 1, peakInFlight: 3, rateLimits60: 1 },
      ],
    });
    expect(out.globalInFlight).toBe(4);
    expect(out.peakInFlight).toBe(13);
    expect(out.rateLimits).toBe(3);
    expect(out.byAccount['a'].rpm1).toBe(12);
  });
});

describe('computeExpirySummary', () => {
  it('sorts packages without mutating input and calculates 7-day expiring window', () => {
    const now = Date.UTC(2026, 9, 16);
    const day = 86_400_000;
    const packages: CreditPackage[] = [
      { packageName: 'later', remaining: 8, total: 10, expiresAt: now + 8 * day },
      { packageName: '<img src=x>', remaining: 3, total: 5, expiresAt: now + day },
      { packageName: 'expired', remaining: 90, total: 100, expiresAt: now - 1 },
      { packageName: 'unknown', remaining: null, total: null },
      { packageName: 'soon', remaining: 5, total: 5, expiresAt: now + 7 * day },
      { packageName: 'unlimited', remaining: -1, total: -1, expiresAt: now + day },
    ];
    const snapshotBefore = JSON.stringify(packages);
    const out = computeExpirySummary({ accountId: 'a1', credits: { packages }, quota: { checkedAt: now } }, now);

    expect(JSON.stringify(packages)).toBe(snapshotBefore);
    expect(out.expiringCount7d).toBe(2);
    expect(out.expiringAmount7d).toBe(8);
    expect(out.rows[0].packageName).toBe('expired');
    expect(out.rows[0].statusLabel).toBe('已过期');
    expect(out.rows.find((r) => r.packageName === 'unknown')?.statusLabel).toBe('到期未知');
    expect(out.rows.find((r) => r.packageName === 'unlimited')?.remainingText).toBe('不限量');
  });

  it('handles empty packages safely', () => {
    const out = computeExpirySummary({ accountId: 'a1', credits: { packages: [] } }, Date.now());
    expect(out.totalPackages).toBe(0);
    expect(out.rows).toEqual([]);
  });
});

describe('System Prompt result and form classification', () => {
  it('never marks 11128 / failure / normal fold as recovered success', () => {
    const blocked = spResultInfo({ at: 1, degraded: true, status: 400, errorCode: '11128', form: 'keep', mode: 'auto' });
    expect(blocked.label).toBe('被拦截 · 11128');
    expect(blocked.label).not.toContain('降级成功');

    const foldNormal = spResultInfo({ at: 2, form: 'fold', status: 200, mode: 'fold', recovered: false });
    expect(foldNormal.label).toBe('OK');

    const strayRecovered = spResultInfo({ at: 3, form: 'fold', status: 200, mode: 'fold', recovered: true });
    expect(strayRecovered.label).not.toContain('降级成功');

    const realRecovered = spResultInfo({ at: 4, form: 'fold', status: 200, mode: 'auto', recovered: true });
    expect(realRecovered.label).toBe('降级成功');

    const transportFail = spResultInfo({ at: 5, status: 0, degraded: true, errorCode: 'read: connection reset' });
    expect(transportFail.label).toBe('传输失败');
  });

  it('distinguishes replay and stripped forms accurately', () => {
    expect(spFormInfo({ at: 1, form: 'keep', degraded: true }).label).toBe('keep(被拦截)');
    expect(spFormInfo({ at: 1, form: 'keep', degraded: true, stripped: true }).label).toBe('keep(已剥离·被拦截)');
    expect(spFormInfo({ at: 1, form: 'fold', recovered: true }).label).toBe('fold(降级重放)');
    expect(spFormInfo({ at: 1, form: 'keep', stripped: true }).label).toBe('keep(已剥离)');
  });

  it('falls back to local date rather than UTC date when event dates are empty', () => {
    const local3am = new Date(2026, 0, 1, 3, 0, 0);
    const dates = resolveLocalDateList([], '', local3am);
    expect(dates).toEqual(['20260101']);
  });
});

describe('computeModelAvailability', () => {
  it('counts available, cooled, and below-floor accounts accurately', () => {
    const now = Date.UTC(2026, 10, 20, 10, 0, 0);
    const accounts: AccountSummary[] = [
      { id: 'cn-1', site: 'domestic', enabled: true, hasCredentials: true, quotaRemaining: 3, quotaCheckedAt: now },
      { id: 'gl-1', site: 'global', enabled: true, hasCredentials: true, modelCooldowns: { 'test-model': now + 600_000 } },
    ];
    const r1 = computeModelAvailability('test-model', ['domestic', 'global'], accounts, 18, now);
    expect(r1.text).toBe('可用 0 · 冷却 1 · 额度不足 1');

    accounts[0].quotaRemaining = 50;
    const r2 = computeModelAvailability('test-model', ['domestic', 'global'], accounts, 18, now);
    expect(r2.text).toBe('可用 1 · 冷却 1');
  });
});

describe('mergeModelLists and validators', () => {
  it('merges domestic and global catalogs with site tags', () => {
    const merged = mergeModelLists(
      { models: [{ modelId: 'codebuddy/claude-4', credits: '1x credits' }, 'deepseek-v3'] },
      { models: [{ modelId: 'claude-4' }, { modelId: 'gpt-5.6' }] },
    );
    expect(merged.map((m) => m.modelId)).toEqual(['claude-4', 'deepseek-v3', 'gpt-5.6']);
    expect(merged[0].sites).toEqual(['domestic', 'global']);
    expect(merged[1].sites).toEqual(['domestic']);
    expect(merged[2].sites).toEqual(['global']);
  });

  it('validates checkin rules and model routes', () => {
    expect(
      validateCheckinRules([
        { accountId: 'a', enabled: true, mode: 'window', start: '11:00', end: '09:00', offset: 0 },
      ]),
    ).toContain('结束时间必须晚于开始时间');
    expect(
      validateModelRoutes([
        { fromSite: 'domestic', match: 'gpt-5', site: 'domestic', target: 'gpt-5', mode: 'always', note: '' },
      ]),
    ).toContain('源和目标完全一样');
  });

  it('guards safeHttpUrl and formats compact numbers', () => {
    expect(safeHttpUrl('javascript:alert(1)', 'https://fallback.test')).toBe('https://fallback.test');
    expect(safeHttpUrl('https://www.codebuddy.cn/plan', 'https://fallback.test')).toBe('https://www.codebuddy.cn/plan');
    expect(formatCompactNumber(1_420_000)).toBe('1.42M');
    expect(formatCompactNumber(18_500)).toBe('18.5K');
  });
  it('model name normalization matches the backend exactly', () => {
    // 后端 normalizeModelName 去掉 - _ . 空格 / :
    expect(normalizeModelName('deepseek/v3')).toBe(normalizeModelName('deepseekv3'));
    expect(normalizeModelName('a:b')).toBe(normalizeModelName('ab'));
    expect(normalizeModelName('hy4flash-free')).toBe(normalizeModelName('hy4-flash-free'));
    // 站点前缀大小写敏感：CN:auto 不被剥离，因此不属于 auto 一族
    expect(stripModelSitePrefix('cn:auto')).toBe('auto');
    expect(stripModelSitePrefix('CN:auto')).toBe('CN:auto');
    expect(stripModelSitePrefix('cn:')).toBe('cn:');
    expect(isAutoFamilyModel('cn:auto')).toBe(true);
    expect(isAutoFamilyModel('CN:auto')).toBe(false);
    expect(isAutoFamilyModel('autopilot')).toBe(false);
    expect(isAutoFamilyModel('auto-max')).toBe(true);
  });

});
