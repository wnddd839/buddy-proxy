import { useMemo, useState } from 'react';
import { Check, Copy, KeyRound, Plus, RefreshCw, Trash2 } from 'lucide-react';
import type { AccountSummary, ClientConfigPayload, Site } from '@/types/api';
import { bareModelId, normalizeSite, siteLabel } from '@/lib/format';
import { apiRequest, copyToClipboard } from '@/lib/api';
import {
  buildClientSnippet,
  CLIENT_SNIPPET_OPTIONS,
  type ClientSnippetId,
} from '@/lib/snippets';
import { useToast } from '@/hooks/useToast';
import { useConfirm } from '@/hooks/useConfirm';
import { Button } from '@/components/ui/Button';
import { Card, EmptyState, PageHeader } from '@/components/ui/Card';
import { ScopeNote } from '@/components/ui/ScopeNote';
import { SegmentedControl } from '@/components/ui/SegmentedControl';
import { StatusBadge } from '@/components/ui/StatusBadge';

interface ClientPageProps {
  config: ClientConfigPayload | null;
  accounts: AccountSummary[];
  onRefresh: () => Promise<ClientConfigPayload>;
  onConfigUpdated: (next: ClientConfigPayload) => void;
}

export function ClientPage({
  config,
  accounts,
  onRefresh,
  onConfigUpdated,
}: ClientPageProps) {
  const { showToast } = useToast();
  const { confirm } = useConfirm();

  const [refreshing, setRefreshing] = useState(false);
  const [generatingKey, setGeneratingKey] = useState(false);
  const [creatingBound, setCreatingBound] = useState(false);
  const [deletingKey, setDeletingKey] = useState('');
  const [copiedId, setCopiedId] = useState('');

  const [boundSite, setBoundSite] = useState<Site>('domestic');
  const [boundAccountId, setBoundAccountId] = useState('');
  const [snippetTab, setSnippetTab] = useState<ClientSnippetId>('codex');

  const baseUrl = config?.baseUrl || config?.apiBase || '';
  const chatUrl =
    config?.chatCompletionsUrl ||
    (baseUrl ? `${baseUrl.replace(/\/$/, '')}/chat/completions` : '');
  const responsesUrl =
    config?.responsesUrl || (baseUrl ? `${baseUrl.replace(/\/$/, '')}/responses` : '');
  const recommendedModel = bareModelId(config?.recommendedModel || 'auto');
  const apiKeyConfigured = !!(config?.apiKeyConfigured || config?.apiKey);
  const boundKeys = config?.apiKeys || [];

  const eligibleAccounts = useMemo(
    () => accounts.filter((a) => a.enabled && a.hasCredentials),
    [accounts],
  );

  const flashCopy = async (id: string, value: string, label: string) => {
    try {
      await copyToClipboard(value);
      setCopiedId(id);
      showToast(`${label}已复制`, 'success');
      window.setTimeout(() => setCopiedId((cur) => (cur === id ? '' : cur)), 1300);
    } catch (e: unknown) {
      showToast(`复制失败：${e instanceof Error ? e.message : String(e)}`, 'error');
    }
  };

  const handleCopyMainKey = async () => {
    try {
      const fresh = await onRefresh();
      if (!fresh.apiKey) {
        showToast('API Key 未配置', 'error');
        return;
      }
      await flashCopy('mainKey', fresh.apiKey, 'API Key');
    } catch (e: unknown) {
      showToast(e instanceof Error ? e.message : String(e), 'error');
    }
  };

  const handleRefresh = async () => {
    setRefreshing(true);
    try {
      await onRefresh();
      showToast('接入信息已刷新');
    } catch (e: unknown) {
      showToast(e instanceof Error ? e.message : String(e), 'error');
    } finally {
      setRefreshing(false);
    }
  };

  const handleGenerateMainKey = async () => {
    const ok = await confirm({
      title: '生成新的网关主 API Key？',
      description:
        '旧主 Key 会立即失效，新 Key 会写入 .env 并在重启后继续生效。\n请同步更新 ZCode / Codex / OpenCode 等客户端里的 Key（区域绑定 Key 不受影响）。',
      confirmLabel: '立即生成并覆盖旧 Key',
      danger: true,
    });
    if (!ok) return;
    setGeneratingKey(true);
    try {
      const data = await apiRequest<ClientConfigPayload>(
        '/direct-admin/api/client-config/generate-key',
        { method: 'POST', body: '{}' },
      );
      onConfigUpdated(data);
      if (data.apiKey) {
        await copyToClipboard(data.apiKey).catch(() => {});
      }
      showToast(data.note || '新 API Key 已生成并复制', 'success');
    } catch (e: unknown) {
      showToast(e instanceof Error ? e.message : String(e), 'error');
    } finally {
      setGeneratingKey(false);
    }
  };

  const handleCreateBoundKey = async () => {
    setCreatingBound(true);
    try {
      const binding = boundAccountId ? { accountId: boundAccountId } : { site: boundSite };
      const data = await apiRequest<ClientConfigPayload>(
        '/direct-admin/api/client-config/bound-keys',
        { method: 'POST', body: JSON.stringify(binding) },
      );
      onConfigUpdated(data);
      const created = (data.apiKeys || [])
        .filter(
          (item) =>
            item.key &&
            (boundAccountId
              ? item.accountId === boundAccountId
              : !item.accountId && normalizeSite(item.site) === boundSite),
        )
        .pop();
      if (created?.key) {
        await copyToClipboard(created.key).catch(() => {});
      }
      showToast(data.note || '绑定 Key 已生成并复制', 'success');
    } catch (e: unknown) {
      showToast(e instanceof Error ? e.message : String(e), 'error');
    } finally {
      setCreatingBound(false);
    }
  };

  const handleDeleteBoundKey = async (key: string) => {
    const ok = await confirm({
      title: '删除这把绑定 Key？',
      description: '删除后使用该 Key 的客户端将无法访问，主 API Key 不受影响。',
      confirmLabel: '确认删除',
      danger: true,
    });
    if (!ok) return;
    setDeletingKey(key);
    try {
      const data = await apiRequest<ClientConfigPayload>(
        '/direct-admin/api/client-config/bound-keys',
        { method: 'DELETE', body: JSON.stringify({ key }) },
      );
      onConfigUpdated(data);
      showToast('绑定 Key 已删除');
    } catch (e: unknown) {
      showToast(e instanceof Error ? e.message : String(e), 'error');
    } finally {
      setDeletingKey('');
    }
  };

  const snippetText = useMemo(
    () =>
      buildClientSnippet(
        snippetTab,
        baseUrl,
        recommendedModel,
        config?.apiKeyPreview || '你的网关APIKey',
      ),
    [snippetTab, baseUrl, recommendedModel, config?.apiKeyPreview],
  );
  const currentSnippetMeta = CLIENT_SNIPPET_OPTIONS.find((s) => s.id === snippetTab);

  const accountNameMap = useMemo(() => {
    const m: Record<string, string> = {};
    for (const a of accounts) m[a.id] = a.label || a.id;
    return m;
  }, [accounts]);

  return (
    <div id="tab-client" className="ds-page-enter space-y-5">
      <PageHeader
        title="客户端接入与 API Key"
        subtitle="标准 OpenAI /v1 协议入口；右侧自动带入当前监听地址生成各主流客户端配置，绝不暴露上游 OAuth Token。"
        actions={
          <>
            <Button
              id="btnRefreshClient"
              variant="ghost"
              size="sm"
              loading={refreshing}
              onClick={handleRefresh}
            >
              <RefreshCw className={`w-3.5 h-3.5 ${refreshing ? 'animate-spin' : ''}`} />
              刷新接入信息
            </Button>
            <Button
              id="btnGenerateKey"
              variant="primary"
              size="sm"
              loading={generatingKey}
              onClick={handleGenerateMainKey}
            >
              <KeyRound className="w-3.5 h-3.5" />
              轮换主 API Key
            </Button>
          </>
        }
      />

      <ScopeNote>
        口径：Base / Chat / Responses 是并列的三个端点，不是「Base 加路径」的包含关系，
        配置客户端时要按协议类型选对应的那一个。
            </ScopeNote>

      <div id="client-config" className="grid grid-cols-1 lg:grid-cols-12 gap-4">
        {/* 左 5 栏：端点与绑定 Key 管理 */}
        <div className="lg:col-span-5 space-y-4">
          <Card className="p-4 space-y-3">
            <div className="text-sm font-medium text-[var(--fg)]">核心端点与主密钥</div>
            <div className="space-y-3 text-xs">
              <div>
                <label htmlFor="openAiBaseUrl" className="block text-[11px] text-[var(--fg-3)] mb-1">
                  Base URL
                </label>
                <div className="flex gap-1.5">
                  <input
                    id="openAiBaseUrl"
                    readOnly
                    value={baseUrl}
                    placeholder="加载中…"
                    className="ds-input ds-mono flex-1 px-2.5 py-1.5 text-xs"
                  />
                  <Button
                    id="copyBaseUrl"
                    variant="ghost"
                    size="sm"
                    onClick={() => flashCopy('base', baseUrl, 'Base URL')}
                  >
                    {copiedId === 'base' ? <Check className="w-3.5 h-3.5" /> : <Copy className="w-3.5 h-3.5" />}
                    {copiedId === 'base' ? '已复制' : '复制'}
                  </Button>
                </div>
              </div>

              <div>
                <label htmlFor="openAiChatUrl" className="block text-[11px] text-[var(--fg-3)] mb-1">
                  Chat Completions（ZCode / OpenCode / SDK）
                </label>
                <div className="flex gap-1.5">
                  <input
                    id="openAiChatUrl"
                    readOnly
                    value={chatUrl}
                    placeholder="加载中…"
                    className="ds-input ds-mono flex-1 px-2.5 py-1.5 text-xs"
                  />
                  <Button
                    id="copyChatUrl"
                    variant="ghost"
                    size="sm"
                    onClick={() => flashCopy('chat', chatUrl, 'Chat Completions')}
                  >
                    {copiedId === 'chat' ? '已复制' : '复制'}
                  </Button>
                </div>
              </div>

              <div>
                <label htmlFor="openAiResponsesUrl" className="block text-[11px] text-[var(--fg-3)] mb-1">
                  Responses API（Codex CLI）
                </label>
                <div className="flex gap-1.5">
                  <input
                    id="openAiResponsesUrl"
                    readOnly
                    value={responsesUrl}
                    placeholder="加载中…"
                    className="ds-input ds-mono flex-1 px-2.5 py-1.5 text-xs"
                  />
                  <Button
                    id="copyResponsesUrl"
                    variant="ghost"
                    size="sm"
                    onClick={() => flashCopy('resp', responsesUrl, 'Responses')}
                  >
                    {copiedId === 'resp' ? '已复制' : '复制'}
                  </Button>
                </div>
              </div>

              <div className="grid grid-cols-1 sm:grid-cols-2 gap-2.5">
                <div>
                  <label htmlFor="openAiApiKey" className="block text-[11px] text-[var(--fg-3)] mb-1">
                    网关主 API Key
                  </label>
                  <div className="flex gap-1.5">
                    <input
                      id="openAiApiKey"
                      type="password"
                      readOnly
                      value={
                        apiKeyConfigured ? config?.apiKeyPreview || '已配置 · 点击复制' : ''
                      }
                      placeholder={apiKeyConfigured ? '' : '未配置'}
                      className="ds-input ds-mono flex-1 min-w-0 px-2.5 py-1.5 text-xs"
                    />
                    <Button
                      id="copyApiKey"
                      variant="ghost"
                      size="sm"
                      disabled={!apiKeyConfigured}
                      onClick={handleCopyMainKey}
                    >
                      {copiedId === 'mainKey' ? '已复制' : '复制'}
                    </Button>
                  </div>
                </div>

                <div>
                  <label htmlFor="openAiModel" className="block text-[11px] text-[var(--fg-3)] mb-1">
                    推荐模型
                  </label>
                  <div className="flex gap-1.5">
                    <input
                      id="openAiModel"
                      readOnly
                      value={recommendedModel}
                      className="ds-input ds-mono flex-1 min-w-0 px-2.5 py-1.5 text-xs"
                    />
                    <Button
                      id="copyModel"
                      variant="ghost"
                      size="sm"
                      onClick={() => flashCopy('model', recommendedModel, '模型')}
                    >
                      {copiedId === 'model' ? '已复制' : '复制'}
                    </Button>
                  </div>
                </div>
              </div>
            </div>

            <p id="clientConfigHint" className="text-[11px] text-[var(--fg-3)] leading-relaxed m-0 pt-1">
              {config?.note ||
                '点击复制时才读取明文 Key。主 Key 默认落盘于 ~/.codebuddy/proxy.env，换目录启动同样沿用。'}
            </p>
          </Card>

          {/* 区域 / 固定账号绑定 Key */}
          <Card className="p-4 space-y-3.5">
            <div>
              <div className="text-sm font-medium text-[var(--fg)]">区域与固定账号绑定 Key</div>
              <p className="text-[11px] text-[var(--fg-3)] mt-0.5 m-0">
                为不同客户端分发专属 Key，换 Key 等于换区或钉死单号，不改变主 Key。
              </p>
            </div>

            <div id="boundApiKeys">
              {!boundKeys.length ? (
                <EmptyState
                  title="暂无绑定 Key"
                  description="主 Key 仍可正常使用；若需分区分号隔离，请在下方新建一把。"
                  className="py-5"
                />
              ) : (
                <div className="divide-y divide-[var(--border)] ds-surface-2 px-3">
                  {boundKeys.map((item, idx) => {
                    const keyVal = item.key || '';
                    return (
                      <div key={keyVal || idx} className="py-2.5 flex items-center justify-between gap-2">
                        <div className="min-w-0">
                          <div className="ds-mono text-xs text-[var(--fg)] truncate">
                            {item.preview || '已配置'}
                          </div>
                          <div className="mt-1 flex items-center gap-1.5 flex-wrap">
                            {item.accountId ? (
                              <StatusBadge tone="info">
                                固定账号 · {accountNameMap[item.accountId] || item.accountId}
                              </StatusBadge>
                            ) : (
                              <StatusBadge tone="neutral">
                                {siteLabel(item.site)}站自动选号
                              </StatusBadge>
                            )}
                          </div>
                        </div>
                        <div className="flex items-center gap-1 shrink-0">
                          <Button
                            variant="ghost"
                            size="xs"
                            onClick={() => flashCopy(`b:${idx}`, keyVal, '绑定 Key')}
                          >
                            {copiedId === `b:${idx}` ? '已复制' : '复制'}
                          </Button>
                          <Button
                            variant="danger"
                            size="xs"
                            loading={deletingKey === keyVal}
                            onClick={() => handleDeleteBoundKey(keyVal)}
                          >
                            <Trash2 className="w-3 h-3" />
                          </Button>
                        </div>
                      </div>
                    );
                  })}
                </div>
              )}
            </div>

            {/* 严格保持 boundKeySite -> boundKeyAccount -> btnNewBoundKey 顺序 */}
            <div className="pt-2 border-t border-[var(--border)] space-y-2">
              <div className="text-[11px] text-[var(--fg-3)]">新建绑定 Key</div>
              <div className="grid grid-cols-1 sm:grid-cols-12 gap-2">
                <select
                  id="boundKeySite"
                  aria-label="绑定区域"
                  value={boundSite}
                  onChange={(e) => setBoundSite(normalizeSite(e.target.value))}
                  className="ds-select sm:col-span-4 px-2.5 py-1.5 text-xs"
                >
                  <option value="domestic">国内 domestic</option>
                  <option value="global">国际 global</option>
                </select>
                <select
                  id="boundKeyAccount"
                  aria-label="绑定账号"
                  value={boundAccountId}
                  onChange={(e) => setBoundAccountId(e.target.value)}
                  className="ds-select sm:col-span-5 px-2.5 py-1.5 text-xs"
                >
                  <option value="">区域内自动选号</option>
                  {eligibleAccounts.map((a) => (
                    <option key={a.id} value={a.id}>
                      {(a.label || a.id) + ' · ' + siteLabel(a.site)}
                    </option>
                  ))}
                </select>
                <Button
                  id="btnNewBoundKey"
                  variant="primary"
                  size="sm"
                  loading={creatingBound}
                  onClick={handleCreateBoundKey}
                  className="sm:col-span-3"
                >
                  <Plus className="w-3.5 h-3.5" />
                  新建
                </Button>
              </div>
            </div>
          </Card>
        </div>

        {/* 右 7 栏：动态客户端配置代码生成器 */}
        <Card className="lg:col-span-7 flex flex-col overflow-hidden">
          <div className="px-4 py-3 border-b border-[var(--border)] flex flex-wrap items-center justify-between gap-2.5 bg-[var(--surface-2)]/50">
            <SegmentedControl<ClientSnippetId>
              ariaLabel="选择客户端模板"
              size="xs"
              wrap
              value={snippetTab}
              onChange={setSnippetTab}
              options={CLIENT_SNIPPET_OPTIONS.map((o) => ({ value: o.id, label: o.label }))}
            />
            <Button
              variant="primary"
              size="xs"
              onClick={() => flashCopy('snippet', snippetText, `${currentSnippetMeta?.label} 配置`)}
            >
              {copiedId === 'snippet' ? <Check className="w-3 h-3" /> : <Copy className="w-3 h-3" />}
              {copiedId === 'snippet' ? '已复制配置' : '复制代码片段'}
            </Button>
          </div>

          <div className="px-4 py-2 border-b border-[var(--border)] text-[11px] text-[var(--fg-3)]">
            {currentSnippetMeta?.hint}
          </div>

          <pre className="p-4 ds-mono text-xs leading-relaxed text-[var(--fg)] overflow-x-auto flex-1 m-0 bg-[var(--surface)]">
            {snippetText}
          </pre>

          <div className="px-4 py-2.5 border-t border-[var(--border)] bg-[var(--surface-2)]/60 text-[11px] text-[var(--fg-3)] flex flex-wrap items-center justify-between gap-2">
            <span>自动注入当前网关地址：{baseUrl || 'http://127.0.0.1:32126/v1'}</span>
            <span>模型名无需加 codebuddy/ 前缀，传 auto 亦可</span>
          </div>
        </Card>
      </div>
    </div>
  );
}
