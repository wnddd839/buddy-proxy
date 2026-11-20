import React, { useState } from 'react';
import {
  Activity,
  CalendarCheck2,
  Code2,
  GitFork,
  Globe,
  LayoutDashboard,
  MapPin,
  Menu,
  Moon,
  Palette,
  RefreshCw,
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
import type { Product, Site } from '@/types/api';

export type TabId =
  | 'tab-overview'
  | 'tab-pool'
  | 'tab-checkin'
  | 'tab-client'
  | 'tab-models'
  | 'tab-system-prompt'
  | 'tab-usage';

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
  inFlight: number;
  peakInFlight: number;
  attentionCount: number;
  poolCountLabel: string;
  checkinAutoEnabled: boolean;
  spMode: string;
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
  inFlight,
  peakInFlight,
  attentionCount,
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
          label: '概览大盘',
          icon: LayoutDashboard,
          badge:
            attentionCount > 0 ? (
              <StatusBadge tone="warn" className="!px-1.5 !py-0 !text-[10px]">
                {attentionCount}
              </StatusBadge>
            ) : null,
        },
        {
          id: 'tab-usage',
          label: '用量与明细',
          icon: Activity,
        },
      ],
    },
    {
      title: '账号与调度',
      items: [
        {
          id: 'tab-pool',
          label: '账号池与授权',
          icon: Users,
          badge: poolCountLabel ? (
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
          ) : null,
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
        <div className="h-14 px-4 flex items-center justify-between gap-2.5 border-b border-[var(--border)]">
          <div className="flex items-center gap-2.5 min-w-0">
            <div className="w-7 h-7 rounded-[5px] bg-[var(--surface)] border border-[var(--border-strong)] flex items-center justify-center shrink-0 shadow-[var(--shadow)]">
              <svg
                width="16"
                height="16"
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
              <div className="ds-heading text-[15px] text-[var(--fg)] truncate">
                Buddy Proxy
              </div>
              <div
                id="pillTransport"
                className="ds-mono text-[10px] tracking-wider uppercase text-[var(--fg-3)] truncate"
              >
                {transport || 'protocol_direct'}
              </div>
            </div>
          </div>
          <button
            type="button"
            aria-label="关闭导航菜单"
            onClick={() => setMobileMenuOpen(false)}
            className="lg:hidden p-1.5 rounded-[var(--radius-sm)] text-[var(--fg-3)] hover:text-[var(--fg)] cursor-pointer"
          >
            <X className="w-4 h-4" />
          </button>
        </div>

        {/* 分组导航 */}
        <nav className="tabs-nav p-3 space-y-5" role="tablist" aria-label="控制台导航">
          {navGroups.map((group) => (
            <div key={group.title}>
              <div className="px-2.5 mb-1.5 text-[10px] font-medium tracking-[0.14em] uppercase text-[var(--fg-3)]">
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
                        'tab-link w-full flex items-center justify-between gap-2 px-2.5 py-2 rounded-[var(--radius-sm)] text-[13px] transition-all duration-150 cursor-pointer text-left',
                        active
                          ? 'active bg-[var(--surface)] text-[var(--fg)] font-medium border border-[var(--border)] shadow-[var(--shadow)]'
                          : 'text-[var(--fg-2)] border border-transparent hover:bg-[var(--surface-hover)] hover:text-[var(--fg)]',
                      ].join(' ')}
                    >
                      <span className="flex items-center gap-2.5 min-w-0">
                        <Icon
                          className={`w-4 h-4 shrink-0 ${active ? 'text-[var(--fg)]' : 'opacity-70'}`}
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
      <div className="p-3 border-t border-[var(--border)] space-y-2.5">
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
    <div className="min-h-screen flex bg-[var(--canvas)] text-[var(--fg)]">
      {/* 桌面端固定侧边栏 */}
      <aside className="hidden lg:block w-[240px] shrink-0 bg-[var(--sidebar)] border-r border-[var(--border)] sticky top-0 h-screen overflow-y-auto">
        {sidebarContent}
      </aside>

      {/* 移动端滑出侧边栏 */}
      {mobileMenuOpen && (
        <div
          className="lg:hidden fixed inset-0 z-50 flex bg-black/45 backdrop-blur-[2px]"
          onMouseDown={(e) => {
            if (e.target === e.currentTarget) setMobileMenuOpen(false);
          }}
        >
          <aside className="w-[260px] max-w-[82vw] h-full bg-[var(--sidebar)] border-r border-[var(--border)] shadow-[var(--shadow-lg)] overflow-y-auto">
            {sidebarContent}
          </aside>
        </div>
      )}

      {/* 右侧主区域 */}
      <div className="flex-1 flex flex-col min-w-0">
        {/* 极简全局顶栏 */}
        <header className="sticky top-0 z-30 min-h-14 px-4 sm:px-6 py-2 bg-[var(--canvas)]/92 backdrop-blur-md border-b border-[var(--border)] flex flex-wrap items-center justify-between gap-2.5">
          <div className="flex items-center gap-2.5 min-w-0 flex-wrap">
            <button
              type="button"
              aria-label="打开导航菜单"
              onClick={() => setMobileMenuOpen(true)}
              className="lg:hidden p-1.5 rounded-[var(--radius-sm)] border border-[var(--border-strong)] text-[var(--fg-2)] hover:text-[var(--fg)] cursor-pointer"
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

            <span className="hidden sm:inline-block h-4 w-px bg-[var(--border-strong)] shrink-0" aria-hidden="true" />

            {/* 全局站点 & 产品快捷切换 */}
            <div className="flex items-center gap-2 flex-wrap">
              <SegmentedControl<Site>
                id="poolSiteSeg"
                ariaLabel="号池区域"
                size="sm"
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
                size="sm"
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

          <div className="flex items-center gap-2 shrink-0">
            <span className="pill hidden xl:inline-flex px-2.5 h-8 rounded-[var(--radius-sm)] bg-[var(--surface-2)] border border-[var(--border)] text-xs text-[var(--fg-2)]">
              <span className="w-1.5 h-1.5 rounded-full bg-[var(--info)]" />
              在途 <strong className="ds-mono text-[var(--fg)]">{inFlight}</strong>
              <span className="text-[var(--fg-3)]">·</span>
              峰值 <strong className="ds-mono text-[var(--fg)]">{peakInFlight}</strong>
            </span>

            <button
              type="button"
              aria-label={isDark ? '切换至浅色模式' : '切换至深色模式'}
              title={isDark ? '切换至浅色模式' : '切换至深色模式'}
              onClick={onToggleDark}
              className="h-8 w-8 inline-flex items-center justify-center rounded-[var(--radius-sm)] border border-[var(--border-strong)] text-[var(--fg-2)] hover:text-[var(--fg)] hover:bg-[var(--surface-hover)] transition cursor-pointer shrink-0"
            >
              {isDark ? <Sun className="w-3.5 h-3.5" /> : <Moon className="w-3.5 h-3.5" />}
            </button>

            <Button
              id="btnRefresh"
              variant="primary"
              size="sm"
              loading={refreshing}
              onClick={onRefreshStatus}
              className="h-8"
              title="重新拉取每个号的余额；有恢复证据时解除额度冷却，真实限流保持不变"
            >
              <RefreshCw className={`w-3.5 h-3.5 shrink-0 ${refreshing ? 'animate-spin' : ''}`} />
              <span>刷新状态</span>
            </Button>
          </div>
        </header>

        {/* 主内容视口 */}
        <main className="flex-1 px-4 sm:px-6 py-6">
          <div className="max-w-[1380px] mx-auto">{children}</div>
        </main>
      </div>
    </div>
  );
}
