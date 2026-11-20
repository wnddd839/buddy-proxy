import { useEffect, useMemo, useState } from 'react';
import {
  CalendarCheck2,
  ChevronDown,
  ChevronUp,
  ExternalLink,
  FlaskConical,
  Globe,
  LayoutGrid,
  List,
  MoreHorizontal,
  Plus,
  RefreshCw,
  Search,
  SlidersHorizontal,
  Trash2,
  Upload,
} from 'lucide-react';
import type {
  AccountSummary,
  AccountUsageItem,
  ImportAccountsResult,
  ModelItem,
  OAuthResponse,
  PoolChatTestResult,
  PoolCheckinResult,
  ProxiesPayload,
  ProxyCheckResult,
  Site,
  StatusPayload,
} from '@/types/api';
import {
  computeAccountStates,
  computeCreditLevel,
  computeExpirySummary,
  type ConcurrencySummary,
} from '@/lib/domain';
import {
  bareModelId,
  formatNextCheckin,
  formatTime,
  normalizeSite,
  proxyHost,
  safeHttpUrl,
  siteLabel,
} from '@/lib/format';
import { apiRequest } from '@/lib/api';
import { useToast } from '@/hooks/useToast';
import { useConfirm } from '@/hooks/useConfirm';
import { Button } from '@/components/ui/Button';
import { Card, EmptyState, PageHeader } from '@/components/ui/Card';
import { Modal } from '@/components/ui/Modal';
import { SegmentedControl } from '@/components/ui/SegmentedControl';
import { SlideDrawer } from '@/components/ui/SlideDrawer';
import { StatusBadge } from '@/components/ui/StatusBadge';
import type { TabId } from '@/components/layout/AppShell';

interface PoolPageProps {
  status: StatusPayload | null;
  activeSite: Site;
  usageByAccount: Record<string, AccountUsageItem>;
  concurrency: ConcurrencySummary;
  models: ModelItem[];
  quotaFloorSaved: number | null;
  onQuotaFloorSavedChange: (v: number) => void;
  egressState: ProxiesPayload;
  onRefreshEgress: () => Promise<void>;
  onRefreshStatus: (fresh?: boolean) => Promise<void>;
  onFetchAccountUsage: (accountId: string, silent?: boolean) => Promise<void>;
  onNavigate: (tab: TabId) => void;
  initialModal?: 'oauth' | 'import' | 'strategy' | null;
  onClearInitialModal?: () => void;
}

type StatusFilter = 'all' | 'ok' | 'issue' | 'disabled';
type SortKey = 'default' | 'credits-desc' | 'credits-asc';
type LayoutMode = 'grid' | 'table';

export function PoolPage({
  status,
  activeSite,
  usageByAccount,
  concurrency,
  models,
  quotaFloorSaved,
  onQuotaFloorSavedChange,
  egressState,
  onRefreshEgress,
  onRefreshStatus,
  onFetchAccountUsage,
  onNavigate,
  initialModal,
  onClearInitialModal,
}: PoolPageProps) {
  const { showToast } = useToast();
  const { confirm } = useConfirm();

  const [search, setSearch] = useState('');
  const [statusFilter, setStatusFilter] = useState<StatusFilter>('all');
  const [sortBy, setSortBy] = useState<SortKey>('default');
  const [layout, setLayout] = useState<LayoutMode>('grid');
  const [testModel, setTestModel] = useState('');
  const [expandedExpiry, setExpandedExpiry] = useState<Record<string, boolean>>({});
  const [openMenuId, setOpenMenuId] = useState<string | null>(null);
  const [busyAction, setBusyAction] = useState('');

  // 批量操作结果
  const [checkinBusy, setCheckinBusy] = useState(false);
  const [checkinLog, setCheckinLog] = useState('');
  const [poolTestBusy, setPoolTestBusy] = useState(false);
  const [poolTestLog, setPoolTestLog] = useState('');

  // 添加账号弹窗（OAuth + 批量导入双 Tab）
  const [addModalOpen, setAddModalOpen] = useState(false);
  const [addTab, setAddTab] = useState<'oauth' | 'import'>('oauth');
  const [oauthSite, setOauthSite] = useState<Site>(activeSite);
  const [oauthSiteTouched, setOauthSiteTouched] = useState(false);
  const [oauthLabel, setOauthLabel] = useState('CodeBuddy OAuth');
  const [oauthLaunchUrl, setOauthLaunchUrl] = useState('#');
  const [oauthMsg, setOauthMsg] = useState('选择站点并点击「开始认证」，授权完成后点击「检查登录」自动入池。');
  const [oauthRaw, setOauthRaw] = useState('idle');
  const [oauthBusy, setOauthBusy] = useState(false);

  const [importText, setImportText] = useState('');
  const [importSite, setImportSite] = useState('');
  const [importResult, setImportResult] = useState('');
  const [importBusy, setImportBusy] = useState(false);

  // 选号门槛 + 出口代理池右侧抽屉
  const [strategyDrawerOpen, setStrategyDrawerOpen] = useState(false);
  const [quotaFloorInput, setQuotaFloorInput] = useState(
    quotaFloorSaved != null ? String(quotaFloorSaved) : '0',
  );
  const [quotaFloorSaving, setQuotaFloorSaving] = useState(false);
  const [quotaFloorError, setQuotaFloorError] = useState('');
  const [egressNewUrl, setEgressNewUrl] = useState('');
  const [egressNewLabel, setEgressNewLabel] = useState('');
  const [egressBusy, setEgressBusy] = useState('');

  // 单账号设置出口弹窗
  const [egressModalAccount, setEgressModalAccount] = useState<AccountSummary | null>(null);
  const [egressSelect, setEgressSelect] = useState('');
  const [egressCustom, setEgressCustom] = useState('');
  const [egressCheckRaw, setEgressCheckRaw] = useState('');
  const [egressModalBusy, setEgressModalBusy] = useState(false);

  // 跟随 activeSite 同步默认 OAuth 站点（用户手动选过之后不覆盖）
  useEffect(() => {
    if (!oauthSiteTouched) setOauthSite(activeSite);
  }, [activeSite, oauthSiteTouched]);

  // 同步外部加载好的 quotaFloorSaved 到输入框
  useEffect(() => {
    if (quotaFloorSaved != null) {
      setQuotaFloorInput(String(quotaFloorSaved));
    }
  }, [quotaFloorSaved]);

  // 切换站点时清空上次批量操作输出
  useEffect(() => {
    if (!checkinBusy) setCheckinLog('');
    if (!poolTestBusy) setPoolTestLog('');
  }, [activeSite, checkinBusy, poolTestBusy]);

  // 响应 #codebuddy / #quota-floor 等深度锚点
  useEffect(() => {
    if (!initialModal) return;
    if (initialModal === 'oauth') {
      setAddTab('oauth');
      setAddModalOpen(true);
    } else if (initialModal === 'import') {
      setAddTab('import');
      setAddModalOpen(true);
    } else if (initialModal === 'strategy') {
      setStrategyDrawerOpen(true);
    }
    onClearInitialModal?.();
  }, [initialModal, onClearInitialModal]);

  // 点击空白处收起「更多」菜单
  useEffect(() => {
    if (!openMenuId) return;
    const onDocClick = () => setOpenMenuId(null);
    window.addEventListener('click', onDocClick);
    return () => window.removeEventListener('click', onDocClick);
  }, [openMenuId]);

  const allAccounts = status?.accounts?.accounts || [];
  const siteAccounts = useMemo(
    () => allAccounts.filter((a) => normalizeSite(a.site) === activeSite),
    [allAccounts, activeSite],
  );

  const testModelOptions = useMemo(() => {
    const seen = new Set<string>();
    const list: Array<{ id: string; label: string }> = [];
    for (const m of models) {
      const id = bareModelId(m.id || m.modelId || m.upstreamId || m.name);
      if (!id || id === 'auto' || seen.has(id)) continue;
      seen.add(id);
      const credits = m.credits ? String(m.credits).replace(/\s*credits$/i, '') : '';
      list.push({ id, label: credits ? `${id} · ${credits}` : id });
    }
    return list;
  }, [models]);

  const counts = useMemo(() => {
    let ok = 0;
    let issue = 0;
    let disabled = 0;
    for (const a of siteAccounts) {
      if (!a.enabled) {
        disabled++;
        continue;
      }
      const states = computeAccountStates(a);
      const logged = a.loggedIn && a.hasCredentials;
      if (!logged || a.tokenExpired || states.length > 0 || a.lastError) issue++;
      else ok++;
    }
    return { all: siteAccounts.length, ok, issue, disabled };
  }, [siteAccounts]);

  const visibleAccounts = useMemo(() => {
    const q = search.trim().toLowerCase();
    const filtered = siteAccounts.filter((a) => {
      if (statusFilter === 'disabled' && a.enabled) return false;
      if (statusFilter !== 'all' && statusFilter !== 'disabled') {
        if (!a.enabled) return false;
        const states = computeAccountStates(a);
        const logged = a.loggedIn && a.hasCredentials;
        const hasIssue = !logged || a.tokenExpired || states.length > 0 || !!a.lastError;
        if (statusFilter === 'ok' && hasIssue) return false;
        if (statusFilter === 'issue' && !hasIssue) return false;
      }
      if (q) {
        const hay = `${a.label || ''} ${a.id} ${a.userNickname || ''} ${a.userName || ''} ${a.proxy || ''}`.toLowerCase();
        if (!hay.includes(q)) return false;
      }
      return true;
    });

    if (sortBy === 'default') return filtered;
    const creditScore = (a: AccountSummary) => {
      const u = usageByAccount[a.id];
      if (u?.credits?.unlimited || a.quotaUnlimited) return Number.MAX_SAFE_INTEGER;
      if (u?.credits?.remaining != null) return Number(u.credits.remaining);
      if (a.quotaRemaining != null) return Number(a.quotaRemaining);
      return -1;
    };
    return filtered.slice().sort((x, y) => {
      const dx = creditScore(x);
      const dy = creditScore(y);
      return sortBy === 'credits-desc' ? dy - dx : dx - dy;
    });
  }, [siteAccounts, statusFilter, search, sortBy, usageByAccount]);

  // 批量签到
  const handlePoolCheckin = async () => {
    setCheckinBusy(true);
    try {
      const data = await apiRequest<PoolCheckinResult>('/direct-admin/api/codebuddy/checkin', {
        method: 'POST',
        body: JSON.stringify({ site: activeSite }),
      });
      const s = data.summary || {};
      const parts: string[] = [];
      if (s.checkedIn) parts.push(`新签 ${s.checkedIn}`);
      if (s.alreadyDone) parts.push(`已签 ${s.alreadyDone}`);
      if (s.skipped) parts.push(`跳过 ${s.skipped}`);
      if (s.unsupported) parts.push(`不支持 ${s.unsupported}`);
      if (s.failed) parts.push(`失败 ${s.failed}`);
      showToast(
        parts.length ? parts.join(' · ') : data.note || '当前号池没有可签到的账号',
        data.ok ? 'success' : 'error',
      );
      for (const item of data.results || []) {
        if (item.ok && item.accountId && !item.alreadyCheckedIn) {
          await onFetchAccountUsage(item.accountId, true);
        }
      }
      await onRefreshStatus();
      if (data.results?.length) {
        setCheckinLog(
          data.results
            .map((item) => {
              const name = item.label || item.accountId || '账号';
              if (!item.supported) return `${name}：${item.message || '不支持'}`;
              if (item.ok && item.alreadyCheckedIn) return `${name}：今日已签`;
              if (item.ok) return `${name}：${item.message || `+${item.rewardCredits || 0} 积分`}`;
              return `${name}：${item.message || '失败'}`;
            })
            .join('\n'),
        );
      }
    } catch (e: unknown) {
      showToast(`签到失败：${e instanceof Error ? e.message : String(e)}`, 'error');
    } finally {
      setCheckinBusy(false);
    }
  };

  // 批量 Chat 探测
  const handlePoolTest = async () => {
    setPoolTestBusy(true);
    try {
      const body: Record<string, string> = { site: activeSite };
      if (testModel) body.model = testModel;
      const data = await apiRequest<PoolChatTestResult>('/direct-admin/api/codebuddy/test', {
        method: 'POST',
        body: JSON.stringify(body),
      });
      const s = data.summary || {};
      const parts: string[] = [];
      if (s.passed) parts.push(`通过 ${s.passed}`);
      if (s.failed) parts.push(`失败 ${s.failed}`);
      if (s.skipped) parts.push(`跳过 ${s.skipped}`);
      const m = data.model ? ` · ${data.model}` : '';
      showToast(
        (parts.length ? parts.join(' · ') : data.note || '无可测试账号') + m,
        data.ok ? 'success' : 'error',
      );
      if (data.results?.length) {
        setPoolTestLog(
          data.results
            .map((item) => {
              const name = item.label || item.accountId || '账号';
              if (item.ok) return `${name}：ok · ${item.latencyMs || 0}ms · ${item.model || ''}`;
              return `${name}：${item.message || '失败'}`;
            })
            .join('\n'),
        );
      }
    } catch (e: unknown) {
      showToast(`批量测试失败：${e instanceof Error ? e.message : String(e)}`, 'error');
    } finally {
      setPoolTestBusy(false);
    }
  };

  // 单账号操作
  const handleAccountAction = async (
    account: AccountSummary,
    action: 'test' | 'usage' | 'toggle' | 'refresh' | 'delete',
  ) => {
    setOpenMenuId(null);
    const key = `${account.id}:${action}`;
    setBusyAction(key);
    try {
      if (action === 'usage') {
        await onFetchAccountUsage(account.id, false);
        return;
      }
      if (action === 'test') {
        const body: Record<string, string> = {};
        if (testModel) body.model = testModel;
        const res = await apiRequest<{
          ok?: boolean;
          label?: string;
          accountId?: string;
          latencyMs?: number;
          model?: string;
          message?: string;
        }>(`/direct-admin/api/codebuddy/accounts/${encodeURIComponent(account.id)}/test`, {
          method: 'POST',
          body: JSON.stringify(body),
        });
        if (res.ok) {
          showToast(
            `${res.label || res.accountId || '账号'} · ${res.latencyMs || 0}ms · ${res.model || ''}`,
            'success',
          );
        } else {
          showToast(`测试失败：${res.message || 'unknown'}`, 'error');
        }
        return;
      }
      if (action === 'delete') {
        const ok = await confirm({
          title: `删除账号「${account.label || account.id}」？`,
          description: '删除后本地凭据将被移除，绑定该账号的会话将自动切换或失效。该操作不可撤销。',
          confirmLabel: '确认删除',
          danger: true,
        });
        if (!ok) return;
        await apiRequest(`/direct-admin/api/codebuddy/accounts/${encodeURIComponent(account.id)}`, {
          method: 'DELETE',
        });
        showToast('账号已删除');
      } else if (action === 'toggle') {
        await apiRequest(
          `/direct-admin/api/codebuddy/accounts/${encodeURIComponent(account.id)}/${
            account.enabled ? 'disable' : 'enable'
          }`,
          { method: 'POST', body: '{}' },
        );
        showToast(account.enabled ? '账号已禁用' : '账号已启用');
      } else if (action === 'refresh') {
        await apiRequest(
          `/direct-admin/api/codebuddy/accounts/${encodeURIComponent(account.id)}/refresh-token`,
          { method: 'POST', body: '{}' },
        );
        showToast('Token 刷新成功');
      }
      await onRefreshStatus();
    } catch (e: unknown) {
      showToast(e instanceof Error ? e.message : String(e), 'error');
    } finally {
      setBusyAction('');
    }
  };

  // OAuth 认证
  const handleStartOAuth = async () => {
    setOauthBusy(true);
    setOauthMsg('正在创建登录会话…');
    try {
      const data = await apiRequest<OAuthResponse>('/direct-admin/api/codebuddy/oauth/start', {
        method: 'POST',
        body: JSON.stringify({ site: oauthSite, label: oauthLabel, reuseExisting: false }),
      });
      const session = data.session || {};
      const login = data.login || {};
      const st = session.status || login.status || (data.ok ? 'ok' : 'idle');
      const err = session.error || login.message || '';
      setOauthMsg(
        `状态：${st}${session.label ? ` · ${session.label}` : ''}${
          session.site ? ` · ${session.site}` : ''
        }${err ? ` · ${err}` : ''}`,
      );
      setOauthRaw(JSON.stringify(data, null, 2));
      const url = login.url || session.url || '#';
      const launch = login.launchUrl || session.launchUrl || url;
      setOauthLaunchUrl(launch);
      if (url && url !== '#') window.open(url, '_blank', 'noopener');
    } catch (e: unknown) {
      const msg = e instanceof Error ? e.message : String(e);
      setOauthMsg(msg);
      setOauthRaw(msg);
    } finally {
      setOauthBusy(false);
    }
  };

  const handlePollOAuth = async () => {
    setOauthBusy(true);
    setOauthMsg('正在检查登录状态…');
    try {
      const data = await apiRequest<OAuthResponse>('/direct-admin/api/codebuddy/oauth/poll', {
        method: 'POST',
        body: '{}',
      });
      const session = data.session || {};
      const login = data.login || {};
      const st = session.status || login.status || (data.ok ? 'ok' : 'idle');
      const err = session.error || login.message || '';
      setOauthMsg(
        `状态：${st}${session.label ? ` · ${session.label}` : ''}${
          session.site ? ` · ${session.site}` : ''
        }${err ? ` · ${err}` : ''}`,
      );
      setOauthRaw(JSON.stringify(data, null, 2));
      await onRefreshStatus();
      if (data.ok) showToast('已检查登录态并同步账号池');
    } catch (e: unknown) {
      const msg = e instanceof Error ? e.message : String(e);
      setOauthMsg(msg);
      setOauthRaw(msg);
    } finally {
      setOauthBusy(false);
    }
  };

  // 批量 JSON 导入
  const handleRunImport = async () => {
    const text = importText.trim();
    if (!text) {
      showToast('请先粘贴账号 JSON', 'error');
      return;
    }
    setImportBusy(true);
    setImportResult('正在导入…');
    try {
      const data = await apiRequest<ImportAccountsResult>(
        '/direct-admin/api/codebuddy/accounts/import',
        {
          method: 'POST',
          body: JSON.stringify({ text, site: importSite }),
        },
      );
      const lines = (data.results || []).map((r) => {
        const tag = r.status === 'added' ? '新增' : r.status === 'updated' ? '更新' : '失败';
        return `  [${tag}] ${r.label || r.id || `#${r.index}`}${r.site ? ` · ${r.site}` : ''}${
          r.error ? ` — ${r.error}` : ''
        }`;
      });
      setImportResult(
        `新增 ${data.added || 0} / 更新 ${data.updated || 0} / 失败 ${data.failed || 0}\n${lines.join('\n')}`,
      );
      showToast(
        `导入完成：新增 ${data.added || 0}，更新 ${data.updated || 0}${
          data.failed ? `，失败 ${data.failed}` : ''
        }`,
        data.failed ? 'error' : 'success',
      );
      await onRefreshStatus();
      if (data.ok) {
        window.setTimeout(() => {
          setAddModalOpen(false);
          setImportText('');
          setImportResult('');
        }, 700);
      }
    } catch (e: unknown) {
      const msg = e instanceof Error ? e.message : String(e);
      setImportResult(`导入失败：${msg}`);
      showToast(`导入失败：${msg}`, 'error');
    } finally {
      setImportBusy(false);
    }
  };

  // 选号门槛保存
  const quotaFloorDirty =
    quotaFloorSaved != null && Number(quotaFloorInput) !== quotaFloorSaved;

  const handleSaveQuotaFloor = async () => {
    const v = Number(quotaFloorInput);
    if (!Number.isFinite(v) || v < 0) {
      showToast('请填 0 或正数（0 = 不启用）', 'error');
      return;
    }
    setQuotaFloorSaving(true);
    setQuotaFloorError('');
    try {
      const data = await apiRequest<{ quotaFloor?: number }>('/direct-admin/api/quota-floor', {
        method: 'POST',
        body: JSON.stringify({ quotaFloor: v }),
      });
      const saved = data.quotaFloor != null ? Number(data.quotaFloor) : v;
      onQuotaFloorSavedChange(saved);
      setQuotaFloorInput(String(saved));
      showToast(saved > 0 ? `已保存：积分下限 ${saved}` : '已保存：积分下限已关闭', 'success');
    } catch (e: unknown) {
      const msg = e instanceof Error ? e.message : String(e);
      setQuotaFloorError(`保存失败，输入已保留：${msg}`);
      showToast(msg, 'error');
    } finally {
      setQuotaFloorSaving(false);
    }
  };

  // 出口代理池管理
  const proxyCounts = useMemo(() => {
    const m: Record<string, number> = {};
    for (const u of egressState.used || []) m[u.id] = u.count || 0;
    return m;
  }, [egressState.used]);

  const handleAddEgress = async () => {
    const url = egressNewUrl.trim();
    if (!url) {
      showToast('先填出口 URL', 'error');
      return;
    }
    setEgressBusy('add');
    try {
      await apiRequest('/direct-admin/api/codebuddy/proxies/candidates', {
        method: 'POST',
        body: JSON.stringify({ url, label: egressNewLabel.trim() }),
      });
      setEgressNewUrl('');
      setEgressNewLabel('');
      showToast('已加入候选出口', 'success');
      await onRefreshEgress();
    } catch (e: unknown) {
      showToast(e instanceof Error ? e.message : String(e), 'error');
    } finally {
      setEgressBusy('');
    }
  };

  const handleCheckCandidate = async (candidateId: string) => {
    setEgressBusy(`check:${candidateId}`);
    try {
      const res = await apiRequest<ProxyCheckResult>('/direct-admin/api/codebuddy/proxies/check', {
        method: 'POST',
        body: JSON.stringify({ candidateId }),
      });
      if (res.ok) {
        const direct = res.direct?.ok ? ` · 本机 ${res.direct.ip}` : '';
        showToast(`出口 ${res.ip} · ${res.elapsedMs}ms${direct}`, 'success');
      } else {
        showToast(`出口不可用：${res.error || '未知错误'}`, 'error');
      }
    } catch (e: unknown) {
      showToast(e instanceof Error ? e.message : String(e), 'error');
    } finally {
      setEgressBusy('');
    }
  };

  const handleDeleteCandidate = async (candidateId: string) => {
    const ok = await confirm({
      title: '删除候选出口？',
      description: '仅从候选清单移除，已绑定该出口的账号不受影响。',
      confirmLabel: '确认删除',
      danger: true,
    });
    if (!ok) return;
    setEgressBusy(`del:${candidateId}`);
    try {
      await apiRequest('/direct-admin/api/codebuddy/proxies/candidates', {
        method: 'DELETE',
        body: JSON.stringify({ id: candidateId }),
      });
      showToast('已从候选清单删除');
      await onRefreshEgress();
    } catch (e: unknown) {
      showToast(e instanceof Error ? e.message : String(e), 'error');
    } finally {
      setEgressBusy('');
    }
  };

  const handleBalanceEgress = async () => {
    if (!egressState.candidates?.length) {
      showToast('请先添加至少一条候选出口', 'error');
      return;
    }
    const ok = await confirm({
      title: '一键均衡分配出口？',
      description: `将当前「${siteLabel(activeSite)}站」全部已启用账号轮流分配到候选出口，会覆盖现有出口绑定。`,
      confirmLabel: '确认分配',
    });
    if (!ok) return;
    setEgressBusy('balance');
    try {
      const res = await apiRequest<{ assigned?: number; proxies?: number }>(
        '/direct-admin/api/codebuddy/proxies/balance',
        { method: 'POST', body: JSON.stringify({ site: activeSite }) },
      );
      showToast(`已把 ${res.assigned} 个账号均衡分到 ${res.proxies} 条出口`, 'success');
      await onRefreshStatus();
      await onRefreshEgress();
    } catch (e: unknown) {
      showToast(e instanceof Error ? e.message : String(e), 'error');
    } finally {
      setEgressBusy('');
    }
  };

  // 打开单账号出口弹窗
  const openAccountEgressModal = (account: AccountSummary) => {
    setOpenMenuId(null);
    setEgressModalAccount(account);
    setEgressCustom('');
    setEgressCheckRaw('');
    const matched = (egressState.candidates || []).find(
      (c) => account.proxy && c.url === account.proxy,
    );
    if (matched) setEgressSelect(matched.id);
    else if (account.proxy) setEgressSelect('__current__');
    else setEgressSelect('');
  };

  const handleCheckAccountEgress = async () => {
    if (!egressModalAccount) return;
    setEgressCheckRaw('检测中…（走一次真实 HTTPS 请求）');
    setEgressModalBusy(true);
    try {
      let body: Record<string, string>;
      if (egressSelect === '__custom__') body = { url: egressCustom.trim() };
      else if (egressSelect === '__current__') body = { accountId: egressModalAccount.id };
      else if (egressSelect === '') body = { url: '' };
      else body = { candidateId: egressSelect };

      const res = await apiRequest<ProxyCheckResult>('/direct-admin/api/codebuddy/proxies/check', {
        method: 'POST',
        body: JSON.stringify(body),
      });
      if (res.ok) {
        setEgressCheckRaw(
          `出口 IP：${res.ip} · ${res.elapsedMs}ms` +
            (res.direct?.ok
              ? `\n本机 IP：${res.direct.ip} · ${res.direct.elapsedMs}ms`
              : ''),
        );
      } else {
        setEgressCheckRaw(`失败：${res.error || '未知错误'}`);
      }
    } catch (e: unknown) {
      setEgressCheckRaw(e instanceof Error ? e.message : String(e));
    } finally {
      setEgressModalBusy(false);
    }
  };

  const handleSaveAccountEgress = async () => {
    if (!egressModalAccount) return;
    if (egressSelect === '__current__') {
      setEgressModalAccount(null);
      return;
    }
    let body: Record<string, unknown>;
    if (egressSelect === '') body = { clear: true };
    else if (egressSelect === '__custom__') {
      if (!egressCustom.trim()) {
        showToast('自定义出口 URL 不能为空', 'error');
        return;
      }
      body = { proxy: egressCustom.trim() };
    } else {
      body = { candidateId: egressSelect };
    }
    setEgressModalBusy(true);
    try {
      await apiRequest(
        `/direct-admin/api/codebuddy/accounts/${encodeURIComponent(egressModalAccount.id)}/proxy`,
        { method: 'POST', body: JSON.stringify(body) },
      );
      setEgressModalAccount(null);
      showToast('出口已更新', 'success');
      await onRefreshStatus();
      await onRefreshEgress();
    } catch (e: unknown) {
      showToast(e instanceof Error ? e.message : String(e), 'error');
    } finally {
      setEgressModalBusy(false);
    }
  };

  const renderCheckinPill = (a: AccountSummary) => {
    const state = a.checkinToday || 'none';
    const count = Number(a.checkinCount48h || 0);
    let tone: 'ok' | 'err' | 'neutral' = 'neutral';
    let text = '未签到';
    if (state === 'done' || state === 'already') {
      tone = 'ok';
      text = '已签到';
    } else if (state === 'failed') {
      tone = 'err';
      text = '签到失败';
    }
    const nextLabel = formatNextCheckin(a.checkinNextAt);
    const body =
      `${text} · ${count}/2` +
      (nextLabel && state !== 'done' && state !== 'already' ? ` · 下次 ${nextLabel}` : '');
    const title =
      `最近 48 小时成功签到 ${count} 次（目标 2 次）` +
      (state === 'failed' && a.checkinLastError ? `；失败原因：${a.checkinLastError}` : '') +
      (nextLabel ? `；下次自动签到：${nextLabel}` : '');
    return (
      <StatusBadge tone={tone} title={title}>
        {body}
      </StatusBadge>
    );
  };

  return (
    <div id="tab-pool" className="ds-page-enter space-y-5">
      <PageHeader
        title="账号池与授权"
        subtitle={
          <span id="poolSiteHint">
            当前展示「{siteLabel(activeSite)}站」 · 启用{' '}
            <strong className="ds-mono text-[var(--fg)]">
              {status?.accounts?.activeEnabledCount ?? counts.ok + counts.issue}
            </strong>{' '}
            · 无阻塞{' '}
            <strong className="ds-mono text-[var(--fg)]">
              {status?.accounts?.activeAvailableCount ?? counts.ok}
            </strong>
            {quotaFloorSaved && quotaFloorSaved > 0 ? (
              <>
                {' '}
                · 积分下限 <strong className="ds-mono text-[var(--fg)]">{quotaFloorSaved}</strong>
              </>
            ) : null}
          </span>
        }
        actions={
          <>
            <Button
              id="btnGoCheckin"
              variant="ghost"
              size="sm"
              onClick={() => onNavigate('tab-checkin')}
            >
              <CalendarCheck2 className="w-3.5 h-3.5" />
              自动签到排程
            </Button>
            <Button
              id="btnStrategyOpen"
              variant="ghost"
              size="sm"
              onClick={() => setStrategyDrawerOpen(true)}
            >
              <SlidersHorizontal className="w-3.5 h-3.5" />
              选号门槛与出口池
            </Button>
            <Button
              id="btnImportOpen"
              variant="primary"
              size="sm"
              onClick={() => {
                setAddTab('oauth');
                setAddModalOpen(true);
              }}
            >
              <Plus className="w-3.5 h-3.5" />
              添加账号
            </Button>
          </>
        }
      />

      {/* 顶部紧凑操作条：筛选 / 搜索 / 批量操作 / 视图切换 */}
      <Card className="p-3.5 space-y-3">
        <div className="flex flex-wrap items-center justify-between gap-2.5">
          <div className="flex items-center gap-2 flex-wrap flex-1 min-w-[220px]">
            <div className="relative flex-1 min-w-[180px] max-w-xs">
              <Search className="w-3.5 h-3.5 absolute left-2.5 top-1/2 -translate-y-1/2 text-[var(--fg-3)]" />
              <input
                value={search}
                onChange={(e) => setSearch(e.target.value)}
                placeholder="搜索账号名、用户名或出口..."
                className="ds-input w-full pl-8 pr-3 py-1.5 text-xs"
              />
            </div>

            <SegmentedControl<StatusFilter>
              ariaLabel="按状态筛选账号"
              size="xs"
              wrap
              value={statusFilter}
              onChange={setStatusFilter}
              options={[
                { value: 'all', label: '全部', badge: counts.all },
                { value: 'ok', label: '正常', badge: counts.ok },
                { value: 'issue', label: '冷却/异常', badge: counts.issue },
                { value: 'disabled', label: '已禁用', badge: counts.disabled },
              ]}
            />
          </div>

          <div className="flex items-center gap-2 flex-wrap">
            <select
              aria-label="排序方式"
              value={sortBy}
              onChange={(e) => setSortBy(e.target.value as SortKey)}
              className="ds-select px-2.5 py-1 text-xs"
            >
              <option value="default">默认排序</option>
              <option value="credits-desc">剩余积分：高 → 低</option>
              <option value="credits-asc">剩余积分：低 → 高</option>
            </select>

            <SegmentedControl<LayoutMode>
              ariaLabel="列表视图"
              size="xs"
              value={layout}
              onChange={setLayout}
              options={[
                { value: 'grid', label: '卡片' },
                { value: 'table', label: '紧凑表' },
              ]}
            />
          </div>
        </div>

        {/* 第二行：批量签到与批量探测 */}
        <div className="pt-2.5 border-t border-[var(--border)] flex flex-wrap items-center justify-between gap-2.5 text-xs">
          <div className="flex items-center gap-2 flex-wrap">
            <Button
              id="btnCheckin"
              variant="subtle"
              size="xs"
              loading={checkinBusy}
              disabled={!siteAccounts.length}
              onClick={handlePoolCheckin}
            >
              <CalendarCheck2 className="w-3.5 h-3.5" />
              {checkinBusy ? '签到中…' : '批量每日签到'}
            </Button>
            <Button
              id="btnPoolTest"
              variant="subtle"
              size="xs"
              loading={poolTestBusy}
              disabled={!siteAccounts.length}
              onClick={handlePoolTest}
            >
              <FlaskConical className="w-3.5 h-3.5" />
              {poolTestBusy ? '测试中…' : '批量 Chat 探测'}
            </Button>
            <label className="inline-flex items-center gap-1.5 text-[var(--fg-2)]">
              <span className="text-[11px] text-[var(--fg-3)]">探测模型</span>
              <select
                id="testModelSelect"
                aria-label="账号测试使用的模型"
                value={testModel}
                onChange={(e) => setTestModel(e.target.value)}
                className="ds-select px-2 py-1 text-xs max-w-[200px]"
              >
                <option value="">自动（最低倍率）</option>
                {testModelOptions.map((m) => (
                  <option key={m.id} value={m.id}>
                    {m.label}
                  </option>
                ))}
              </select>
            </label>
          </div>

          <div className="flex items-center gap-1.5 text-[11px] text-[var(--fg-3)]">
            {layout === 'grid' ? (
              <LayoutGrid className="w-3.5 h-3.5" />
            ) : (
              <List className="w-3.5 h-3.5" />
            )}
            <span>共显示 {visibleAccounts.length} 个账号</span>
          </div>
        </div>

        {checkinLog && (
          <pre
            id="checkinRaw"
            className="ds-surface-2 ds-mono text-[11px] p-3 text-[var(--fg-2)] whitespace-pre-wrap m-0 max-h-40 overflow-auto"
          >
            {checkinLog}
          </pre>
        )}
        {poolTestLog && (
          <pre
            id="poolTestRaw"
            className="ds-surface-2 ds-mono text-[11px] p-3 text-[var(--fg-2)] whitespace-pre-wrap m-0 max-h-40 overflow-auto"
          >
            {poolTestLog}
          </pre>
        )}
      </Card>

      {/* 账号列表区域 */}
      <div id="accounts">
        {!allAccounts.length ? (
          <EmptyState
            title="还没有账号"
            description="点击右上角「添加账号」，可通过浏览器 OAuth 快速授权，或直接粘贴客户端凭据 JSON 批量导入。"
            action={
              <Button
                variant="primary"
                onClick={() => {
                  setAddTab('oauth');
                  setAddModalOpen(true);
                }}
              >
                <Plus className="w-3.5 h-3.5" />
                立即添加账号
              </Button>
            }
          />
        ) : !siteAccounts.length ? (
          <EmptyState
            title={`「${siteLabel(activeSite)}站」号池暂无账号`}
            description="可在顶部切换至另一站点查看已有账号，或为当前站点授权新账号。"
          />
        ) : !visibleAccounts.length ? (
          <EmptyState title="没有匹配筛选条件的账号" description="请调整搜索关键词或状态筛选。" />
        ) : layout === 'table' ? (
          /* 紧凑表格视图 */
          <Card className="overflow-hidden">
            <div className="overflow-x-auto">
              <table className="w-full text-left border-collapse text-xs min-w-[780px]">
                <thead>
                  <tr className="border-b border-[var(--border)] bg-[var(--surface-2)] text-[11px] uppercase tracking-wider text-[var(--fg-3)]">
                    <th className="py-2.5 px-3.5 font-medium">账号</th>
                    <th className="py-2.5 px-3 font-medium">状态</th>
                    <th className="py-2.5 px-3 font-medium">Credits 余额</th>
                    <th className="py-2.5 px-3 font-medium">实时并发</th>
                    <th className="py-2.5 px-3 font-medium">签到</th>
                    <th className="py-2.5 px-3 font-medium">出口</th>
                    <th className="py-2.5 px-3.5 text-right font-medium">操作</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-[var(--border)]">
                  {visibleAccounts.map((a) => {
                    const usage = usageByAccount[a.id];
                    const states = computeAccountStates(a);
                    const cc = concurrency.byAccount[a.id];
                    const logged = a.loggedIn && a.hasCredentials;
                    const rem = usage?.credits?.unlimited
                      ? '不限量'
                      : usage?.credits?.remaining != null
                        ? String(usage.credits.remaining)
                        : a.quotaRemaining != null
                          ? String(a.quotaRemaining)
                          : '—';
                    const tot = usage?.credits?.unlimited
                      ? ''
                      : usage?.credits?.total != null
                        ? ` / ${usage.credits.total}`
                        : '';
                    return (
                      <tr key={a.id} className="hover:bg-[var(--surface-2)]/60 transition-colors">
                        <td className="py-2.5 px-3.5">
                          <div className="font-medium text-[var(--fg)]">{a.label || a.id}</div>
                          <div className="ds-mono text-[11px] text-[var(--fg-3)]">
                            {a.userNickname || a.userName || a.userId || '—'}
                          </div>
                        </td>
                        <td className="py-2.5 px-3">
                          <div className="flex flex-wrap gap-1">
                            {!a.enabled ? (
                              <StatusBadge tone="neutral">已禁用</StatusBadge>
                            ) : a.tokenExpired ? (
                              <StatusBadge tone="err">Token 过期</StatusBadge>
                            ) : !logged ? (
                              <StatusBadge tone="err">未登录</StatusBadge>
                            ) : states.length ? (
                              <StatusBadge tone={states[0].tone}>{states[0].text}</StatusBadge>
                            ) : (
                              <StatusBadge tone="ok" dot>
                                可用
                              </StatusBadge>
                            )}
                          </div>
                        </td>
                        <td className="py-2.5 px-3 ds-mono text-[var(--fg)]">
                          {rem}
                          <span className="text-[var(--fg-3)]">{tot}</span>
                        </td>
                        <td className="py-2.5 px-3 ds-mono text-[var(--fg-2)]">
                          {cc ? `${cc.inFlight} 在途 · ${cc.rpm1.toFixed(1)} RPM` : '0 在途'}
                        </td>
                        <td className="py-2.5 px-3">{renderCheckinPill(a)}</td>
                        <td className="py-2.5 px-3 ds-mono text-[var(--fg-2)]">
                          {a.proxy ? proxyHost(a.proxy) : '直连'}
                        </td>
                        <td className="py-2.5 px-3.5 text-right whitespace-nowrap">
                          <div className="inline-flex items-center gap-1">
                            <Button
                              variant="ghost"
                              size="xs"
                              loading={busyAction === `${a.id}:test`}
                              onClick={() => handleAccountAction(a, 'test')}
                            >
                              测试
                            </Button>
                            <Button
                              variant="ghost"
                              size="xs"
                              loading={busyAction === `${a.id}:usage`}
                              onClick={() => handleAccountAction(a, 'usage')}
                            >
                              余额
                            </Button>
                            <Button
                              variant="ghost"
                              size="xs"
                              onClick={() => openAccountEgressModal(a)}
                            >
                              出口
                            </Button>
                          </div>
                        </td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            </div>
          </Card>
        ) : (
          /* 精致双列卡片视图 */
          <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
            {visibleAccounts.map((a) => {
              const logged = a.loggedIn && a.hasCredentials;
              const states = computeAccountStates(a);
              const usage = usageByAccount[a.id];
              const cc = concurrency.byAccount[a.id];
              const expiry = usage?.credits ? computeExpirySummary(usage) : null;
              const creditLvl = usage ? computeCreditLevel(usage) : 'good';
              const ratio =
                usage?.credits && !usage.credits.unlimited && Number(usage.credits.total) > 0
                  ? Math.min(
                      100,
                      Math.max(
                        0,
                        (Number(usage.credits.remaining || 0) / Number(usage.credits.total)) * 100,
                      ),
                    )
                  : usage?.credits?.unlimited
                    ? 100
                    : 0;
              const isExpOpen = !!expandedExpiry[a.id];

              return (
                <Card
                  key={a.id}
                  className={`p-4 flex flex-col justify-between gap-3.5 transition-colors ${
                    !a.enabled ? 'opacity-70' : ''
                  }`}
                >
                  {/* 卡片顶行：身份 + 核心状态 + 操作 */}
                  <div className="flex items-start justify-between gap-3">
                    <div className="min-w-0">
                      <div className="flex items-center gap-1.5 flex-wrap">
                        <span className="ds-heading text-base text-[var(--fg)]">
                          {a.label || a.id}
                        </span>
                        <StatusBadge tone={logged ? 'ok' : 'err'} dot={logged}>
                          {logged ? '已登录' : '未登录'}
                        </StatusBadge>
                        {!a.enabled && <StatusBadge tone="neutral">已禁用</StatusBadge>}
                        {a.tokenExpired && <StatusBadge tone="err">Token 过期</StatusBadge>}
                        {a.proxy && (
                          <StatusBadge tone="info" title={a.proxy}>
                            <Globe className="w-3 h-3" />
                            {proxyHost(a.proxy)}
                          </StatusBadge>
                        )}
                      </div>
                      <div className="text-[11px] text-[var(--fg-3)] mt-1 ds-mono truncate">
                        {a.userNickname || a.userName || a.userId || '未命名用户'}
                        {a.authType ? ` · ${a.authType}` : ''}
                      </div>
                    </div>

                    <div className="flex items-center gap-1 shrink-0 relative">
                      <Button
                        variant="ghost"
                        size="xs"
                        loading={busyAction === `${a.id}:test`}
                        onClick={() => handleAccountAction(a, 'test')}
                      >
                        测试
                      </Button>
                      <Button
                        variant="ghost"
                        size="xs"
                        loading={busyAction === `${a.id}:usage`}
                        onClick={() => handleAccountAction(a, 'usage')}
                      >
                        {usage?.credits ? '刷新余额' : '查余额'}
                      </Button>
                      <button
                        type="button"
                        aria-label="更多操作"
                        onClick={(e) => {
                          e.stopPropagation();
                          setOpenMenuId((cur) => (cur === a.id ? null : a.id));
                        }}
                        className="p-1.5 rounded-[var(--radius-sm)] border border-[var(--border-strong)] text-[var(--fg-2)] hover:text-[var(--fg)] hover:bg-[var(--surface-hover)] cursor-pointer"
                      >
                        <MoreHorizontal className="w-3.5 h-3.5" />
                      </button>

                      {openMenuId === a.id && (
                        <div
                          onClick={(e) => e.stopPropagation()}
                          className="absolute right-0 top-full mt-1.5 z-30 w-36 py-1 rounded-[var(--radius-sm)] bg-[var(--surface)] border border-[var(--border-strong)] shadow-[var(--shadow-lg)] flex flex-col text-xs"
                        >
                          <button
                            type="button"
                            onClick={() => handleAccountAction(a, 'toggle')}
                            className="px-3 py-1.5 text-left text-[var(--fg-2)] hover:bg-[var(--surface-hover)] hover:text-[var(--fg)] cursor-pointer"
                          >
                            {a.enabled ? '禁用账号' : '启用账号'}
                          </button>
                          <button
                            type="button"
                            onClick={() => openAccountEgressModal(a)}
                            className="px-3 py-1.5 text-left text-[var(--fg-2)] hover:bg-[var(--surface-hover)] hover:text-[var(--fg)] cursor-pointer"
                          >
                            设置出口
                          </button>
                          <button
                            type="button"
                            onClick={() => handleAccountAction(a, 'refresh')}
                            className="px-3 py-1.5 text-left text-[var(--fg-2)] hover:bg-[var(--surface-hover)] hover:text-[var(--fg)] cursor-pointer"
                          >
                            刷新 Token
                          </button>
                          <div className="my-1 border-t border-[var(--border)]" />
                          <button
                            type="button"
                            onClick={() => handleAccountAction(a, 'delete')}
                            className="px-3 py-1.5 text-left text-[var(--err)] hover:bg-[var(--err-bg)] cursor-pointer"
                          >
                            删除账号
                          </button>
                        </div>
                      )}
                    </div>
                  </div>

                  {/* 额度可视化区 */}
                  <div className="ds-surface-2 p-3 space-y-2">
                    <div className="flex items-center justify-between gap-2 text-xs">
                      <span className="text-[var(--fg-2)]">Credits 余额</span>
                      {usage?.error ? (
                        <StatusBadge tone="err" title={usage.error}>
                          查询失败
                        </StatusBadge>
                      ) : usage?.credits ? (
                        <div className="flex items-center gap-2">
                          <span className="ds-mono font-medium text-[var(--fg)]">
                            {usage.credits.unlimited
                              ? '不限量'
                              : `${usage.credits.remaining ?? '-'} / ${usage.credits.total ?? '-'}`}
                          </span>
                          <a
                            href={safeHttpUrl(
                              usage.officialUsageUrl,
                              'https://www.codebuddy.cn/profile/plan',
                            )}
                            target="_blank"
                            rel="noreferrer"
                            className="text-[11px] text-[var(--fg-3)] hover:text-[var(--fg)] inline-flex items-center gap-0.5"
                          >
                            官网
                            <ExternalLink className="w-3 h-3" />
                          </a>
                        </div>
                      ) : (
                        <span className="ds-mono text-[var(--fg-3)]">
                          {a.quotaRemaining != null ? `${a.quotaRemaining}（缓存）` : '尚未查询'}
                        </span>
                      )}
                    </div>

                    {usage?.credits && (
                      <div className="h-1.5 w-full rounded-full overflow-hidden bg-[var(--border)]">
                        <div
                          className="h-full rounded-full transition-all duration-300"
                          style={{
                            width: `${ratio}%`,
                            background:
                              creditLvl === 'bad'
                                ? 'var(--err)'
                                : creditLvl === 'warn'
                                  ? 'var(--warn)'
                                  : 'var(--ok)',
                          }}
                        />
                      </div>
                    )}

                    <div className="flex items-center justify-between gap-2 flex-wrap pt-0.5">
                      {renderCheckinPill(a)}
                      {expiry && (
                        <button
                          type="button"
                          onClick={() =>
                            setExpandedExpiry((prev) => ({ ...prev, [a.id]: !prev[a.id] }))
                          }
                          className="text-[11px] text-[var(--fg-2)] hover:text-[var(--fg)] inline-flex items-center gap-1 cursor-pointer"
                        >
                          <span>
                            {expiry.expiringCount7d > 0
                              ? `7 日到期 ${expiry.expiringCount7d} 包 / ${expiry.expiringAmount7d.toFixed(0)} 分`
                              : `${expiry.totalPackages} 个套餐包`}
                          </span>
                          {isExpOpen ? (
                            <ChevronUp className="w-3 h-3" />
                          ) : (
                            <ChevronDown className="w-3 h-3" />
                          )}
                        </button>
                      )}
                    </div>

                    {/* 展开的积分到期明细表 */}
                    {expiry && isExpOpen && (
                      <div className="pt-2 mt-1 border-t border-[var(--border)] overflow-x-auto">
                        <p className="text-[11px] text-[var(--fg-3)] m-0 mb-2">
                          快照时间：{expiry.checkedAt ? formatTime(expiry.checkedAt) : '未知'} · 按本地时区显示
                        </p>
                        <table className="w-full text-left border-collapse text-[11px]">
                          <thead>
                            <tr className="border-b border-[var(--border)] text-[var(--fg-3)]">
                              <th className="py-1 pr-2 font-medium">到期时间</th>
                              <th className="py-1 pr-2 font-medium">状态</th>
                              <th className="py-1 pr-2 font-medium">套餐</th>
                              <th className="py-1 pr-2 font-medium">剩余/总</th>
                              <th className="py-1 font-medium">已用</th>
                            </tr>
                          </thead>
                          <tbody className="divide-y divide-[var(--border)]">
                            {expiry.rows.length ? (
                              expiry.rows.map((r, idx) => (
                                <tr key={idx}>
                                  <td className="py-1.5 pr-2 ds-mono text-[var(--fg-2)] whitespace-nowrap">
                                    {r.expiresAt ? formatTime(r.expiresAt) : '未知'}
                                  </td>
                                  <td className="py-1.5 pr-2">
                                    <StatusBadge tone={r.tone} className="!text-[10px] !px-1.5 !py-0">
                                      {r.statusLabel}
                                    </StatusBadge>
                                  </td>
                                  <td className="py-1.5 pr-2 text-[var(--fg)]">{r.packageName}</td>
                                  <td className="py-1.5 pr-2 ds-mono text-[var(--fg)] whitespace-nowrap">
                                    {r.remainingText} / {r.totalText}
                                  </td>
                                  <td className="py-1.5 ds-mono text-[var(--fg-2)]">{r.usedText}</td>
                                </tr>
                              ))
                            ) : (
                              <tr>
                                <td colSpan={5} className="py-2 text-center text-[var(--fg-3)]">
                                  暂无套餐明细，已过期历史包可能不在上游有效套餐接口中。
                                </td>
                              </tr>
                            )}
                          </tbody>
                        </table>
                      </div>
                    )}
                  </div>

                  {/* 调度阻塞与报错提示 */}
                  {(states.length > 0 || a.lastError) && (
                    <div className="space-y-1.5">
                      {states.length > 0 && (
                        <div className="flex flex-wrap gap-1.5">
                          {states.map((s, idx) => (
                            <StatusBadge key={idx} tone={s.tone}>
                              {s.text}
                            </StatusBadge>
                          ))}
                        </div>
                      )}
                      {a.lastError && (
                        <div className="ds-mono text-[11px] text-[var(--err)] bg-[var(--err-bg)] px-2.5 py-1.5 rounded-[var(--radius-sm)] break-all">
                          {a.lastError}
                        </div>
                      )}
                    </div>
                  )}

                  {/* 底部实时并发与请求统计 */}
                  <div className="pt-2 border-t border-[var(--border)] flex items-center justify-between gap-2 text-[11px] ds-mono text-[var(--fg-3)]">
                    <span>
                      在途 <strong className="text-[var(--fg)]">{cc?.inFlight ?? 0}</strong> · 1m RPM{' '}
                      <strong className="text-[var(--fg)]">{(cc?.rpm1 ?? 0).toFixed(1)}</strong> · 峰值{' '}
                      <strong className="text-[var(--fg)]">{cc?.peakInFlight ?? 0}</strong>
                    </span>
                    <span>
                      OK <strong className="text-[var(--fg)]">{a.successRequests || 0}</strong> / FAIL{' '}
                      <strong className={a.failedRequests ? 'text-[var(--err)]' : 'text-[var(--fg)]'}>
                        {a.failedRequests || 0}
                      </strong>
                    </span>
                  </div>
                </Card>
              );
            })}
          </div>
        )}
      </div>

      {/* ==================== 统一「添加账号」双 Tab 弹窗 ==================== */}
      <Modal
        open={addModalOpen}
        onClose={() => setAddModalOpen(false)}
        id="importModal"
        titleId="importTitle"
        title="添加账号至号池"
        subtitle="支持浏览器 OAuth 官方授权，或直接粘贴客户端凭据 / 账号池 JSON 批量导入。"
      >
        <div className="space-y-4">
          <SegmentedControl<'oauth' | 'import'>
            value={addTab}
            onChange={setAddTab}
            options={[
              { value: 'oauth', label: 'OAuth 浏览器授权' },
              { value: 'import', label: '批量 JSON 导入' },
            ]}
          />

          {addTab === 'oauth' ? (
            <div id="codebuddy" className="space-y-3.5 text-xs">
              <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
                <div>
                  <label htmlFor="site" className="block text-[11px] text-[var(--fg-3)] mb-1">
                    站点
                  </label>
                  <select
                    id="site"
                    value={oauthSite}
                    onChange={(e) => {
                      setOauthSite(normalizeSite(e.target.value));
                      setOauthSiteTouched(true);
                    }}
                    className="ds-select w-full px-2.5 py-2 text-xs"
                  >
                    <option value="domestic">国内站 · domestic</option>
                    <option value="global">国际站 · global</option>
                  </select>
                </div>
                <div>
                  <label htmlFor="label" className="block text-[11px] text-[var(--fg-3)] mb-1">
                    账号标签
                  </label>
                  <input
                    id="label"
                    value={oauthLabel}
                    onChange={(e) => setOauthLabel(e.target.value)}
                    placeholder="例如：办公主力号"
                    className="ds-input w-full px-2.5 py-2 text-xs"
                  />
                </div>
              </div>

              <div className="flex flex-wrap items-center gap-2">
                <Button
                  id="btnStart"
                  variant="primary"
                  size="sm"
                  loading={oauthBusy}
                  onClick={handleStartOAuth}
                >
                  开始认证
                </Button>
                <Button
                  id="btnPoll"
                  variant="ghost"
                  size="sm"
                  loading={oauthBusy}
                  onClick={handlePollOAuth}
                >
                  <RefreshCw className="w-3.5 h-3.5" />
                  检查登录
                </Button>
                {oauthLaunchUrl !== '#' && (
                  <a
                    id="launchLink"
                    href={safeHttpUrl(oauthLaunchUrl, '#')}
                    target="_blank"
                    rel="noreferrer"
                    className="inline-flex items-center gap-1 px-3 py-1.5 rounded-[var(--radius-sm)] border border-[var(--border-strong)] text-xs text-[var(--fg-2)] hover:text-[var(--fg)]"
                  >
                    重新打开登录页
                    <ExternalLink className="w-3 h-3" />
                  </a>
                )}
              </div>

              <div className="ds-surface-2 p-3 space-y-1">
                <div className="text-[10px] uppercase tracking-wider text-[var(--fg-3)]">
                  会话状态
                </div>
                <div id="oauthMsg" className="text-xs text-[var(--fg)] leading-relaxed">
                  {oauthMsg}
                </div>
              </div>

              <details className="ds-surface-2 p-2.5">
                <summary className="text-[11px] text-[var(--fg-3)] cursor-pointer select-none">
                  原始 OAuth 响应
                </summary>
                <pre
                  id="oauthRaw"
                  className="ds-mono text-[11px] text-[var(--fg-2)] mt-2 max-h-40 overflow-auto whitespace-pre-wrap m-0"
                >
                  {oauthRaw}
                </pre>
              </details>
            </div>
          ) : (
            <div className="space-y-3 text-xs">
              <p className="text-[var(--fg-2)] m-0 leading-relaxed">
                粘贴账号 JSON 即可（CodeBuddy 客户端凭据、账号池文件、<code className="ds-mono">account + auth</code> 包装均支持）。同账号按 userId + 站点自动更新。
              </p>
              <textarea
                id="importText"
                rows={7}
                value={importText}
                onChange={(e) => setImportText(e.target.value)}
                spellCheck={false}
                placeholder='[{"uid":"…","nickname":"…","domain":"www.codebuddy.cn","access_token":"…","refresh_token":"…","expires_at":1795951021000}]'
                className="ds-input ds-mono w-full p-3 text-[11px] leading-relaxed resize-y"
              />
              <div className="flex flex-wrap items-center justify-between gap-3">
                <label className="inline-flex items-center gap-2 text-[var(--fg-2)]">
                  <span>站点识别</span>
                  <select
                    id="importSite"
                    value={importSite}
                    onChange={(e) => setImportSite(e.target.value)}
                    className="ds-select px-2.5 py-1.5 text-xs"
                  >
                    <option value="">自动识别（按域名）</option>
                    <option value="domestic">国内站 · domestic</option>
                    <option value="global">国际站 · global</option>
                  </select>
                </label>
                <div className="flex items-center gap-2">
                  <Button id="btnImportCancel" variant="ghost" onClick={() => setAddModalOpen(false)}>
                    取消
                  </Button>
                  <Button id="btnImport" variant="primary" loading={importBusy} onClick={handleRunImport}>
                    <Upload className="w-3.5 h-3.5" />
                    {importBusy ? '导入中…' : '执行导入'}
                  </Button>
                </div>
              </div>
              {importResult && (
                <pre
                  id="importResult"
                  className="ds-surface-2 ds-mono text-[11px] p-3 text-[var(--fg-2)] whitespace-pre-wrap max-h-40 overflow-auto m-0"
                >
                  {importResult}
                </pre>
              )}
            </div>
          )}
        </div>
      </Modal>

      {/* ==================== 右侧抽屉：选号门槛 + 出口代理池 ==================== */}
      <SlideDrawer
        open={strategyDrawerOpen}
        onClose={() => setStrategyDrawerOpen(false)}
        title="选号门槛与出口代理池"
        subtitle="全局调度门槛与固定出口候选列表，低频运维配置集中收纳于此。"
        widthClass="max-w-xl"
      >
        <div className="space-y-6 text-xs">
          {/* 选号门槛 */}
          <section id="quota-floor" className="space-y-3">
            <div>
              <h4 className="ds-heading text-base text-[var(--fg)] m-0">选号门槛（积分下限）</h4>
              <p className="text-[var(--fg-3)] mt-1 m-0 leading-relaxed">
                账号积分低于此值时不再参与选号（防止把号扣穿）。每次请求后本地实时扣减生效；填 0 表示关闭。
              </p>
            </div>
            <div className="flex items-center gap-2">
              <input
                id="quotaFloor"
                type="number"
                min={0}
                step={1}
                value={quotaFloorInput}
                onChange={(e) => {
                  setQuotaFloorInput(e.target.value);
                  setQuotaFloorError('');
                }}
                disabled={quotaFloorSaving}
                className="ds-input ds-mono w-32 px-3 py-1.5 text-sm"
              />
              <Button
                id="btnQuotaFloorSave"
                variant="primary"
                size="sm"
                loading={quotaFloorSaving}
                disabled={quotaFloorSaved === null}
                onClick={handleSaveQuotaFloor}
              >
                保存门槛
              </Button>
            </div>
            <p
              id="quotaFloorState"
              role="status"
              aria-live="polite"
              data-state={
                quotaFloorError
                  ? 'error'
                  : quotaFloorDirty
                    ? 'dirty'
                    : 'saved'
              }
              className={`ds-mono text-[11px] m-0 ${
                quotaFloorError
                  ? 'text-[var(--err)]'
                  : quotaFloorDirty
                    ? 'text-[var(--warn)]'
                    : 'text-[var(--fg-2)]'
              }`}
            >
              {quotaFloorError
                ? quotaFloorError
                : quotaFloorDirty
                  ? `积分下限尚未保存；当前生效值：${quotaFloorSaved}`
                  : quotaFloorSaved && quotaFloorSaved > 0
                    ? `当前：已启用 —— 缓存余额低于 ${quotaFloorSaved} 的账号会被跳过，不参与选号`
                    : '当前：未启用 · 所有账号都参与选号'}
            </p>
          </section>

          <div className="border-t border-[var(--border)]" />

          {/* 账号出口代理池 */}
          <section id="egressPanel" className="space-y-3">
            <div className="flex items-center justify-between gap-2">
              <div>
                <h4 className="ds-heading text-base text-[var(--fg)] m-0">账号出口代理池</h4>
                <p id="egressHint" className="text-[var(--fg-3)] mt-0.5 m-0">
                  {egressState.candidates.length} 条候选出口 · {egressState.direct} 个账号本机直连
                </p>
              </div>
              <Button
                id="btnEgressBalance"
                variant="ghost"
                size="xs"
                loading={egressBusy === 'balance'}
                onClick={handleBalanceEgress}
              >
                均衡分配
              </Button>
            </div>

            <div id="egressList" className="divide-y divide-[var(--border)] ds-surface-2 px-3">
              {!egressState.candidates.length ? (
                <div className="py-4 text-center text-[var(--fg-3)]">
                  暂无候选出口，账号默认走本机直连。
                </div>
              ) : (
                <>
                  {egressState.candidates.map((c) => (
                    <div key={c.id} className="py-2.5 flex items-center justify-between gap-2">
                      <div className="min-w-0 flex-1">
                        <div className="font-medium text-[var(--fg)] truncate">
                          {c.label || proxyHost(c.url)}
                        </div>
                        <div className="ds-mono text-[11px] text-[var(--fg-3)] truncate">{c.url}</div>
                      </div>
                      <span className="ds-mono text-[11px] text-[var(--fg-2)] shrink-0">
                        {proxyCounts[c.id] || 0} 号
                      </span>
                      <div className="flex items-center gap-1 shrink-0">
                        <Button
                          variant="ghost"
                          size="xs"
                          loading={egressBusy === `check:${c.id}`}
                          onClick={() => handleCheckCandidate(c.id)}
                        >
                          检测
                        </Button>
                        <Button
                          variant="danger"
                          size="xs"
                          loading={egressBusy === `del:${c.id}`}
                          onClick={() => handleDeleteCandidate(c.id)}
                        >
                          <Trash2 className="w-3 h-3" />
                        </Button>
                      </div>
                    </div>
                  ))}
                  <div className="py-2.5 flex items-center justify-between gap-2 text-[var(--fg-2)]">
                    <div>
                      <div className="font-medium text-[var(--fg)]">直连（本机 IP）</div>
                      <div className="text-[11px] text-[var(--fg-3)]">未绑定出口的账号</div>
                    </div>
                    <span className="ds-mono text-[11px]">{egressState.direct} 号</span>
                  </div>
                </>
              )}
            </div>

            <div className="space-y-2 pt-1">
              <input
                id="egressNewUrl"
                value={egressNewUrl}
                onChange={(e) => setEgressNewUrl(e.target.value)}
                placeholder="socks5h://user:pass@host:port 或 http://host:port"
                spellCheck={false}
                className="ds-input ds-mono w-full px-3 py-1.5 text-xs"
              />
              <div className="flex items-center gap-2">
                <input
                  id="egressNewLabel"
                  value={egressNewLabel}
                  onChange={(e) => setEgressNewLabel(e.target.value)}
                  placeholder="备注（可选，如 HK-Node-01）"
                  className="ds-input flex-1 px-3 py-1.5 text-xs"
                />
                <Button
                  id="btnEgressAdd"
                  variant="primary"
                  size="sm"
                  loading={egressBusy === 'add'}
                  onClick={handleAddEgress}
                >
                  加入候选
                </Button>
              </div>
            </div>
          </section>
        </div>
      </SlideDrawer>

      {/* ==================== 单账号设置出口弹窗 ==================== */}
      <Modal
        open={!!egressModalAccount}
        onClose={() => setEgressModalAccount(null)}
        id="egressModal"
        titleId="egressTitle"
        title={`设置出口 · ${egressModalAccount?.label || egressModalAccount?.id || ''}`}
        subtitle="绑定后该账号的请求固定走这条出口（失效不回退直连）。选「直连」则使用本机 IP。"
        maxWidth="max-w-md"
      >
        <div className="space-y-3.5 text-xs">
          <div>
            <label htmlFor="egressSelect" className="block text-[11px] text-[var(--fg-3)] mb-1">
              出口选择
            </label>
            <select
              id="egressSelect"
              value={egressSelect}
              onChange={(e) => setEgressSelect(e.target.value)}
              className="ds-select w-full px-2.5 py-2 text-xs"
            >
              <option value="">直连（本机 IP）</option>
              {egressState.candidates.map((c) => (
                <option key={c.id} value={c.id}>
                  {c.label || proxyHost(c.url)} · {c.url}
                </option>
              ))}
              {egressModalAccount?.proxy &&
                !egressState.candidates.some((c) => c.url === egressModalAccount.proxy) && (
                  <option value="__current__">
                    当前：{egressModalAccount.proxy}（不在候选清单）
                  </option>
                )}
              <option value="__custom__">自定义 URL…</option>
            </select>
          </div>

          {egressSelect === '__custom__' && (
            <div id="egressCustomRow">
              <label htmlFor="egressCustom" className="block text-[11px] text-[var(--fg-3)] mb-1">
                自定义代理 URL
              </label>
              <input
                id="egressCustom"
                value={egressCustom}
                onChange={(e) => setEgressCustom(e.target.value)}
                placeholder="socks5h://user:pass@host:port"
                spellCheck={false}
                className="ds-input ds-mono w-full px-2.5 py-2 text-xs"
              />
            </div>
          )}

          {egressCheckRaw && (
            <pre
              id="egressCheckRaw"
              className="ds-surface-2 ds-mono text-[11px] p-3 text-[var(--fg-2)] whitespace-pre-wrap m-0"
            >
              {egressCheckRaw}
            </pre>
          )}

          <div className="flex items-center justify-end gap-2 pt-2">
            <Button
              id="btnEgressCheck"
              variant="ghost"
              size="sm"
              loading={egressModalBusy}
              onClick={handleCheckAccountEgress}
            >
              真实探测
            </Button>
            <Button
              id="btnEgressCancel"
              variant="ghost"
              size="sm"
              onClick={() => setEgressModalAccount(null)}
            >
              取消
            </Button>
            <Button
              id="btnEgressSave"
              variant="primary"
              size="sm"
              loading={egressModalBusy}
              onClick={handleSaveAccountEgress}
            >
              保存出口
            </Button>
          </div>
        </div>
      </Modal>
    </div>
  );
}
