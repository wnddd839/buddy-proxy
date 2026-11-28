import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import type {
  AccountUsageItem,
  CheckinConfigPayload,
  ClientConfigPayload,
  ConcurrencyPayload,
  ModelItem,
  ModelRouteRule,
  ModelRoutesPayload,
  Product,
  ProxiesPayload,
  Site,
  StatusPayload,
  SystemPromptMode,
  SystemPromptOverview,
  TitleRoute,
} from '@/types/api';
import { apiRequest } from '@/lib/api';
import {
  computeAccountStates,
  computeCreditLevel,
  mergeModelLists,
  summarizeConcurrency,
} from '@/lib/domain';
import { normalizeProduct, normalizeSite } from '@/lib/format';
import { useTheme } from '@/hooks/useTheme';
import { ToastProvider, useToast } from '@/hooks/useToast';
import { ConfirmProvider } from '@/hooks/useConfirm';
import { ToastViewport } from '@/components/ui/ToastViewport';
import { AppShell, type TabId } from '@/components/layout/AppShell';
import { OverviewPage } from '@/pages/OverviewPage';
import { PoolPage } from '@/pages/PoolPage';
import { CheckinPage } from '@/pages/CheckinPage';
import { ClientPage } from '@/pages/ClientPage';
import { ModelsPage } from '@/pages/ModelsPage';
import { SystemPromptPage } from '@/pages/SystemPromptPage';
import { UsagePage } from '@/pages/UsagePage';
import { InvariantsPage } from '@/pages/InvariantsPage';
import { TokPage } from '@/pages/TokPage';

const CHAPTER_HASHES: Record<TabId, string> = {
  'tab-overview': '#overview',
  'tab-pool': '#accounts',
  'tab-checkin': '#checkin',
  'tab-client': '#client-config',
  'tab-models': '#models',
  'tab-system-prompt': '#system-prompt',
  'tab-usage': '#usage',
  'tab-invariants': '#invariants',
  'tab-tok': '#tok',
};

function resolveHashToTab(hash: string): {
  tab: TabId;
  poolModal: 'oauth' | 'import' | 'strategy' | null;
} {
  if (hash === '#codebuddy') return { tab: 'tab-pool', poolModal: 'oauth' };
  if (hash === '#quota-floor') return { tab: 'tab-pool', poolModal: 'strategy' };
  if (hash === '#tab-checkin') return { tab: 'tab-checkin', poolModal: null };
  const entry = (Object.entries(CHAPTER_HASHES) as Array<[TabId, string]>).find(
    ([, h]) => h === hash,
  );
  return { tab: entry ? entry[0] : 'tab-overview', poolModal: null };
}

function extractModelList(data: unknown): ModelItem[] {
  const arr = Array.isArray(data)
    ? data
    : Array.isArray((data as { models?: unknown })?.models)
      ? (data as { models: unknown[] }).models
      : Array.isArray((data as { data?: unknown })?.data)
        ? (data as { data: unknown[] }).data
        : [];
  return arr.map((m) => (typeof m === 'string' ? { id: m, modelId: m } : (m as ModelItem)));
}

function ConsoleApp() {
  const { theme, isDark, setTheme, toggleDarkMode } = useTheme();
  const { showToast } = useToast();

  const initialRoute = useMemo(() => resolveHashToTab(window.location.hash), []);
  const [activeTab, setActiveTab] = useState<TabId>(initialRoute.tab);
  const [poolInitialModal, setPoolInitialModal] = useState<
    'oauth' | 'import' | 'strategy' | null
  >(initialRoute.poolModal);
  const [poolInitialFilter, setPoolInitialFilter] = useState<'all' | 'ok' | 'issue' | 'disabled' | null>(null);
  const [refreshTick, setRefreshTick] = useState(0);

  const [status, setStatus] = useState<StatusPayload | null>(null);
  const [statusError, setStatusError] = useState('');
  const [refreshing, setRefreshing] = useState(false);
  const [switchingSite, setSwitchingSite] = useState(false);
  const [switchingProduct, setSwitchingProduct] = useState(false);

  const [usageByAccount, setUsageByAccount] = useState<Record<string, AccountUsageItem>>({});
  const [concurrencyRaw, setConcurrencyRaw] = useState<ConcurrencyPayload | null>(null);
  const [egressState, setEgressState] = useState<ProxiesPayload>({
    candidates: [],
    used: [],
    direct: 0,
    total: 0,
  });

  const [clientConfig, setClientConfig] = useState<ClientConfigPayload | null>(null);
  const [checkinConfig, setCheckinConfig] = useState<CheckinConfigPayload | null>(null);
  const [checkinLoadError, setCheckinLoadError] = useState('');

  const [modelsScope, setModelsScope] = useState<'domestic' | 'global' | 'both'>('domestic');
  const [modelsList, setModelsList] = useState<ModelItem[]>([]);
  const [modelsRaw, setModelsRaw] = useState('[]');

  const [routesPayload, setRoutesPayload] = useState<ModelRoutesPayload | null>(null);
  const [routesError, setRoutesError] = useState('');

  const [quotaFloorSaved, setQuotaFloorSaved] = useState<number | null>(null);
  const [spMode, setSpMode] = useState<SystemPromptMode>('auto');

  const statusSeqRef = useRef(0);
  const usageKickRef = useRef(false);
  const activeSiteRef = useRef<Site>('domestic');

  const uiRevision = useMemo(() => {
    const meta = document.querySelector('meta[name="cbp-ui-revision"]');
    return meta?.getAttribute('content') || '';
  }, []);

  const selectTab = useCallback((tab: TabId, pushHash = true) => {
    setActiveTab(tab);
    const targetHash = CHAPTER_HASHES[tab];
    if (pushHash && window.location.hash !== targetHash) {
      window.history.pushState(null, '', targetHash);
    }
  }, []);

  useEffect(() => {
    const onHash = () => {
      const resolved = resolveHashToTab(window.location.hash);
      setActiveTab(resolved.tab);
      if (resolved.poolModal) setPoolInitialModal(resolved.poolModal);
    };
    window.addEventListener('hashchange', onHash);
    window.addEventListener('popstate', onHash);
    return () => {
      window.removeEventListener('hashchange', onHash);
      window.removeEventListener('popstate', onHash);
    };
  }, []);

  const refreshConcurrency = useCallback(async () => {
    try {
      const data = await apiRequest<{ concurrency?: ConcurrencyPayload } & ConcurrencyPayload>(
        '/direct-admin/api/concurrency',
      );
      setConcurrencyRaw(data.concurrency || data);
    } catch {
      // 并发快照失败不影响主状态页
    }
  }, []);

  const refreshEgress = useCallback(async () => {
    try {
      const data = await apiRequest<ProxiesPayload>('/direct-admin/api/codebuddy/proxies');
      setEgressState({
        candidates: data.candidates || [],
        used: data.used || [],
        direct: data.direct || 0,
        total: data.total || 0,
      });
    } catch {
      // 出口池为附加信息，失败不打断主流程
    }
  }, []);

  const fetchAccountUsage = useCallback(
    async (accountId: string, silent = false) => {
      try {
        const result = await apiRequest<AccountUsageItem>(
          `/direct-admin/api/codebuddy/accounts/${encodeURIComponent(accountId)}/usage`,
        );
        setUsageByAccount((prev) => ({ ...prev, [accountId]: result }));
        if (!silent) {
          const credits = result.credits || {};
          showToast(credits.display || credits.label || '余额已更新', 'success');
        }
      } catch (e: unknown) {
        const msg = e instanceof Error ? e.message : String(e);
        setUsageByAccount((prev) => ({
          ...prev,
          [accountId]: { accountId, error: msg },
        }));
        if (!silent) showToast(`查余额失败：${msg}`, 'error');
      }
    },
    [showToast],
  );

  const applyStatusPayload = useCallback(
    (data: StatusPayload) => {
      setStatus(data);
      setStatusError('');
      const site = normalizeSite(data.poolSite || data.config?.poolSite || data.config?.site || 'global');
      activeSiteRef.current = site;
      if (data.concurrency) {
        setConcurrencyRaw((prev) => prev || data.concurrency || null);
      }
      if (Array.isArray(data.accountUsages)) {
        setUsageByAccount((prev) => {
          const next = { ...prev };
          for (const item of data.accountUsages || []) {
            if (item?.accountId) next[item.accountId] = item;
          }
          return next;
        });
      }
      const primary = data.accounts?.primary || data.accounts?.accounts?.[0] || null;
      if (primary?.id && primary.hasCredentials && !usageKickRef.current) {
        usageKickRef.current = true;
        fetchAccountUsage(primary.id, true).catch(() => {});
      }
    },
    [fetchAccountUsage],
  );

  const refreshStatus = useCallback(
    async (fresh = false) => {
      const seq = ++statusSeqRef.current;
      if (fresh) setRefreshing(true);
      try {
        const data = await apiRequest<StatusPayload>(
          '/direct-admin/api/status' + (fresh ? '?fresh=1' : ''),
        );
        if (seq !== statusSeqRef.current) return;
        applyStatusPayload(data);
        setRefreshTick((t) => t + 1);
        if (fresh && data.creditsRefreshed) {
          showToast('额度已从上游刷新', 'success');
        }
        refreshConcurrency();
        refreshEgress();
      } catch (e: unknown) {
        if (seq !== statusSeqRef.current) return;
        const msg = e instanceof Error ? e.message : String(e);
        setStatusError(msg);
        if (fresh) showToast(`刷新失败：${msg}`, 'error');
      } finally {
        if (fresh) setRefreshing(false);
      }
    },
    [applyStatusPayload, refreshConcurrency, refreshEgress, showToast],
  );

  const refreshModels = useCallback(async (scope?: 'domestic' | 'global' | 'both') => {
    const target = scope || activeSiteRef.current || 'domestic';
    setModelsScope(target);
    if (target === 'both') {
      const [dom, glob] = await Promise.all([
        apiRequest('/direct-admin/api/codebuddy/models?fresh=1&site=domestic').catch((e: unknown) => ({
          error: e instanceof Error ? e.message : String(e),
        })),
        apiRequest('/direct-admin/api/codebuddy/models?fresh=1&site=global').catch((e: unknown) => ({
          error: e instanceof Error ? e.message : String(e),
        })),
      ]);
      const merged = mergeModelLists(dom, glob);
      setModelsRaw(JSON.stringify({ domestic: dom, global: glob }, null, 2));
      setModelsList(merged);
      return;
    }
    const data = await apiRequest(
      `/direct-admin/api/codebuddy/models?fresh=1&site=${encodeURIComponent(target)}`,
    );
    setModelsRaw(JSON.stringify(data, null, 2));
    setModelsList(extractModelList(data));
  }, []);

  const refreshClientConfig = useCallback(async () => {
    const data = await apiRequest<ClientConfigPayload>('/direct-admin/api/client-config');
    setClientConfig(data);
    return data;
  }, []);

  // 初始化并行拉取各模块配置
  useEffect(() => {
    refreshStatus(false);
    refreshClientConfig().catch((e) =>
      showToast(e instanceof Error ? e.message : String(e), 'error'),
    );
    apiRequest<CheckinConfigPayload>('/direct-admin/api/checkin-config')
      .then((d) => {
        setCheckinConfig(d);
        setCheckinLoadError('');
      })
      .catch((e: unknown) => setCheckinLoadError(e instanceof Error ? e.message : String(e)));
    apiRequest<ModelRoutesPayload>('/direct-admin/api/model-routes')
      .then((d) => {
        setRoutesPayload(d);
        setRoutesError('');
      })
      .catch((e: unknown) => setRoutesError(e instanceof Error ? e.message : String(e)));
    apiRequest<{ quotaFloor?: number }>('/direct-admin/api/quota-floor')
      .then((d) => setQuotaFloorSaved(d.quotaFloor != null ? Number(d.quotaFloor) : 0))
      .catch(() => {});
    apiRequest<SystemPromptOverview>('/direct-admin/api/system-prompt')
      .then((d) => {
        if (d.mode) setSpMode(d.mode);
      })
      .catch(() => {});
    refreshModels().catch(() => {});
  }, []);

  // 15 秒常规轮询（严禁传 fresh=true）
  useEffect(() => {
    const timer = window.setInterval(() => {
      if (document.visibilityState === 'hidden') return;
      refreshStatus(false);
    }, 15_000);
    return () => window.clearInterval(timer);
  }, [refreshStatus]);

  const handleSwitchSite = async (site: Site) => {
    setSwitchingSite(true);
    try {
      const data = await apiRequest<StatusPayload>('/direct-admin/api/pool-site', {
        method: 'POST',
        body: JSON.stringify({ site }),
      });
      applyStatusPayload(data);
      if (data.note) showToast(data.note);
      refreshModels(site).catch(() => {});
    } catch (e: unknown) {
      showToast(e instanceof Error ? e.message : String(e), 'error');
    } finally {
      setSwitchingSite(false);
    }
  };

  const handleSwitchProduct = async (product: Product) => {
    setSwitchingProduct(true);
    try {
      const data = await apiRequest<StatusPayload>('/direct-admin/api/pool-product', {
        method: 'POST',
        body: JSON.stringify({ product }),
      });
      applyStatusPayload(data);
      if (data.note) showToast(data.note);
      refreshModels(activeSiteRef.current).catch(() => {});
    } catch (e: unknown) {
      showToast(e instanceof Error ? e.message : String(e), 'error');
    } finally {
      setSwitchingProduct(false);
    }
  };

  // 派生全局顶部展示态
  const poolSite = normalizeSite(
    status?.poolSite || status?.config?.poolSite || status?.config?.site || 'global',
  );
  const poolProduct = normalizeProduct(
    status?.poolProduct || status?.product || status?.config?.poolProduct || status?.config?.product || 'codebuddy',
  );
  const concurrency = useMemo(() => summarizeConcurrency(concurrencyRaw), [concurrencyRaw]);
  const allAccounts = status?.accounts?.accounts || [];

  const { healthOk, healthText, healthTitle } = useMemo(() => {
    if (statusError) {
      return { healthOk: false, healthText: '无法连接', healthTitle: statusError };
    }
    if (!status) {
      return { healthOk: true, healthText: '检查中', healthTitle: '正在获取网关状态' };
    }
    const loggedIn = !!status.accounts?.loggedIn;
    const up = status.upstream || {};
    let ok = !!status.ok;
    let text = ok ? (loggedIn ? '服务正常' : '服务正常 · 号池未登录') : '状态异常';
    if (up.probed && up.ok === false) {
      ok = false;
      text = '上游不可达';
    } else if (up.probed && up.ok) {
      text = '服务正常 · 上游可达';
    }
    return { healthOk: ok, healthText: text, healthTitle: up.message || text };
  }, [status, statusError]);

  // 侧边栏「需要关注」角标计数
  const attentionCount = useMemo(() => {
    return allAccounts
      .filter((a) => a.enabled && normalizeSite(a.site) === poolSite)
      .filter((a) => {
        if (!(a.loggedIn && a.hasCredentials) || a.tokenExpired || a.lastError) return true;
        if (computeAccountStates(a).length > 0) return true;
        const u = usageByAccount[a.id];
        if (u?.credits && computeCreditLevel(u) !== 'good') return true;
        return false;
      }).length;
  }, [allAccounts, poolSite, usageByAccount]);

  const siteAccountsCount = allAccounts.filter((a) => normalizeSite(a.site) === poolSite).length;
  const siteEnabledCount = allAccounts.filter(
    (a) => a.enabled && normalizeSite(a.site) === poolSite,
  ).length;
  const version = status?.version || status?.build?.version || 'dev';

  return (
    <>
      <AppShell
        activeTab={activeTab}
        onSelectTab={(t) => selectTab(t, true)}
        healthOk={healthOk}
        healthText={healthText}
        healthTitle={healthTitle}
        transport={status?.transport || 'protocol_direct'}
        poolSite={poolSite}
        poolProduct={poolProduct}
        onSwitchSite={handleSwitchSite}
        onSwitchProduct={handleSwitchProduct}
        switchingSite={switchingSite}
        switchingProduct={switchingProduct}
        onRefreshStatus={() => refreshStatus(true)}
        refreshing={refreshing}
        attentionCount={attentionCount}
        poolCountLabel={siteAccountsCount ? `${siteEnabledCount}/${siteAccountsCount}` : ''}
        checkinAutoEnabled={!!checkinConfig?.auto}
        spMode={spMode}
        version={version}
        uiRevision={uiRevision}
        theme={theme}
        isDark={isDark}
        onSetTheme={setTheme}
        onToggleDark={toggleDarkMode}
      >
        {activeTab === 'tab-overview' && (
          <OverviewPage
            active={activeTab === 'tab-overview'}
            refreshTick={refreshTick}
            status={status}
            activeSite={poolSite}
            usageByAccount={usageByAccount}
            concurrency={concurrency}
            onNavigate={(t) => selectTab(t, true)}
            onNavigatePoolIssues={() => {
              setPoolInitialFilter('issue');
              selectTab('tab-pool', true);
            }}
          />
        )}

        {activeTab === 'tab-pool' && (
          <PoolPage
            status={status}
            activeSite={poolSite}
            usageByAccount={usageByAccount}
            concurrency={concurrency}
            models={modelsList}
            quotaFloorSaved={quotaFloorSaved}
            onQuotaFloorSavedChange={(v) => setQuotaFloorSaved(v)}
            egressState={egressState}
            onRefreshEgress={refreshEgress}
            onRefreshStatus={refreshStatus}
            onFetchAccountUsage={async (id, silent) => {
              await fetchAccountUsage(id, silent);
              await refreshStatus(false);
            }}
            onNavigate={(t) => selectTab(t, true)}
            onSwitchSite={handleSwitchSite}
            initialModal={poolInitialModal}
            onClearInitialModal={() => setPoolInitialModal(null)}
            initialFilter={poolInitialFilter}
            onClearInitialFilter={() => setPoolInitialFilter(null)}
          />
        )}

        {activeTab === 'tab-checkin' && (
          <CheckinPage
            accounts={allAccounts}
            config={checkinConfig}
            loadError={checkinLoadError}
            onConfigSaved={(next) => setCheckinConfig(next)}
          />
        )}

        {activeTab === 'tab-client' && (
          <ClientPage
            config={clientConfig}
            accounts={allAccounts}
            onRefresh={refreshClientConfig}
            onConfigUpdated={(next) => setClientConfig(next)}
          />
        )}

        {activeTab === 'tab-models' && (
          <ModelsPage
            activeSite={poolSite}
            accounts={allAccounts}
            quotaFloorSaved={quotaFloorSaved || 0}
            models={modelsList}
            modelsRaw={modelsRaw}
            modelsScope={modelsScope}
            onSetModelsScope={setModelsScope}
            onRefreshModels={refreshModels}
            routesPayload={routesPayload}
            routesError={routesError}
            onRoutesSaved={(nextRules: ModelRouteRule[], nextTitleRoute?: TitleRoute) =>
              setRoutesPayload((prev) =>
                prev
                  ? {
                      ...prev,
                      rules: nextRules,
                      ...(nextTitleRoute ? { titleRoute: nextTitleRoute } : {}),
                    }
                  : prev,
              )
            }
          />
        )}

        {activeTab === 'tab-system-prompt' && (
          <SystemPromptPage
            active={activeTab === 'tab-system-prompt'}
            onModeSynced={setSpMode}
          />
        )}

        {activeTab === 'tab-usage' && <UsagePage active={activeTab === 'tab-usage'} />}

        {activeTab === 'tab-invariants' && (
          <InvariantsPage active={activeTab === 'tab-invariants'} />
        )}

        {activeTab === 'tab-tok' && <TokPage active={activeTab === 'tab-tok'} />}
      </AppShell>

      <ToastViewport />
    </>
  );
}

export default function App() {
  return (
    <ToastProvider>
      <ConfirmProvider>
        <ConsoleApp />
      </ConfirmProvider>
    </ToastProvider>
  );
}
