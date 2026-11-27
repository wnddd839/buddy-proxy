import { useEffect, useMemo, useState } from 'react';
import { Clock, FileText, RefreshCw, Sparkles } from 'lucide-react';
import type {
  AccountSummary,
  CheckinConfigPayload,
  CheckinLogEntry,
  CheckinMode,
  CheckinRule,
} from '@/types/api';
import {
  inferCheckinMode,
  parseHHMMToMinutes,
  validateCheckinRules,
} from '@/lib/domain';
import {
  formatMonthDayTime,
  formatNextCheckin,
  normalizeSite,
  proxyHost,
  siteLabel,
} from '@/lib/format';
import { apiRequest } from '@/lib/api';
import { useToast } from '@/hooks/useToast';
import { Button } from '@/components/ui/Button';
import { Card, EmptyState, PageHeader, StatCard } from '@/components/ui/Card';
import { DirtySaveBar } from '@/components/ui/DirtySaveBar';
import { SegmentedControl } from '@/components/ui/SegmentedControl';
import { SlideDrawer } from '@/components/ui/SlideDrawer';
import { StatusBadge } from '@/components/ui/StatusBadge';

interface CheckinPageProps {
  accounts: AccountSummary[];
  config: CheckinConfigPayload | null;
  loadError: string;
  onConfigSaved: (next: CheckinConfigPayload) => void;
}

type SiteFilter = '' | 'domestic' | 'global';

interface DraftConfig {
  auto: boolean;
  allowDirectFallback: boolean;
  checkinRetryMin: number;
  checkinRetryMax: number;
  rules: CheckinRule[];
}

function normalizeRulesForAccounts(
  existingRules: CheckinRule[],
  accounts: AccountSummary[],
): CheckinRule[] {
  const byId = new Map<string, CheckinRule>();
  for (const r of existingRules) byId.set(r.accountId, r);

  const accountIds = new Set(accounts.map((a) => a.id));
  const merged: CheckinRule[] = accounts.map((a) => {
    const r = byId.get(a.id);
    if (!r) {
      return { accountId: a.id, enabled: false, mode: 'human', start: '', end: '', offset: 0 };
    }
    return {
      accountId: a.id,
      enabled: !!r.enabled,
      mode: inferCheckinMode(r),
      start: r.start || '',
      end: r.end || '',
      offset: Number(r.offset || 0),
    };
  });

  // 保留不在当前列表但原配置中存在的规则，避免意外丢失
  for (const r of existingRules) {
    if (!accountIds.has(r.accountId)) merged.push(r);
  }
  return merged;
}

export function CheckinPage({
  accounts,
  config,
  loadError,
  onConfigSaved,
}: CheckinPageProps) {
  const { showToast } = useToast();
  const [siteFilter, setSiteFilter] = useState<SiteFilter>('');
  const [draft, setDraft] = useState<DraftConfig | null>(null);
  const [savedSnapshot, setSavedSnapshot] = useState('');
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState('');
  const [justSaved, setJustSaved] = useState(false);
  const [presetValue, setPresetValue] = useState('');

  // 48h 审计日志抽屉
  const [logOpen, setLogOpen] = useState(false);
  const [logLoading, setLogLoading] = useState(false);
  const [logEntries, setLogEntries] = useState<CheckinLogEntry[]>([]);

  // 仅在首次加载或外部 config 变更且当前无未保存草稿时初始化草稿
  useEffect(() => {
    if (!config) return;
    const normalized: DraftConfig = {
      auto: !!config.auto,
      allowDirectFallback: !!config.allowDirectFallback,
      checkinRetryMin: config.checkinRetryMin ?? 10,
      checkinRetryMax: config.checkinRetryMax ?? 1,
      rules: normalizeRulesForAccounts(config.rules || [], accounts),
    };
    const snap = JSON.stringify(normalized);
    setDraft((prev) => {
      if (!prev) {
        setSavedSnapshot(snap);
        return normalized;
      }
      // 如果账号列表变化，把新增账号补进草稿，但不覆盖用户正在编辑的规则
      const nextRules = normalizeRulesForAccounts(prev.rules, accounts);
      return { ...prev, rules: nextRules };
    });
  }, [config, accounts]);

  const isDirty = useMemo(() => {
    if (!draft || !savedSnapshot) return false;
    return JSON.stringify(draft) !== savedSnapshot;
  }, [draft, savedSnapshot]);

  const siteById = useMemo(() => {
    const m: Record<string, string> = {};
    for (const a of accounts) m[a.id] = normalizeSite(a.site);
    return m;
  }, [accounts]);

  // 当前站点过滤下的可见账号（列表与统计真正保持一致！）
  const visibleAccounts = useMemo(() => {
    if (!siteFilter) return accounts;
    return accounts.filter((a) => normalizeSite(a.site) === siteFilter);
  }, [accounts, siteFilter]);

  // 统计数字
  const summary = useMemo(() => {
    let done = 0;
    let none = 0;
    let failed = 0;
    for (const a of visibleAccounts) {
      if (!a.enabled) continue;
      const st = a.checkinToday || 'none';
      if (st === 'done' || st === 'already') done++;
      else if (st === 'failed') failed++;
      else none++;
    }
    const enabledRules = (draft?.rules || []).filter((r) => {
      if (!r.enabled) return false;
      if (!siteFilter) return true;
      return siteById[r.accountId] === siteFilter;
    }).length;
    return { done, none, failed, enabledRules };
  }, [visibleAccounts, draft, siteFilter, siteById]);

  // 24h 时间轴分布点（优先用服务端 checkinNextAt，其次用草稿固定/窗口中心时刻）
  const timelinePoints = useMemo(() => {
    const ruleById = new Map<string, CheckinRule>();
    for (const r of draft?.rules || []) ruleById.set(r.accountId, r);

    const points: Array<{
      id: string;
      label: string;
      minute: number;
      hhmm: string;
      status: 'done' | 'failed' | 'pending';
      detail: string;
    }> = [];

    for (const a of visibleAccounts) {
      const r = ruleById.get(a.id);
      const st = a.checkinToday || 'none';
      const status: 'done' | 'failed' | 'pending' =
        st === 'done' || st === 'already' ? 'done' : st === 'failed' ? 'failed' : 'pending';

      let minute: number | null = null;
      let detail = '';

      if (a.checkinNextAt) {
        const d = new Date(a.checkinNextAt);
        if (!Number.isNaN(d.getTime())) {
          minute = d.getHours() * 60 + d.getMinutes();
          detail = `下次 ${formatNextCheckin(a.checkinNextAt)}`;
        }
      }
      if (minute == null && r?.enabled) {
        const mode = inferCheckinMode(r);
        if (mode === 'fixed') {
          const m = parseHHMMToMinutes(r.start);
          if (m != null) {
            minute = Math.max(0, Math.min(1439, m + (r.offset || 0)));
            detail = `固定 ${r.start}`;
          }
        } else if (mode === 'window') {
          const s = parseHHMMToMinutes(r.start);
          const e = parseHHMMToMinutes(r.end);
          if (s != null && e != null && e > s) {
            minute = Math.round((s + e) / 2);
            detail = `窗口 ${r.start}–${r.end}`;
          }
        }
      }

      if (minute != null) {
        const hh = String(Math.floor(minute / 60)).padStart(2, '0');
        const mm = String(minute % 60).padStart(2, '0');
        points.push({
          id: a.id,
          label: a.label || a.id,
          minute,
          hhmm: `${hh}:${mm}`,
          status,
          detail,
        });
      }
    }
    return points.sort((x, y) => x.minute - y.minute);
  }, [visibleAccounts, draft]);

  const updateRule = (accountId: string, patch: Partial<CheckinRule>) => {
    setJustSaved(false);
    setSaveError('');
    setDraft((prev) => {
      if (!prev) return prev;
      return {
        ...prev,
        rules: prev.rules.map((r) => (r.accountId === accountId ? { ...r, ...patch } : r)),
      };
    });
  };

  const setAllVisibleEnabled = (enabled: boolean) => {
    const targetIds = new Set(visibleAccounts.map((a) => a.id));
    setJustSaved(false);
    setDraft((prev) => {
      if (!prev) return prev;
      return {
        ...prev,
        rules: prev.rules.map((r) => (targetIds.has(r.accountId) ? { ...r, enabled } : r)),
      };
    });
  };

  const applyPreset = (v: string) => {
    setPresetValue('');
    if (!v || !draft) return;
    const targetIds = visibleAccounts.map((a) => a.id);
    if (!targetIds.length) {
      showToast('当前范围没有账号', 'error');
      return;
    }
    const targetSet = new Set(targetIds);
    const scopeLabel = siteFilter ? siteLabel(siteFilter) : '全部';

    if (v === 'spread') {
      const winStart = 8 * 60;
      const winEnd = 23 * 60;
      const span = winEnd - winStart;
      const slot = Math.max(1, Math.floor(span / targetIds.length));
      const fixedMap = new Map<string, string>();
      targetIds.forEach((id, idx) => {
        const base = winStart + Math.floor((idx * span) / targetIds.length);
        const jitter = Math.floor(Math.random() * Math.max(1, Math.floor(slot / 3)));
        const minute = Math.min(winEnd - 1, base + jitter);
        const hh = String(Math.floor(minute / 60)).padStart(2, '0');
        const mm = String(minute % 60).padStart(2, '0');
        fixedMap.set(id, `${hh}:${mm}`);
      });
      setDraft({
        ...draft,
        rules: draft.rules.map((r) =>
          targetSet.has(r.accountId)
            ? { ...r, mode: 'fixed', start: fixedMap.get(r.accountId) || '09:00', end: '' }
            : r,
        ),
      });
      setJustSaved(false);
      showToast(
        `已把 ${scopeLabel} ${targetIds.length} 个账号错开到 08:00–23:00（点击保存生效）`,
      );
      return;
    }

    const presetSpecs: Record<string, { mode: CheckinMode; start: string; end: string; label: string }> = {
      human: { mode: 'human', start: '', end: '', label: '仿人随机' },
      allday: { mode: 'allDay', start: '', end: '', label: '全天随机' },
      '0900': { mode: 'fixed', start: '09:00', end: '', label: '每天 09:00' },
      '1200': { mode: 'fixed', start: '12:00', end: '', label: '每天 12:00' },
      '2000': { mode: 'fixed', start: '20:00', end: '', label: '每天 20:00' },
      '0900-1100': { mode: 'window', start: '09:00', end: '11:00', label: '09:00–11:00' },
      '2000-2300': { mode: 'window', start: '20:00', end: '23:00', label: '20:00–23:00' },
    };
    const spec = presetSpecs[v];
    if (!spec) return;

    setDraft({
      ...draft,
      rules: draft.rules.map((r) =>
        targetSet.has(r.accountId)
          ? { ...r, mode: spec.mode, start: spec.start, end: spec.end }
          : r,
      ),
    });
    setJustSaved(false);
    showToast(`已把 ${scopeLabel} ${targetIds.length} 个账号设为「${spec.label}」（记得保存）`);
  };

  const handleSave = async () => {
    if (!draft || saving) return;
    const err = validateCheckinRules(draft.rules);
    if (err) {
      setSaveError(err);
      showToast(err, 'error');
      return;
    }
    setSaving(true);
    setSaveError('');
    try {
      await apiRequest('/direct-admin/api/checkin-config', {
        method: 'POST',
        body: JSON.stringify(draft),
      });
      const nextSnap = JSON.stringify(draft);
      setSavedSnapshot(nextSnap);
      setJustSaved(true);
      onConfigSaved({ ...draft, ok: true });
      showToast('自动签到配置已保存并生效', 'success');
    } catch (e: unknown) {
      const msg = e instanceof Error ? e.message : String(e);
      setSaveError(`保存失败，草稿已保留：${msg}`);
      showToast(msg, 'error');
    } finally {
      setSaving(false);
    }
  };

  const handleReset = () => {
    if (!savedSnapshot) return;
    setDraft(JSON.parse(savedSnapshot) as DraftConfig);
    setSaveError('');
    setJustSaved(false);
  };

  const openLogDrawer = async () => {
    setLogOpen(true);
    setLogLoading(true);
    try {
      const data = await apiRequest<{ entries?: CheckinLogEntry[] }>('/direct-admin/api/checkin-log');
      const sorted = (data.entries || [])
        .slice()
        .sort((x, y) => (y.ranAt || y.at || 0) - (x.ranAt || x.at || 0));
      setLogEntries(sorted);
    } catch (e: unknown) {
      showToast(e instanceof Error ? e.message : String(e), 'error');
    } finally {
      setLogLoading(false);
    }
  };

  const nameById = useMemo(() => {
    const m: Record<string, string> = {};
    for (const a of accounts) m[a.id] = a.label || a.id;
    return m;
  }, [accounts]);

  const ruleMap = useMemo(() => {
    const m = new Map<string, CheckinRule>();
    for (const r of draft?.rules || []) m.set(r.accountId, r);
    return m;
  }, [draft]);

  const totalEnabledDraft = (draft?.rules || []).filter((r) => r.enabled).length;
  const saveStateCode = loadError || saveError ? 'error' : saving ? 'saving' : isDirty ? 'dirty' : 'saved';
  const saveStateText = loadError
    ? `读取失败，请刷新重试：${loadError}`
    : saveError
      ? saveError
      : saving
        ? '正在保存签到配置…'
        : isDirty
          ? '有未保存的签到配置'
          : justSaved
            ? '签到配置已保存并生效'
            : draft
              ? '签到配置已保存'
              : '配置加载中…';

  return (
    <div id="tab-checkin" className="ds-page-enter space-y-5">
      <PageHeader
        title="自动签到与错峰调度"
        subtitle="统一按北京时间执行；启动不补签，错过等次日。范围过滤同步作用于统计、时间轴与下方列表，保存时始终持久化两站全部规则。"
        actions={
          <>
            <span
              id="checkinSaveState"
              role="status"
              aria-live="polite"
              data-state={saveStateCode}
              className={`ds-mono text-xs ${
                saveStateCode === 'error'
                  ? 'text-[var(--err)]'
                  : saveStateCode === 'dirty'
                    ? 'text-[var(--warn)] font-medium'
                    : 'text-[var(--fg-3)]'
              }`}
            >
              {saveStateText}
            </span>
            <Button id="btnCheckinLog" variant="ghost" size="sm" onClick={openLogDrawer}>
              <FileText className="w-3.5 h-3.5" />
              48h 审计日志
            </Button>
            <Button
              id="btnCheckinSave"
              variant="primary"
              size="sm"
              loading={saving}
              disabled={!draft}
              onClick={handleSave}
            >
              保存签到配置
            </Button>
          </>
        }
      />

      {/* 范围切换 + 4 格统计 */}
      <div className="flex items-center justify-between flex-wrap gap-3">
        <SegmentedControl<SiteFilter>
          id="checkinSiteSeg"
          ariaLabel="签到统计范围"
          value={siteFilter}
          onChange={setSiteFilter}
          options={[
            { value: '', label: '全部站点', dataAttrs: { 'data-cksite': '' } },
            { value: 'domestic', label: '国内站', dataAttrs: { 'data-cksite': 'domestic' } },
            { value: 'global', label: '国际站', dataAttrs: { 'data-cksite': 'global' } },
          ]}
        />
        {draft?.auto && totalEnabledDraft === 0 && (
          <StatusBadge tone="warn">
            ⚠ 总开关已开启但尚未勾选任何账号，当前不会自动签到
          </StatusBadge>
        )}
      </div>

      <div id="checkinMetrics" className="grid grid-cols-2 lg:grid-cols-4 gap-3.5">
        <StatCard
          label="今日已签到"
          valueId="ckDone"
          value={summary.done}
          badge={<StatusBadge tone="ok">完成</StatusBadge>}
        />
        <StatCard label="今日未签到" valueId="ckNone" value={summary.none} />
        <StatCard
          label="今日签到失败"
          valueId="ckFailed"
          value={summary.failed}
          valueClassName={summary.failed > 0 ? 'text-[var(--err)]' : ''}
          badge={summary.failed > 0 ? <StatusBadge tone="err">需排查</StatusBadge> : undefined}
        />
        <StatCard
          label="已启用规则（草稿）"
          valueId="ckRules"
          value={summary.enabledRules}
          badge={<StatusBadge tone="info">排程中</StatusBadge>}
        />
      </div>

      {/* 24 小时签到时刻分布可视化条 */}
      <Card className="p-5">
        <div className="flex flex-wrap items-center justify-between gap-2 mb-3">
          <div>
            <div className="text-sm font-medium text-[var(--fg)] flex items-center gap-2">
              <Clock className="w-4 h-4 text-[var(--info)]" />
              24 小时错峰分布预览（北京时间）
            </div>
            <div className="text-xs text-[var(--fg-3)] mt-0.5">
              展示已有预定时刻或固定/窗口排程的账号分布，悬停查看账号详情，一眼避免整点扎堆。
            </div>
          </div>
          <span className="text-[11px] ds-mono text-[var(--fg-3)]">
            已映射 {timelinePoints.length} 个时刻点
          </span>
        </div>

        <div className="relative pt-6 pb-2 px-1">
          <div className="h-2.5 w-full rounded-full bg-[var(--surface-2)] relative border border-[var(--border)]">
            {/* 08:00 - 23:00 建议活跃区 */}
            <div
              className="absolute top-0 bottom-0 rounded-full"
              style={{
                left: '33.33%',
                width: '62.5%',
                background: 'var(--info-bg)',
              }}
            />
            {timelinePoints.map((pt) => {
              const pct = (pt.minute / 1440) * 100;
              const dotBg =
                pt.status === 'done'
                  ? 'var(--ok)'
                  : pt.status === 'failed'
                    ? 'var(--err)'
                    : 'var(--info)';
              return (
                <div
                  key={pt.id}
                  className="group absolute top-1/2 -translate-y-1/2 -translate-x-1/2 cursor-pointer"
                  style={{ left: `${pct}%` }}
                >
                  <div
                    className="w-3.5 h-3.5 rounded-full border-2 border-[var(--surface)] transition-transform group-hover:scale-125"
                    style={{ background: dotBg }}
                  />
                  <div className="hidden group-hover:block absolute bottom-5 left-1/2 -translate-x-1/2 whitespace-nowrap px-2.5 py-1 text-[11px] ds-mono rounded-[var(--radius-sm)] bg-[var(--surface)] border border-[var(--border-strong)] shadow-[var(--shadow-lg)] text-[var(--fg)] z-20">
                    {pt.label} · {pt.hhmm} ({pt.detail})
                  </div>
                </div>
              );
            })}
          </div>
          <div className="flex justify-between text-[10px] ds-mono text-[var(--fg-3)] mt-2">
            <span>00:00</span>
            <span>06:00</span>
            <span>12:00</span>
            <span>18:00</span>
            <span>24:00</span>
          </div>
        </div>
      </Card>

      {/* 全局策略 3 卡片 */}
      <div className="grid grid-cols-1 md:grid-cols-3 gap-3.5">
        <Card className="p-4 flex items-center justify-between gap-3">
          <div>
            <div className="text-xs font-medium text-[var(--fg)]">自动签到总开关</div>
            <div className="text-[11px] text-[var(--fg-3)] mt-0.5">保存后立即生效，无需重启</div>
          </div>
          <select
            id="checkinAuto"
            aria-label="自动签到总开关"
            disabled={!draft || saving}
            value={draft?.auto ? 'on' : 'off'}
            onChange={(e) => {
              setJustSaved(false);
              setDraft((p) => (p ? { ...p, auto: e.target.value === 'on' } : p));
            }}
            className="ds-select px-2.5 py-1.5 text-xs"
          >
            <option value="off">关闭</option>
            <option value="on">开启</option>
          </select>
        </Card>

        <Card className="p-4 flex items-center justify-between gap-3">
          <div>
            <div className="text-xs font-medium text-[var(--fg)]">抖动失败重试</div>
            <div className="text-[11px] text-[var(--fg-3)] mt-0.5">仅重试出口抖动与 5xx</div>
          </div>
          <div className="flex items-center gap-1.5">
            <select
              id="checkinRetryMin"
              aria-label="签到失败后多久重试"
              disabled={!draft || saving}
              value={String(draft?.checkinRetryMin ?? 10)}
              onChange={(e) => {
                setJustSaved(false);
                setDraft((p) => (p ? { ...p, checkinRetryMin: Number(e.target.value) } : p));
              }}
              className="ds-select px-2 py-1.5 text-xs"
            >
              <option value="0">不重试</option>
              <option value="5">5 分钟</option>
              <option value="10">10 分钟</option>
              <option value="30">30 分钟</option>
              <option value="60">60 分钟</option>
            </select>
            <select
              id="checkinRetryMax"
              aria-label="签到失败最多重试几次"
              disabled={!draft || saving}
              value={String(draft?.checkinRetryMax ?? 1)}
              onChange={(e) => {
                setJustSaved(false);
                setDraft((p) => (p ? { ...p, checkinRetryMax: Number(e.target.value) } : p));
              }}
              className="ds-select px-2 py-1.5 text-xs"
            >
              <option value="1">最多 1 次</option>
              <option value="2">最多 2 次</option>
            </select>
          </div>
        </Card>

        <Card className="p-4 flex items-center justify-between gap-3">
          <div>
            <div className="text-xs font-medium text-[var(--fg)]">出口失败回落直连</div>
            <div className="text-[11px] text-[var(--fg-3)] mt-0.5">仅代理连不上时直连重试</div>
          </div>
          <select
            id="checkinFallback"
            aria-label="签到出口失败时是否回落直连"
            disabled={!draft || saving}
            value={draft?.allowDirectFallback ? 'on' : 'off'}
            onChange={(e) => {
              setJustSaved(false);
              setDraft((p) => (p ? { ...p, allowDirectFallback: e.target.value === 'on' } : p));
            }}
            className="ds-select px-2.5 py-1.5 text-xs"
          >
            <option value="off">关闭</option>
            <option value="on">开启</option>
          </select>
        </Card>
      </div>

      {/* 逐账号规则列表 */}
      <Card className="overflow-hidden">
        <div className="px-4 py-3 border-b border-[var(--border)] flex flex-wrap items-center justify-between gap-2.5 bg-[var(--surface-2)]/50">
          <div className="text-sm font-medium text-[var(--fg)] flex items-center gap-2">
            <Sparkles className="w-4 h-4 text-[var(--fg-2)]" />
            逐账号排程规则（{visibleAccounts.length}）
          </div>
          <div className="flex items-center gap-2 flex-wrap">
            <Button
              id="btnCheckinAll"
              variant="ghost"
              size="xs"
              disabled={!draft || saving}
              onClick={() => setAllVisibleEnabled(true)}
            >
              全选
            </Button>
            <Button
              id="btnCheckinNone"
              variant="ghost"
              size="xs"
              disabled={!draft || saving}
              onClick={() => setAllVisibleEnabled(false)}
            >
              全不选
            </Button>
            <select
              id="checkinPreset"
              aria-label="批量设置签到时间"
              disabled={!draft || saving}
              value={presetValue}
              onChange={(e) => applyPreset(e.target.value)}
              className="ds-select px-2.5 py-1 text-xs"
            >
              <option value="">一键套用节奏预设…</option>
              <option value="human">仿人随机（推荐）</option>
              <option value="allday">全天随机</option>
              <option value="0900">每天 09:00</option>
              <option value="1200">每天 12:00</option>
              <option value="2000">每天 20:00</option>
              <option value="0900-1100">09:00–11:00 随机</option>
              <option value="2000-2300">20:00–23:00 随机</option>
              <option value="spread">整批错开（08:00–23:00 固定时刻）</option>
            </select>
          </div>
        </div>

        <div id="checkinRules" className="divide-y divide-[var(--border)]">
          {!visibleAccounts.length ? (
            <div className="p-6">
              <EmptyState
                title={accounts.length ? '当前站点过滤下没有账号' : '账号池还没有账号'}
                description="先去「账号池与授权」添加账号，即可在此配置每账号独立的自动签到排程。"
              />
            </div>
          ) : (
            visibleAccounts.map((a) => {
              const r = ruleMap.get(a.id) || {
                accountId: a.id,
                enabled: false,
                mode: 'human',
                start: '',
                end: '',
                offset: 0,
              };
              const mode = inferCheckinMode(r);
              const showStart = mode === 'fixed' || mode === 'window';
              const showEnd = mode === 'window';

              return (
                <div
                  key={a.id}
                  data-id={a.id}
                  className="checkin-rule px-4 py-3 flex flex-wrap items-center justify-between gap-3 text-xs hover:bg-[var(--surface-2)]/40 transition-colors"
                >
                  <label className="inline-flex items-center gap-2.5 min-w-[200px] cursor-pointer">
                    <input
                      type="checkbox"
                      data-f="enabled"
                      disabled={saving}
                      checked={!!r.enabled}
                      onChange={(e) => updateRule(a.id, { enabled: e.target.checked })}
                      className="w-4 h-4 accent-[var(--accent)] cursor-pointer"
                    />
                    <span className="font-medium text-[var(--fg)]">{a.label || a.id}</span>
                    <StatusBadge tone="neutral">{siteLabel(a.site)}</StatusBadge>
                    {!a.enabled && <StatusBadge tone="neutral">已禁用</StatusBadge>}
                  </label>

                  <div className="flex items-center gap-2.5 flex-wrap">
                    <label className="inline-flex items-center gap-1.5 text-[var(--fg-2)]">
                      <span>模式</span>
                      <select
                        data-f="mode"
                        disabled={saving}
                        value={mode}
                        onChange={(e) => updateRule(a.id, { mode: e.target.value as CheckinMode })}
                        className="ds-select px-2 py-1 text-xs"
                      >
                        <option value="human">仿人随机</option>
                        <option value="allDay">全天随机</option>
                        <option value="window">时段窗口</option>
                        <option value="fixed">固定时刻</option>
                      </select>
                    </label>

                    {showStart && (
                      <label className="inline-flex items-center gap-1.5 text-[var(--fg-2)]">
                        <span>开始</span>
                        <input
                          type="text"
                          data-f="start"
                          disabled={saving}
                          placeholder="09:00"
                          value={r.start}
                          onChange={(e) => updateRule(a.id, { start: e.target.value })}
                          className="ds-input ds-mono w-20 px-2 py-1 text-xs"
                        />
                      </label>
                    )}

                    {showEnd && (
                      <label className="inline-flex items-center gap-1.5 text-[var(--fg-2)]">
                        <span>结束</span>
                        <input
                          type="text"
                          data-f="end"
                          disabled={saving}
                          placeholder="11:00"
                          value={r.end}
                          onChange={(e) => updateRule(a.id, { end: e.target.value })}
                          className="ds-input ds-mono w-20 px-2 py-1 text-xs"
                        />
                      </label>
                    )}

                    <label
                      title="在最终时刻上 ± 分钟（-120 至 120），用于错开同一批账号"
                      className="inline-flex items-center gap-1.5 text-[var(--fg-2)]"
                    >
                      <span>微调</span>
                      <input
                        type="number"
                        data-f="offset"
                        disabled={saving}
                        min={-120}
                        max={120}
                        step={5}
                        value={r.offset}
                        onChange={(e) =>
                          updateRule(a.id, { offset: parseInt(e.target.value, 10) || 0 })
                        }
                        className="ds-input ds-mono w-16 px-2 py-1 text-xs"
                      />
                    </label>
                  </div>
                </div>
              );
            })
          )}
        </div>
      </Card>

      <DirtySaveBar
        visible={isDirty}
        saving={saving}
        message="自动签到配置有未保存的更改"
        subMessage="保存将同时持久化国内与国际两站规则并立即生效"
        onReset={handleReset}
        onSave={handleSave}
      />

      {/* 右侧抽屉：48h 签到审计日志 */}
      <SlideDrawer
        open={logOpen}
        onClose={() => setLogOpen(false)}
        title="48 小时签到审计日志"
        subtitle="记录最近 48 小时内每一次自动或手动签到尝试的结果、积分与实际出口。"
        widthClass="max-w-3xl"
      >
        <div className="space-y-3">
          <div className="flex justify-end">
            <Button variant="ghost" size="xs" loading={logLoading} onClick={openLogDrawer}>
              <RefreshCw className="w-3 h-3" />
              刷新日志
            </Button>
          </div>
          <div id="checkinLogWrap" className="ds-surface overflow-x-auto">
            <table className="w-full text-left border-collapse text-xs" aria-label="签到明细">
              <thead>
                <tr className="border-b border-[var(--border)] bg-[var(--surface-2)] text-[11px] uppercase tracking-wider text-[var(--fg-3)]">
                  <th className="py-2 px-3 font-medium">时间</th>
                  <th className="py-2 px-3 font-medium">账号</th>
                  <th className="py-2 px-3 font-medium">结果</th>
                  <th className="py-2 px-3 font-medium">积分</th>
                  <th className="py-2 px-3 font-medium">出口</th>
                  <th className="py-2 px-3 font-medium">说明</th>
                </tr>
              </thead>
              <tbody id="checkinLogRows" className="divide-y divide-[var(--border)]">
                {logLoading ? (
                  <tr>
                    <td colSpan={6} className="py-8 text-center text-[var(--fg-3)]">
                      正在加载审计日志…
                    </td>
                  </tr>
                ) : !logEntries.length ? (
                  <tr>
                    <td colSpan={6} className="py-8 text-center text-[var(--fg-3)]">
                      最近 48 小时还没有签到记录。
                    </td>
                  </tr>
                ) : (
                  logEntries.map((e, idx) => (
                    <tr key={idx}>
                      <td className="py-2 px-3 ds-mono text-[var(--fg-2)] whitespace-nowrap">
                        {formatMonthDayTime(e.ranAt || e.at)}
                      </td>
                      <td className="py-2 px-3 text-[var(--fg)]">
                        {nameById[e.accountId || ''] || e.accountId || '—'}
                      </td>
                      <td className="py-2 px-3 whitespace-nowrap">
                        <div className="flex items-center gap-1">
                          {e.ok ? (
                            <StatusBadge tone="ok">{e.already ? '今日已签' : '已签到'}</StatusBadge>
                          ) : (
                            <StatusBadge tone="err">失败</StatusBadge>
                          )}
                          {e.degraded && (
                            <StatusBadge tone="warn" title="绑定出口失败后回落直连">
                              降级
                            </StatusBadge>
                          )}
                        </div>
                      </td>
                      <td className="py-2 px-3 ds-mono text-[var(--fg)]">
                        {e.credits ? `+${e.credits}` : '—'}
                      </td>
                      <td className="py-2 px-3 ds-mono text-[var(--fg-2)]">
                        {e.egress ? proxyHost(e.egress) : '直连'}
                      </td>
                      <td className="py-2 px-3 text-[var(--fg-2)]">{e.message || '—'}</td>
                    </tr>
                  ))
                )}
              </tbody>
            </table>
          </div>
        </div>
      </SlideDrawer>
    </div>
  );
}
