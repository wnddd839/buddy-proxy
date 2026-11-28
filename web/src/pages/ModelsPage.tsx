import { useEffect, useMemo, useState } from 'react';
import {
  ArrowDown,
  ArrowRight,
  ArrowUp,
  BookOpen,
  Copy,
  Plus,
  RefreshCw,
  Search,
  Trash2,
} from 'lucide-react';
import type {
  AccountSummary,
  ModelItem,
  ModelRouteRule,
  ModelRoutesPayload,
  RouteMode,
  Site,
  TitleRoute,
  UsageEntry,
} from '@/types/api';
import {
  computeModelAvailability,
  mergeModelLists,
  normalizeRouteMode,
  routeRuleKey,
  validateModelRoutes,
} from '@/lib/domain';
import { bareModelId, formatTime, normalizeSite, shortId, siteLabel } from '@/lib/format';
import { apiRequest, copyToClipboard } from '@/lib/api';
import { useToast } from '@/hooks/useToast';
import { Button } from '@/components/ui/Button';
import { Card, EmptyState, PageHeader } from '@/components/ui/Card';
import { ScopeNote } from '@/components/ui/ScopeNote';
import { DirtySaveBar } from '@/components/ui/DirtySaveBar';
import { SegmentedControl } from '@/components/ui/SegmentedControl';
import { SlideDrawer } from '@/components/ui/SlideDrawer';
import { StatusBadge } from '@/components/ui/StatusBadge';

interface EchoMetricProps {
  id: string;
  label: string;
  value: number | string;
  title?: string;
}

/** 回显对照的一个计数格。id 必须保留：外部探针与历史断言都按 id 找。 */
function EchoMetric({ id, label, value, title }: EchoMetricProps) {
  return (
    <div title={title} className="ds-surface p-2.5">
      <div className="text-[11px] text-[var(--fg-3)]">{label}</div>
      <div id={id} className="ds-mono text-lg text-[var(--fg)] mt-0.5">
        {value}
      </div>
    </div>
  );
}

interface ModelsPageProps {
  activeSite: Site;
  accounts: AccountSummary[];
  quotaFloorSaved: number;
  models: ModelItem[];
  modelsRaw: string;
  modelsScope: 'domestic' | 'global' | 'both';
  onSetModelsScope: (scope: 'domestic' | 'global' | 'both') => void;
  onRefreshModels: (scope?: 'domestic' | 'global' | 'both') => Promise<void>;
  routesPayload: ModelRoutesPayload | null;
  routesError: string;
  onRoutesSaved: (rules: ModelRouteRule[], titleRoute?: TitleRoute) => void;
}

export function ModelsPage({
  accounts,
  quotaFloorSaved,
  models,
  modelsRaw,
  modelsScope,
  onSetModelsScope,
  onRefreshModels,
  routesPayload,
  routesError,
  onRoutesSaved,
}: ModelsPageProps) {
  const { showToast } = useToast();
  const [search, setSearch] = useState('');
  const [onlyFree, setOnlyFree] = useState(false);
  const [loadingModels, setLoadingModels] = useState(false);
  const [priorityDrawerOpen, setPriorityDrawerOpen] = useState(false);

  // 路由草稿状态
  const [rules, setRules] = useState<ModelRouteRule[]>([]);
  const [savedSnapshot, setSavedSnapshot] = useState('');
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState('');
  const [justSaved, setJustSaved] = useState(false);

  // 会话标题改写（TitleRoute）。注意：POST /model-routes 是整体写入，
  // 保存路由时如果不带 titleRoute，后端会把标题配置写成 nil。
  const [titleRoute, setTitleRoute] = useState<TitleRoute>({ enabled: false });
  const [savingTitle, setSavingTitle] = useState(false);

  // 上游模型回显对照（I8）：直接复用 /usage 已有的 upstreamModel /
  // modelMismatch 字段，不新增后端接口。
  // null = 还没成功加载过。绝不能初始化为 []：那会让四个计数显示 0，
  // 看起来像「没有模型替换」——本页最危险的假阴性。
  const [echoRows, setEchoRows] = useState<UsageEntry[] | null>(null);
  const [echoLoading, setEchoLoading] = useState(false);
  const [echoError, setEchoError] = useState('');

  useEffect(() => {
    if (!routesPayload) return;
    const normalized: ModelRouteRule[] = (routesPayload.rules || []).map((r) => ({
      fromSite: normalizeSite(r.fromSite),
      match: r.match || '',
      site: normalizeSite(r.site),
      target: r.target || '',
      mode: normalizeRouteMode(r.mode),
      note: r.note || '',
    }));
    const snap = JSON.stringify(normalized);
    setRules(normalized);
    setSavedSnapshot(snap);
    // titleRoute 与 rules 同源同载荷，必须一起同步；否则保存路由时
    // 会用初始空值覆盖线上标题配置。
    const tr = routesPayload.titleRoute;
    setTitleRoute({
      enabled: tr?.enabled === true,
      domestic: tr?.domestic || '',
      global: tr?.global || '',
    });
  }, [routesPayload]);

  const isDirty = useMemo(() => {
    if (!savedSnapshot) return false;
    return JSON.stringify(rules) !== savedSnapshot;
  }, [rules, savedSnapshot]);

  const catalog = routesPayload?.catalog || { domestic: [], global: [] };
  // 标题改写优先只列免费模型：后端 freeModels 就是为此算的。拿不到时
  // 退回全量编目，避免下拉为空导致「看起来没配」。
  const titleDomOptions = routesPayload?.freeModels?.domestic ?? catalog.domestic ?? [];
  const titleGloOptions = routesPayload?.freeModels?.global ?? catalog.global ?? [];

  // 最近 7 天里带上游回显的请求。旧数据没有该字段，过滤掉以免把
  // 「没观测」混进「一致」。
  const refreshModelEcho = async () => {
    setEchoLoading(true);
    setEchoError('');
    try {
      const data = await apiRequest<{ requests?: UsageEntry[] }>(
        '/direct-admin/api/usage?range=week&limit=100',
      );
      setEchoRows((data.requests || []).filter((r) => r.upstreamModel));
    } catch (e: unknown) {
      const msg = e instanceof Error ? e.message : String(e);
      setEchoError(`回显对照加载失败：${msg}`);
    } finally {
      setEchoLoading(false);
    }
  };

  useEffect(() => {
    refreshModelEcho().catch(() => {});
    // 只在挂载时拉一次；之后用「刷新对照」按钮。
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const echoStats = useMemo(() => {
    if (!echoRows) {
      return {
        total: null as number | null,
        match: null as number | null,
        mismatch: null as number | null,
        auto: null as number | null,
        bad: [] as UsageEntry[],
      };
    }
    let match = 0;
    let mismatch = 0;
    let auto = 0;
    const bad: UsageEntry[] = [];
    for (const r of echoRows) {
      if (r.modelMismatch) {
        mismatch++;
        bad.push(r);
        continue;
      }
      // 与后端 isAutoFamily 一致：先去掉 cn:/global: 站点前缀再判 auto。
      const sent = (r.routedModel || r.model || '')
        .toLowerCase()
        .replace(/^(cn|global):/, '');
      if (sent.indexOf('auto') === 0) {
        auto++;
        continue;
      }
      match++;
    }
    return { total: echoRows.length, match, mismatch, auto, bad };
  }, [echoRows]);

  // 上游回显单元格。函数名沿用旧面板 upstreamModelCell —— 源码级契约
  // 测试按这个名字定位（构建产物里会被压缩）。
  const upstreamModelCell = (r: UsageEntry) =>
    r.modelMismatch ? (
      <StatusBadge tone="warn">{r.upstreamModel}</StatusBadge>
    ) : (
      <span className="ds-mono">{r.upstreamModel || '—'}</span>
    );

  const handleFetchScope = async (scope: 'domestic' | 'global' | 'both') => {
    setLoadingModels(true);
    try {
      onSetModelsScope(scope);
      await onRefreshModels(scope);
    } catch (e: unknown) {
      showToast(e instanceof Error ? e.message : String(e), 'error');
    } finally {
      setLoadingModels(false);
    }
  };

  // 规范化展示用模型列表
  const normalizedModels = useMemo(() => {
    return models.map((m) => {
      const raw = m.modelId || m.upstreamId || m.id || m.name || m.model || 'model';
      const id = bareModelId(raw);
      const displayName = bareModelId(m.displayName || m.name || id);
      const credits = m.credits ? String(m.credits) : '';
      const free = m.free === true || /x0(\.0+)?\s*credits/i.test(credits);
      const mult = m.creditMultiplier ?? null;
      const sites = Array.isArray(m.sites) ? m.sites : undefined;
      const avail = computeModelAvailability(id, sites, accounts, quotaFloorSaved);
      return { id, displayName, credits, free, mult, sites, description: m.description || '', avail };
    });
  }, [models, accounts, quotaFloorSaved]);

  const visibleModels = useMemo(() => {
    const q = search.trim().toLowerCase();
    return normalizedModels.filter((m) => {
      if (onlyFree && !m.free) return false;
      if (q && !(m.id + ' ' + m.displayName + ' ' + m.description).toLowerCase().includes(q)) {
        return false;
      }
      return true;
    });
  }, [normalizedModels, search, onlyFree]);

  // 路由序号计算（同一源区 + 源模型第 2 条起标备用 N）
  const routeMeta = useMemo(() => {
    const seen: Record<string, number> = {};
    return rules.map((r, i) => {
      const k = routeRuleKey(r);
      seen[k] = (seen[k] || 0) + 1;
      return { rule: r, index: i, seq: seen[k] };
    });
  }, [rules]);

  const updateRule = (idx: number, patch: Partial<ModelRouteRule>) => {
    setJustSaved(false);
    setSaveError('');
    setRules((prev) => prev.map((r, i) => (i === idx ? { ...r, ...patch } : r)));
  };

  const addRule = () => {
    setJustSaved(false);
    setSaveError('');
    setRules((prev) => [
      ...prev,
      {
        fromSite: 'domestic',
        match: '',
        site: 'global',
        target: '',
        mode: 'onExhausted',
        note: '',
      },
    ]);
  };

  const moveRule = (idx: number, delta: -1 | 1) => {
    const target = idx + delta;
    if (target < 0 || target >= rules.length) return;
    setJustSaved(false);
    setRules((prev) => {
      const next = prev.slice();
      const tmp = next[target];
      next[target] = next[idx];
      next[idx] = tmp;
      return next;
    });
  };

  const removeRule = (idx: number) => {
    setJustSaved(false);
    setRules((prev) => prev.filter((_, i) => i !== idx));
  };

  const titleDom = titleRoute.domestic || '';
  const titleGlo = titleRoute.global || '';
  // 未保存提示：不能让「改了下拉但没点保存」看起来已生效。
  const titleDirty =
    !!routesPayload &&
    (titleRoute.enabled !== (routesPayload.titleRoute?.enabled === true) ||
      titleDom !== (routesPayload.titleRoute?.domestic || '') ||
      titleGlo !== (routesPayload.titleRoute?.global || ''));
  let titleStateText: string;
  if (titleRoute.enabled !== true) {
    titleStateText = '未启用（会话标题按原模型计费）';
  } else if (!titleDom && !titleGlo) {
    // 开关开着但一个目标都没配 —— 最常见的「以为在省钱其实没有」。
    titleStateText = '已启用但未配置目标 ⇒ 实际不改写，标题仍由主模型生成';
  } else {
    titleStateText = `已启用：国内 → ${titleDom || '不改写'} · 国际 → ${titleGlo || '不改写'}`;
  }
  if (titleDirty) titleStateText += ' · 有未保存的更改';

  const handleSaveTitleRoute = async () => {
    // payload 没加载完时 rules / savedSnapshot 都是空的：此时 POST 会
    // 用 rules:[] 覆盖线上整张路由表（后端是整体写入）。必须硬拦。
    if (!routesPayload) {
      showToast('路由配置还没加载完，已阻止保存（避免把线上路由清空）', 'error');
      return;
    }
    if (savingTitle) return;
    setSavingTitle(true);
    try {
      // 路由草稿可能还没保存：只提交已保存的那一版，避免「保存标题」
      // 顺手把未确认的路由改动写进线上。
      const rulesToPersist = savedSnapshot
        ? (JSON.parse(savedSnapshot) as ModelRouteRule[])
        : rules;
      await apiRequest('/direct-admin/api/model-routes', {
        method: 'POST',
        body: JSON.stringify({ rules: rulesToPersist, titleRoute }),
      });
      // 回写父级 payload：否则切页回来会显示旧配置，下一次保存路由
      //（父级 payload 里的旧 titleRoute）会把它静默改回去。
      onRoutesSaved(rulesToPersist, titleRoute);
      showToast('标题改写配置已保存并生效', 'success');
    } catch (e: unknown) {
      showToast(e instanceof Error ? e.message : String(e), 'error');
    } finally {
      setSavingTitle(false);
    }
  };

  const handleSaveRoutes = async () => {
    if (saving) return;
    const err = validateModelRoutes(rules);
    if (err) {
      setSaveError(err);
      showToast(err, 'error');
      return;
    }
    setSaving(true);
    setSaveError('');
    try {
      await apiRequest('/direct-admin/api/model-routes', {
        method: 'POST',
        // 必须带 titleRoute：后端按 Store{Rules,TitleRoute} 整体写入，
        // 少这个字段就等于把线上标题改写配置清空。
        body: JSON.stringify({ rules, titleRoute }),
      });
      const snap = JSON.stringify(rules);
      setSavedSnapshot(snap);
      setJustSaved(true);
      onRoutesSaved(rules, titleRoute);
      showToast('模型路由已保存并生效', 'success');
    } catch (e: unknown) {
      const msg = e instanceof Error ? e.message : String(e);
      setSaveError(`保存失败，草稿已保留：${msg}`);
      showToast(msg, 'error');
    } finally {
      setSaving(false);
    }
  };

  const explainRule = (r: ModelRouteRule) => {
    const fs = normalizeSite(r.fromSite);
    const ts = normalizeSite(r.site);
    if (!r.match.trim()) return '请填写源模型名（支持下拉补全或手动输入）。';
    if (!r.target.trim()) return `${siteLabel(fs)}站收到 ${r.match} → 请选择目标模型。`;
    if (fs === ts && r.match.trim() === r.target.trim()) {
      return '源和目标完全一致，这条规则不会生效。';
    }
    const lead =
      r.mode === 'onExhausted'
        ? `${siteLabel(fs)}站有可用号时正常跑 ${r.match}，用尽后自动切到`
        : `${siteLabel(fs)}站收到 ${r.match} 时立即改用`;
    if (fs === ts) return `${lead}同站的 ${r.target}（同区替身）。`;
    return `${lead} ${siteLabel(ts)}站的 ${r.target}。`;
  };

  const routeSaveCode = routesError || saveError ? 'error' : saving ? 'saving' : isDirty ? 'dirty' : 'saved';
  const routeSaveText = routesError
    ? `读取失败：${routesError}`
    : saveError
      ? saveError
      : saving
        ? '正在保存路由规则…'
        : isDirty
          ? '有未保存的路由规则'
          : justSaved
            ? '路由规则已保存并生效'
            : routesPayload
              ? '路由规则已保存'
              : '配置加载中…';

  // Ensure helper export isn't unused even if merge happens in App
  void mergeModelLists;

  return (
    <div id="tab-models" className="ds-page-enter space-y-5">
      <PageHeader
        title="模型编目与跨区路由"
        subtitle="点击模型卡片即可复制 ID，实时标注当前号池可用/冷却/额度不足账号数；下方按方向配置跨区改写与备用链。"
        actions={
          <>
            <Button
              id="btnModelsDomestic"
              variant="ghost"
              size="xs"
              loading={loadingModels && modelsScope === 'domestic'}
              onClick={() => handleFetchScope('domestic')}
            >
              <RefreshCw className="w-3 h-3" />
              拉取国内
            </Button>
            <Button
              id="btnModelsGlobal"
              variant="ghost"
              size="xs"
              loading={loadingModels && modelsScope === 'global'}
              onClick={() => handleFetchScope('global')}
            >
              <RefreshCw className="w-3 h-3" />
              拉取国际
            </Button>
            <Button
              id="btnModelsBoth"
              variant="ghost"
              size="xs"
              loading={loadingModels && modelsScope === 'both'}
              onClick={() => handleFetchScope('both')}
            >
              两站都拉
            </Button>
          </>
        }
      />

      <ScopeNote>
        本页三块<strong>互不依赖</strong>：编目是从两个站各拉一份真实模型列表；
        路由是你<strong>额外配置</strong>的改写规则，不是上游事实；标题改写只影响展示名。
            </ScopeNote>

      {/* 模型编目卡片区 */}
      <Card className="p-4 space-y-4">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div className="flex items-center gap-3 flex-wrap">
            <SegmentedControl<'domestic' | 'global' | 'both'>
              id="modelsScopeSeg"
              ariaLabel="模型编目范围"
              size="xs"
              value={modelsScope}
              onChange={(s) => handleFetchScope(s)}
              options={[
                { value: 'domestic', label: '国内站', dataAttrs: { 'data-msite': 'domestic' } },
                { value: 'global', label: '国际站', dataAttrs: { 'data-msite': 'global' } },
                { value: 'both', label: '两站合并', dataAttrs: { 'data-msite': 'both' } },
              ]}
            />
            <span id="modelsScopeHint" className="text-xs text-[var(--fg-3)]">
              当前显示：
              {modelsScope === 'both' ? '两站合并' : `${siteLabel(modelsScope)}站`}模型（
              {visibleModels.length}）
            </span>
          </div>

          <div className="flex items-center gap-2.5 flex-wrap">
            <div className="relative w-56 max-w-full">
              <Search className="w-3.5 h-3.5 absolute left-2.5 top-1/2 -translate-y-1/2 text-[var(--fg-3)]" />
              <input
                value={search}
                onChange={(e) => setSearch(e.target.value)}
                placeholder="筛选模型 ID..."
                className="ds-input w-full pl-8 pr-2.5 py-1 text-xs"
              />
            </div>
            <label className="inline-flex items-center gap-1.5 text-xs text-[var(--fg-2)] cursor-pointer select-none">
              <input
                type="checkbox"
                checked={onlyFree}
                onChange={(e) => setOnlyFree(e.target.checked)}
                className="accent-[var(--accent)]"
              />
              仅免费
            </label>
          </div>
        </div>

        <div id="modelChips">
          {!visibleModels.length ? (
            <EmptyState
              title="暂无模型数据"
              description="请确认当前站点有已授权账号，或点击右上角重新拉取上游编目。"
              className="py-7"
            />
          ) : (
            <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 gap-2.5">
              {visibleModels.map((m) => (
                <button
                  key={m.id}
                  type="button"
                  data-copy={m.id}
                  data-sites={(m.sites || []).join(',')}
                  title={[m.id, m.credits ? `倍率 ${m.credits}` : '', m.description]
                    .filter(Boolean)
                    .join(' | ')}
                  onClick={() =>
                    copyToClipboard(m.id)
                      .then(() => showToast(`模型 ${m.id} 已复制`, 'success'))
                      .catch((e) => showToast(String(e), 'error'))
                  }
                  className="group ds-surface-2 p-3 text-left hover:border-[var(--border-strong)] hover:bg-[var(--surface)] transition-all cursor-pointer flex flex-col justify-between gap-2"
                >
                  <div className="flex items-start justify-between gap-2">
                    <div className="min-w-0">
                      <div className="ds-mono text-xs font-medium text-[var(--fg)] truncate group-hover:underline underline-offset-2">
                        {m.displayName}
                      </div>
                      {m.displayName !== m.id && (
                        <div className="ds-mono text-[10px] text-[var(--fg-3)] truncate mt-0.5">
                          {m.id}
                        </div>
                      )}
                    </div>
                    <Copy className="w-3 h-3 text-[var(--fg-3)] opacity-0 group-hover:opacity-100 transition shrink-0 mt-0.5" />
                  </div>

                  <div className="flex items-center justify-between gap-1.5 flex-wrap pt-1 border-t border-[var(--border)]">
                    <div className="flex items-center gap-1 flex-wrap">
                      {m.free ? (
                        <StatusBadge tone="ok" className="!text-[10px] !px-1.5 !py-0">
                          免费
                        </StatusBadge>
                      ) : m.mult != null && m.credits ? (
                        <StatusBadge tone="info" className="!text-[10px] !px-1.5 !py-0">
                          {m.mult}x
                        </StatusBadge>
                      ) : null}
                      {m.sites?.length === 1 && (
                        <StatusBadge tone="neutral" className="!text-[10px] !px-1.5 !py-0">
                          仅{m.sites[0] === 'domestic' ? '国内' : '国际'}
                        </StatusBadge>
                      )}
                      {m.sites?.length === 2 && (
                        <StatusBadge tone="neutral" className="!text-[10px] !px-1.5 !py-0">
                          两站
                        </StatusBadge>
                      )}
                    </div>
                    <StatusBadge
                      tone={m.avail.tone}
                      title={m.avail.title}
                      className="model-avail !text-[10px] !px-1.5 !py-0"
                    >
                      {m.avail.text}
                    </StatusBadge>
                  </div>
                </button>
              ))}
            </div>
          )}
        </div>

        <details className="ds-surface-2 px-3 py-2 text-xs">
          <summary className="text-[11px] text-[var(--fg-3)] cursor-pointer select-none">
            查看原始模型响应 JSON
          </summary>
          <pre
            id="modelsRaw"
            className="ds-mono text-[11px] text-[var(--fg-2)] mt-2 max-h-60 overflow-auto whitespace-pre-wrap m-0"
          >
            {modelsRaw || '[]'}
          </pre>
        </details>
      </Card>

      {/* 有方向的模型路由链路区 */}
      <Card className="p-5 space-y-4">
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div>
            <h2 className="ds-heading text-lg text-[var(--fg)] m-0">有方向的模型路由与备用链</h2>
            <p className="text-xs text-[var(--fg-3)] mt-1 m-0 max-w-2xl leading-relaxed">
              同一「源区域 + 源模型」可配置多条形成有序备用链。没命中规则的模型一字节不改。
            </p>
          </div>
          <div className="flex items-center gap-2 flex-wrap">
            <span
              id="routeSaveState"
              role="status"
              aria-live="polite"
              data-state={routeSaveCode}
              className={`ds-mono text-xs ${
                routeSaveCode === 'error'
                  ? 'text-[var(--err)]'
                  : routeSaveCode === 'dirty'
                    ? 'text-[var(--warn)] font-medium'
                    : 'text-[var(--fg-3)]'
              }`}
            >
              {routeSaveText}
            </span>
            <Button variant="ghost" size="xs" onClick={() => setPriorityDrawerOpen(true)}>
              <BookOpen className="w-3 h-3" />
              区域优先级说明
            </Button>
            <Button
              id="btnRouteAdd"
              variant="ghost"
              size="sm"
              disabled={!routesPayload || saving}
              onClick={addRule}
            >
              <Plus className="w-3.5 h-3.5" />
              新增路由
            </Button>
            <Button
              id="btnRouteSave"
              variant="primary"
              size="sm"
              loading={saving}
              disabled={!routesPayload}
              onClick={handleSaveRoutes}
            >
              保存路由
            </Button>
          </div>
        </div>

        <div id="routeRules" className="space-y-3">
          {!routeMeta.length ? (
            <EmptyState
              title="还没有配置模型路由"
              description="点「新增路由」可创建一条跨区兜底或同区模型替身，例如：国内站收到 gpt-5.6 → 用尽时切至国际站 gpt-5.6。"
              action={
                <Button variant="primary" size="sm" disabled={!routesPayload} onClick={addRule}>
                  <Plus className="w-3.5 h-3.5" />
                  创建第一条路由
                </Button>
              }
            />
          ) : (
            routeMeta.map(({ rule: r, index: i, seq }) => {
              const fromList = Array.from(new Set(catalog[r.fromSite] || []));
              const toList = Array.from(new Set(catalog[r.site] || []));
              const isBackup = seq > 1;
              return (
                <div
                  key={i}
                  data-idx={i}
                  className={[
                    'route-rule ds-surface-2 p-3.5 space-y-2.5 transition-all',
                    isBackup ? 'ml-4 sm:ml-6 border-l-2 border-l-[var(--info)]' : '',
                  ].join(' ')}
                >
                  <div className="flex flex-wrap items-center gap-2 text-xs">
                    {isBackup ? (
                      <StatusBadge tone="info">备用 {seq}</StatusBadge>
                    ) : (
                      <StatusBadge tone="ok">主规则</StatusBadge>
                    )}

                    {/* 源 */}
                    <select
                      data-rf="fromSite"
                      aria-label="源区域"
                      disabled={saving}
                      value={r.fromSite}
                      onChange={(e) => updateRule(i, { fromSite: normalizeSite(e.target.value) })}
                      className="ds-select px-2 py-1 text-xs"
                    >
                      <option value="domestic">国内站</option>
                      <option value="global">国际站</option>
                    </select>
                    <input
                      type="text"
                      data-rf="match"
                      aria-label="源模型"
                      disabled={saving}
                      value={r.match}
                      onChange={(e) => updateRule(i, { match: e.target.value })}
                      placeholder="源模型，如 gpt-5.6"
                      list={`routeFromList-${i}`}
                      className="ds-input ds-mono w-44 max-w-full px-2.5 py-1 text-xs"
                    />
                    <datalist id={`routeFromList-${i}`}>
                      {fromList.map((n) => (
                        <option key={n} value={n} />
                      ))}
                    </datalist>

                    {/* 方向与触发模式 */}
                    <span className="inline-flex items-center gap-1 text-[var(--fg-3)]">
                      <ArrowRight className="w-3.5 h-3.5" />
                    </span>

                    {/* 目标 */}
                    <select
                      data-rf="site"
                      aria-label="目标区域"
                      disabled={saving}
                      value={r.site}
                      onChange={(e) => updateRule(i, { site: normalizeSite(e.target.value) })}
                      className="ds-select px-2 py-1 text-xs"
                    >
                      <option value="domestic">国内站</option>
                      <option value="global">国际站</option>
                    </select>
                    <input
                      type="text"
                      data-rf="target"
                      aria-label="目标模型"
                      disabled={saving}
                      value={r.target}
                      onChange={(e) => updateRule(i, { target: e.target.value })}
                      placeholder="目标模型"
                      list={`routeToList-${i}`}
                      className="ds-input ds-mono w-44 max-w-full px-2.5 py-1 text-xs"
                    />
                    <datalist id={`routeToList-${i}`}>
                      {toList.map((n) => (
                        <option key={n} value={n} />
                      ))}
                    </datalist>

                    <select
                      data-rf="mode"
                      aria-label="生效时机"
                      disabled={saving}
                      value={r.mode}
                      onChange={(e) => updateRule(i, { mode: e.target.value as RouteMode })}
                      className="ds-select px-2 py-1 text-xs"
                    >
                      <option value="always">立即改写</option>
                      <option value="onExhausted">源区用尽才跳</option>
                    </select>

                    <input
                      type="text"
                      data-rf="note"
                      aria-label="备注"
                      disabled={saving}
                      value={r.note}
                      onChange={(e) => updateRule(i, { note: e.target.value })}
                      placeholder="备注（可选）"
                      className="ds-input w-32 px-2 py-1 text-xs"
                    />

                    <div className="ml-auto flex items-center gap-1">
                      <Button
                        variant="ghost"
                        size="xs"
                        disabled={saving || i === 0}
                        title="上移（提高优先级）"
                        onClick={() => moveRule(i, -1)}
                      >
                        <ArrowUp className="w-3 h-3" />
                      </Button>
                      <Button
                        variant="ghost"
                        size="xs"
                        disabled={saving || i === routeMeta.length - 1}
                        title="下移（降低优先级）"
                        onClick={() => moveRule(i, 1)}
                      >
                        <ArrowDown className="w-3 h-3" />
                      </Button>
                      <Button
                        variant="danger"
                        size="xs"
                        disabled={saving}
                        title="删除规则"
                        onClick={() => removeRule(i)}
                      >
                        <Trash2 className="w-3 h-3" />
                      </Button>
                    </div>
                  </div>

                  <p className="text-[11px] text-[var(--fg-2)] m-0 leading-relaxed">
                    {explainRule(r)}
                  </p>
                </div>
              );
            })
          )}
        </div>
      </Card>

      {/* 会话标题改写（TitleRoute）。上游 v0.8.0 的 React 重写漏了整块。 */}
      <Card className="p-5 space-y-4">
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div>
            <h2 className="ds-heading text-lg text-[var(--fg)] m-0">会话标题改写路由</h2>
            <p className="text-xs text-[var(--fg-3)] mt-1 m-0 max-w-2xl leading-relaxed">
              会话标题由一次独立的模型请求生成。把它单独指到便宜或免费模型，正文请求完全不受影响；
              目标留空 = 不改写 = 标题仍由当前会话的主模型生成。
            </p>
          </div>
          <div className="flex items-center gap-2 flex-wrap">
            <span id="titleRouteState" className="ds-mono text-xs text-[var(--fg-3)]">
              {titleStateText}
            </span>
            <Button
              id="btnSaveTitleRoute"
              variant="primary"
              size="sm"
              loading={savingTitle}
              disabled={!routesPayload || savingTitle}
              onClick={handleSaveTitleRoute}
            >
              保存标题改写
            </Button>
          </div>
        </div>
        <div className="flex flex-col sm:flex-row flex-wrap gap-3 items-start sm:items-center">
          <label className="inline-flex items-center gap-2 text-xs text-[var(--fg-2)] cursor-pointer">
            <input
              id="titleRouteEnabled"
              type="checkbox"
              className="accent-[var(--accent)]"
              disabled={savingTitle}
              checked={titleRoute.enabled === true}
              onChange={(e) =>
                setTitleRoute((prev) => ({ ...prev, enabled: e.target.checked }))
              }
            />
            启用标题改写
          </label>
          <label className="text-xs text-[var(--fg-2)] flex items-center gap-2">
            国内站
            <select
              className="ds-input ds-mono text-xs px-2 py-1"
              value={titleDom}
              disabled={titleRoute.enabled !== true || savingTitle}
              onChange={(e) => setTitleRoute((prev) => ({ ...prev, domestic: e.target.value }))}
            >
              <option value="">沿用请求模型（不改写）</option>
              {titleDom && !titleDomOptions.includes(titleDom) && (
                <option value={titleDom}>{titleDom}（不在免费列表）</option>
              )}
              {titleDomOptions.map((m) => (
                <option key={m} value={m}>
                  {m}
                </option>
              ))}
            </select>
            <span className="text-[10px] text-[var(--fg-3)]">
              {titleDomOptions.length > 0
                ? `可选 ${titleDomOptions.length} 个免费模型`
                : '该站点没有免费模型'}
            </span>
          </label>
          <label className="text-xs text-[var(--fg-2)] flex items-center gap-2">
            国际站
            <select
              className="ds-input ds-mono text-xs px-2 py-1"
              value={titleGlo}
              disabled={titleRoute.enabled !== true || savingTitle}
              onChange={(e) => setTitleRoute((prev) => ({ ...prev, global: e.target.value }))}
            >
              <option value="">沿用请求模型（不改写）</option>
              {titleGlo && !titleGloOptions.includes(titleGlo) && (
                <option value={titleGlo}>{titleGlo}（不在免费列表）</option>
              )}
              {titleGloOptions.map((m) => (
                <option key={m} value={m}>
                  {m}
                </option>
              ))}
            </select>
            <span className="text-[10px] text-[var(--fg-3)]">
              {titleGloOptions.length > 0
                ? `可选 ${titleGloOptions.length} 个免费模型`
                : '该站点没有免费模型'}
            </span>
          </label>
        </div>
      </Card>

      {/* 上游模型回显对照（I8）。模型被替换不会报错，只能靠不一致数发现。 */}
      <Card className="p-5 space-y-4">
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div>
            <h2 className="ds-heading text-lg text-[var(--fg)] m-0">上游模型回显对照</h2>
            <p className="text-xs text-[var(--fg-3)] mt-1 m-0 max-w-3xl leading-relaxed">
              发出去的模型与上游 SSE 实际回显的模型是否一致。
              <strong className="text-[var(--fg-2)]">不一致即说明模型被替换</strong>
              ，此时所有按模型做的分析（思考占比、档位效果、缓存命中率）都会失效，且不会有任何报错。
              归一化后比较：<code className="ds-mono">hy4flash-free</code> 与{' '}
              <code className="ds-mono">hy4-flash-free</code> 视为同一模型（实测上游确实少一个连字符）；
              <code className="ds-mono">auto</code> 一族由上游选模型，标为「解析」，不算替换。
            </p>
          </div>
          <Button
            id="btnRefreshEcho"
            variant="ghost"
            size="sm"
            loading={echoLoading}
            onClick={refreshModelEcho}
          >
            <RefreshCw className={`w-3.5 h-3.5 ${echoLoading ? 'animate-spin' : ''}`} />
            刷新对照
          </Button>
        </div>
        {echoError && <p className="text-xs text-[var(--err)] m-0">{echoError}</p>}
        <div className="grid grid-cols-2 sm:grid-cols-4 gap-2.5">
          <EchoMetric
            id="echoTotal"
            label="观测到的请求"
            value={echoStats.total ?? '—'}
            title="最近 7 天里带上游回显的请求数"
          />
          <EchoMetric id="echoMatch" label="一致" value={echoStats.match ?? '—'} />
          <EchoMetric
            id="echoMismatch"
            label="不一致"
            value={echoStats.mismatch ?? '—'}
            title="归一化后仍不一致 —— 这才是真的模型替换"
          />
          <EchoMetric
            id="echoAuto"
            label="上游解析"
            value={echoStats.auto ?? '—'}
            title="auto 一族由上游解析成具体模型，不算异常"
          />
        </div>
        {echoStats.bad.length > 0 ? (
          <div className="overflow-x-auto">
            <table aria-label="模型替换明细" className="ds-table w-full text-xs">
              <thead>
                <tr>
                  <th>时间</th>
                  <th>请求 ID</th>
                  <th>发出去</th>
                  <th>上游回显</th>
                  <th>账号</th>
                </tr>
              </thead>
              <tbody>
                {echoStats.bad.slice(0, 20).map((r) => (
                  <tr key={r.proxyRequestId || String(r.at)}>
                    <td className="ds-mono">{formatTime(r.at)}</td>
                    <td className="ds-mono">{shortId(r.proxyRequestId)}</td>
                    <td className="ds-mono">{r.routedModel || r.model || '—'}</td>
                    <td>{upstreamModelCell(r)}</td>
                    <td>{r.accountLabel || shortId(r.accountId)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        ) : echoRows === null ? (
          <p className="text-xs text-[var(--fg-3)] m-0">
            {echoError
              ? '回显对照尚未成功加载，无法判断有没有模型替换。'
              : '正在加载最近 7 天的回显记录…'}
          </p>
        ) : (
          <p className="text-xs text-[var(--fg-3)] m-0">
            {echoRows.length === 0
              ? '最近 7 天没有带上游回显的记录（旧数据不含该字段）。'
              : `最近 ${echoRows.length} 条带回显的请求里没有模型替换。`}
          </p>
        )}
      </Card>

      <DirtySaveBar
        visible={isDirty}
        saving={saving}
        message="模型路由规则有未保存的更改"
        subMessage="保存后将立即写入并审计落盘"
        onReset={() => {
          if (savedSnapshot) setRules(JSON.parse(savedSnapshot) as ModelRouteRule[]);
          setSaveError('');
        }}
        onSave={handleSaveRoutes}
      />

      {/* 优先级说明抽屉 */}
      <SlideDrawer
        open={priorityDrawerOpen}
        onClose={() => setPriorityDrawerOpen(false)}
        title="区域路由优先级（从高到低）"
        subtitle="当多个来源同时声明目标区域时，网关严格按下列层级裁决。"
      >
        <ol className="list-decimal pl-5 space-y-3 text-xs leading-relaxed text-[var(--fg-2)]">
          <li>
            <strong className="text-[var(--fg)]">客户端前缀硬钉</strong>：模型名带{' '}
            <code className="ds-mono">cn:</code> / <code className="ds-mono">global:</code>{' '}
            前缀时，直接跳过下方所有路由规则，严格走指定区域。
          </li>
          <li>
            <strong className="text-[var(--fg)]">有方向模型路由</strong>：按请求入站区域 +
            模型名匹配。选择「立即改写」会直接映射；选择「源区用尽才跳」则仅当源区无可用号时触发。同一源键可配多条组成备用链。
          </li>
          <li>
            <strong className="text-[var(--fg)]">绑定 Key / X-Site 请求头</strong>：决定请求从哪个区域进入，进入后依然接受模型路由规则。
          </li>
          <li>
            <strong className="text-[var(--fg)]">号池默认区</strong>：顶栏所选的国内 / 国际号池，在以上三层均未指定时兜底。
          </li>
        </ol>
      </SlideDrawer>
    </div>
  );
}
