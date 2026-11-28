import React, { useState } from 'react';
import {
  Activity,
  CalendarCheck2,
  Code2,
  Fingerprint,
  GitFork,
  Globe,
  LayoutDashboard,
  MapPin,
  Menu,
  Moon,
  Palette,
  RefreshCw,
  ShieldAlert,
  Sun,
  TerminalSquare,
  Users,
  X,
} from 'lucide-react';
import { THEME_OPTIONS, type ThemeId } from '@/hooks/useTheme';
import { Button } from '@/components/ui/Button';
import { SegmentedControl } from '@/components/ui/SegmentedControl';
import { StatusBadge } from '@/components/ui/StatusBadge';
import { UpdateWidget } from './UpdateWidget';
import type { Product, Site, SystemPromptMode } from '@/types/api';

export type TabId =
  | 'tab-overview'
  | 'tab-pool'
  | 'tab-checkin'
  | 'tab-client'
  | 'tab-models'
  | 'tab-system-prompt'
  | 'tab-usage'
  | 'tab-invariants'
  | 'tab-tok';

interface NavItem {
  id: TabId;
  label: string;
  icon: React.ComponentType<{ className?: string }>;
  badge?: React.ReactNode;
}

interface AppShellProps {
  activeTab: TabId;
  onSelectTab: (tab: TabId) => void;
  healthOk: boolean;
  healthText: string;
  healthTitle: string;
  transport: string;
  poolSite: Site;
  poolProduct: Product;
  onSwitchSite: (site: Site) => void;
  onSwitchProduct: (product: Product) => void;
  switchingSite: boolean;
  switchingProduct: boolean;
  onRefreshStatus: () => void;
  refreshing: boolean;
  attentionCount: number;
  invariantHits: number | null;
  invariantEnabled: boolean | null;
  invariantDropped: number;
  invariantWriteErrors: number;
  invariantStale: boolean;
  poolCountLabel: string;
  checkinAutoEnabled: boolean;
  spMode: SystemPromptMode | null;
  version: string;
  uiRevision: string;
  theme: ThemeId;
  isDark: boolean;
  onSetTheme: (theme: ThemeId) => void;
  onToggleDark: () => void;
  children: React.ReactNode;
}

export function AppShell({
  activeTab,
  onSelectTab,
  healthOk,
  healthText,
  healthTitle,
  transport,
  poolSite,
  poolProduct,
  onSwitchSite,
  onSwitchProduct,
  switchingSite,
  switchingProduct,
  onRefreshStatus,
  refreshing,
  attentionCount,
  invariantHits,
  invariantEnabled,
  invariantDropped,
  invariantWriteErrors,
  invariantStale,
  poolCountLabel,
  checkinAutoEnabled,
  spMode,
  version,
  uiRevision,
  theme,
  isDark,
  onSetTheme,
  onToggleDark,
  children,
}: AppShellProps) {
  const [mobileMenuOpen, setMobileMenuOpen] = useState(false);
  const [themePickerOpen, setThemePickerOpen] = useState(false);

  const navGroups: Array<{ title: string; items: NavItem[] }> = [
    {
      title: '运行监测',
      items: [
        {
          id: 'tab-overview',
          label: '统计总览',
          icon: LayoutDashboard,
        },
        {
          id: 'tab-usage',
          label: '请求明细',
          icon: Activity,
        },
        {
          id: 'tab-invariants',
          label: '静默体检',
          icon: ShieldAlert,
          badge:
            invariantEnabled === false ? (
              <span className="text-[10px] text-[var(--fg-3)]">未启用</span>
            ) : invariantDropped + invariantWriteErrors > 0 ? (
              <StatusBadge
                tone="warn"
                title="有命中 / 写盘记录被丢弃，命中数不可信（去静默体检看详情）"
                className="!px-1.5 !py-0 !text-[10px]"
              >
                !
              </StatusBadge>
            ) : invariantHits === null ? (
              <span className="ds-mono text-[11px] text-[var(--fg-3)]">—</span>
            ) : invariantHits > 0 ? (
              <StatusBadge
                tone={invariantStale ? 'warn' : 'err'}
                title={invariantStale ? '最近一次刷新失败，这个数字可能已过期' : '近 24h 命中数'}
                className="!px-1.5 !py-0 !text-[10px]"
              >
                {invariantHits}
              </StatusBadge>
            ) : null,
        },
        {
          id: 'tab-tok',
          label: '词表指纹',
          icon: Fingerprint,
        },
      ],
    },
    {
      title: '账号与调度',
      items: [
        {
          id: 'tab-pool',
          label: '账号池',
          icon: Users,
          badge:
            attentionCount > 0 ? (
              <StatusBadge tone="warn" className="!px-1.5 !py-0 !text-[10px]">
                {attentionCount}
              </StatusBadge>
            ) : poolCountLabel ? (
              <span className="ds-mono text-[11px] text-[var(--fg-3)]">{poolCountLabel}</span>
            ) : null,
        },
        {
          id: 'tab-checkin',
          label: '自动签到',
          icon: CalendarCheck2,
          badge: (
            <span
              title={checkinAutoEnabled ? '自动签到总开关已开启' : '自动签到总开关关闭'}
              className={`w-1.5 h-1.5 rounded-full ${
                checkinAutoEnabled ? 'bg-[var(--ok)]' : 'bg-[var(--border-strong)]'
              }`}
            />
          ),
        },
      ],
    },
    {
      title: '接入与策略',
      items: [
        {
          id: 'tab-client',
          label: '客户端接入',
          icon: Code2,
        },
        {
          id: 'tab-models',
          label: '模型与路由',
          icon: GitFork,
        },
        {
          id: 'tab-system-prompt',
          label: 'System Prompt',
          icon: TerminalSquare,
          badge: spMode ? (
            <span className="ds-mono text-[10px] px-1.5 py-0.5 rounded bg-[var(--surface-2)] border border-[var(--border)] text-[var(--fg-2)]">
              {spMode}
            </span>
          ) : (
            <span
              title="System Prompt 配置尚未加载或加载失败 —— 未知，不是 auto"
              className="ds-mono text-[10px] text-[var(--fg-3)]"
            >
              —
            </span>
          ),
        },
      ],
    },
  ];

  const handleNavClick = (tab: TabId) => {
    onSelectTab(tab);
    setMobileMenuOpen(false);
  };

  const sidebarContent = (
    <div className="flex flex-col justify-between h-full">
      <div>
        {/* 品牌头 */}
        <div className="h-12 px-4 flex items-center justify-between gap-2.5 border-b border-[var(--border)]">
          <div className="flex items-center gap-2.5 min-w-0">
            <div className="w-6 h-6 rounded-[5px] bg-[var(--surface)] border border-[var(--border-strong)] flex items-center justify-center shrink-0">
              <svg
                width="14"
                height="14"
                viewBox="0 0 256 256"
                fill="none"
                stroke="currentColor"
                strokeWidth="22"
                strokeLinecap="round"
                strokeLinejoin="round"
                className="text-[var(--fg)]"
                aria-hidden="true"
              >
                <path d="M50 84 122 128 50 172" />
                <path d="M122 128h84" />
              </svg>
            </div>
            <div className="leading-tight min-w-0">
              <div className="ds-heading text-[14px] text-[var(--fg)] truncate">
                Buddy Proxy
              </div>
              <div
                id="pillTransport"
                className="ds-mono text-[9px] tracking-wider uppercase text-[var(--fg-3)] truncate"
              >
                {transport || 'protocol_direct'}
              </div>
            </div>
          </div>
          <button
            type="button"
            aria-label="关闭导航菜单"
            onClick={() => setMobileMenuOpen(false)}
            className="md:hidden p-1.5 rounded-[var(--radius-sm)] text-[var(--fg-3)] hover:text-[var(--fg)] cursor-pointer"
          >
            <X className="w-4 h-4" />
          </button>
        </div>

        {/* 分组导航 */}
        <nav className="tabs-nav p-3 space-y-5" role="tablist" aria-label="控制台导航">
          {navGroups.map((group) => (
            <div key={group.title}>
              <div className="px-2.5 mb-1.5 text-[10px] font-medium tracking-[0.12em] uppercase text-[var(--fg-3)]">
                {group.title}
              </div>
              <div className="space-y-0.5">
                {group.items.map((item) => {
                  const active = activeTab === item.id;
                  const Icon = item.icon;
                  return (
                    <button
                      key={item.id}
                      type="button"
                      role="tab"
                      id={`nav-${item.id}`}
                      data-tab={item.id}
                      aria-selected={active}
                      aria-controls={item.id}
                      onClick={() => handleNavClick(item.id)}
                      className={[
                        'tab-link w-full flex items-center justify-between gap-2 px-2.5 py-1.5 rounded-[var(--radius-sm)] text-[13px] transition-colors duration-150 cursor-pointer text-left',
                        active
                          ? 'active bg-[var(--surface)] text-[var(--fg)] font-medium border border-[var(--border)] shadow-[var(--shadow)]'
                          : 'text-[var(--fg-2)] border border-transparent hover:bg-[var(--surface-hover)] hover:text-[var(--fg)]',
                      ].join(' ')}
                    >
                      <span className="flex items-center gap-2.5 min-w-0">
                        <Icon
                          className={`w-3.5 h-3.5 shrink-0 ${active ? 'text-[var(--fg)]' : 'opacity-65'}`}
                        />
                        <span className="truncate">{item.label}</span>
                      </span>
                      {item.badge}
                    </button>
                  );
                })}
              </div>
            </div>
          ))}
        </nav>
      </div>

      {/* 底部：主题快速切换 + 手动自更新 */}
      <div className="p-3 border-t border-[var(--border)] space-y-2">
        <div className="relative">
          <button
            type="button"
            onClick={() => setThemePickerOpen((o) => !o)}
            className="w-full px-2.5 py-1.5 rounded-[var(--radius-sm)] text-xs text-[var(--fg-2)] hover:bg-[var(--surface-hover)] hover:text-[var(--fg)] flex items-center justify-between transition cursor-pointer"
          >
            <span className="flex items-center gap-2">
              <Palette className="w-3.5 h-3.5 opacity-75" />
              <span>外观主题</span>
            </span>
            <span className="text-[11px] text-[var(--fg-3)]">
              {THEME_OPTIONS.find((t) => t.id === theme)?.short}
            </span>
          </button>
          {themePickerOpen && (
            <div className="absolute bottom-full left-0 right-0 mb-1.5 p-1.5 rounded-[var(--radius)] bg-[var(--surface)] border border-[var(--border-strong)] shadow-[var(--shadow-lg)] grid grid-cols-1 gap-0.5 z-40">
              {THEME_OPTIONS.map((opt) => {
                const selected = opt.id === theme;
                return (
                  <button
                    key={opt.id}
                    type="button"
                    onClick={() => {
                      onSetTheme(opt.id);
                      setThemePickerOpen(false);
                    }}
                    className={[
                      'w-full px-2.5 py-1.5 rounded-[var(--radius-sm)] text-xs flex items-center justify-between cursor-pointer transition',
                      selected
                        ? 'bg-[var(--accent-soft)] text-[var(--fg)] font-medium'
                        : 'text-[var(--fg-2)] hover:bg-[var(--surface-hover)] hover:text-[var(--fg)]',
                    ].join(' ')}
                  >
                    <span className="flex items-center gap-2">
                      <span
                        className="w-3 h-3 rounded-full border border-black/15 shrink-0"
                        style={{ background: opt.swatch }}
                      />
                      {opt.label}
                    </span>
                    {selected && <span className="text-[10px] text-[var(--fg-3)]">当前</span>}
                  </button>
                );
              })}
            </div>
          )}
        </div>
        <UpdateWidget version={version} uiRevision={uiRevision} />
      </div>
    </div>
  );

  return (
    <div className="min-h-screen md:h-screen md:overflow-hidden flex bg-[var(--canvas)] text-[var(--fg)]">
      {/* 常驻固定左侧边栏（md >= 768px 始终常驻） */}
      <aside
        id="appSidebar"
        className="hidden md:flex md:flex-col w-[220px] shrink-0 bg-[var(--sidebar)] border-r border-[var(--border)] h-screen overflow-y-auto"
      >
        {sidebarContent}
      </aside>

      {/* 小屏移动端滑出侧边栏（< 768px） */}
      {mobileMenuOpen && (
        <div
          className="md:hidden fixed inset-0 z-50 flex bg-black/45 backdrop-blur-[2px]"
          onMouseDown={(e) => {
            if (e.target === e.currentTarget) setMobileMenuOpen(false);
          }}
        >
          <aside className="w-[248px] max-w-[82vw] h-full bg-[var(--sidebar)] border-r border-[var(--border)] shadow-[var(--shadow-lg)] overflow-y-auto">
            {sidebarContent}
          </aside>
        </div>
      )}

      {/* 右侧独立滚动主区域 */}
      <div className="flex-1 flex flex-col min-w-0 md:h-screen md:overflow-hidden">
        {/* 极简轻量化全局顶栏（48px） */}
        <header className="sticky top-0 md:static z-30 min-h-12 px-4 sm:px-7 py-1.5 bg-[var(--canvas)]/92 backdrop-blur-md border-b border-[var(--border)] flex flex-wrap items-center justify-between gap-2 shrink-0">
          <div className="flex items-center gap-2.5 min-w-0 flex-wrap">
            <button
              type="button"
              aria-label="打开导航菜单"
              onClick={() => setMobileMenuOpen(true)}
              className="md:hidden p-1.5 rounded-[var(--radius-sm)] border border-[var(--border-strong)] text-[var(--fg-2)] hover:text-[var(--fg)] cursor-pointer"
            >
              <Menu className="w-4 h-4" />
            </button>

            <span className="pill text-xs shrink-0" title={healthTitle}>
              <span
                id="healthDot"
                className={`w-2 h-2 rounded-full shrink-0 ${
                  healthOk ? 'bg-[var(--ok)] ds-pulse-ok' : 'bg-[var(--err)]'
                }`}
              />
              <span id="healthText" className="font-medium text-[var(--fg)] whitespace-nowrap">
                {healthText}
              </span>
            </span>

            <span
              className="hidden sm:inline-block h-3.5 w-px bg-[var(--border-strong)] shrink-0"
              aria-hidden="true"
            />

            {/* 全局站点 & 产品快捷切换 */}
            <div className="flex items-center gap-1.5 flex-wrap">
              <SegmentedControl<Site>
                id="poolSiteSeg"
                ariaLabel="号池区域"
                size="xs"
                disabled={switchingSite}
                value={poolSite}
                onChange={(s) => {
                  if (!switchingSite) onSwitchSite(s);
                }}
                options={[
                  {
                    value: 'domestic',
                    label: '国内站',
                    icon: <MapPin className="w-3 h-3" />,
                    activeTone: 'ok',
                    id: 'btnPoolDomestic',
                    dataAttrs: { 'data-site': 'domestic' },
                  },
                  {
                    value: 'global',
                    label: '国际站',
                    icon: <Globe className="w-3 h-3" />,
                    activeTone: 'info',
                    id: 'btnPoolGlobal',
                    dataAttrs: { 'data-site': 'global' },
                  },
                ]}
              />
              <SegmentedControl<Product>
                id="poolProductSeg"
                ariaLabel="上游产品"
                size="xs"
                disabled={switchingProduct}
                value={poolProduct}
                onChange={(p) => {
                  if (!switchingProduct) onSwitchProduct(p);
                }}
                options={[
                  {
                    value: 'codebuddy',
                    label: 'CodeBuddy',
                    id: 'btnProductCodeBuddy',
                    dataAttrs: { 'data-product': 'codebuddy' },
                  },
                  {
                    value: 'workbuddy',
                    label: 'WorkBuddy',
                    id: 'btnProductWorkBuddy',
                    dataAttrs: { 'data-product': 'workbuddy' },
                  },
                ]}
              />
            </div>
          </div>

          <div className="flex items-center gap-1.5 shrink-0">
            <button
              type="button"
              aria-label={isDark ? '切换至浅色模式' : '切换至深色模式'}
              title={isDark ? '切换至浅色模式' : '切换至深色模式'}
              onClick={onToggleDark}
              className="h-7 w-7 inline-flex items-center justify-center rounded-[var(--radius-sm)] border border-[var(--border-strong)] text-[var(--fg-2)] hover:text-[var(--fg)] hover:bg-[var(--surface-hover)] transition cursor-pointer shrink-0"
            >
              {isDark ? <Sun className="w-3.5 h-3.5" /> : <Moon className="w-3.5 h-3.5" />}
            </button>

            <Button
              id="btnRefresh"
              variant="ghost"
              size="xs"
              loading={refreshing}
              onClick={onRefreshStatus}
              className="h-7"
              title="重新拉取每个号的余额；有恢复证据时解除额度冷却，真实限流保持不变"
            >
              <RefreshCw className={`w-3 h-3 shrink-0 ${refreshing ? 'animate-spin' : ''}`} />
              <span>刷新</span>
            </Button>
          </div>
        </header>

        {/* 主内容视口（独立滚动） */}
        <main className="flex-1 md:overflow-y-auto px-5 sm:px-8 py-6 sm:py-7">
          <div className="max-w-[1200px] mx-auto">{children}</div>
        </main>
      </div>
    </div>
  );
}
