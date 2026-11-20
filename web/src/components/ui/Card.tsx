import React from 'react';

interface CardProps {
  children: React.ReactNode;
  className?: string;
  id?: string;
}

export function Card({ children, className = '', id }: CardProps) {
  return (
    <div id={id} className={`ds-surface ${className}`}>
      {children}
    </div>
  );
}

interface StatCardProps {
  label: string;
  labelId?: string;
  value: React.ReactNode;
  valueId?: string;
  sub?: React.ReactNode;
  badge?: React.ReactNode;
  footer?: React.ReactNode;
  title?: string;
  valueClassName?: string;
  className?: string;
}

export function StatCard({
  label,
  labelId,
  value,
  valueId,
  sub,
  badge,
  footer,
  title,
  valueClassName = '',
  className = '',
}: StatCardProps) {
  return (
    <div title={title} className={`ds-surface p-4 flex flex-col justify-between ${className}`}>
      <div className="flex items-center justify-between gap-2 text-xs text-[var(--fg-2)]">
        <span id={labelId} className="truncate">
          {label}
        </span>
        {badge}
      </div>
      <div className="mt-2 flex items-baseline gap-2 min-w-0">
        <span
          id={valueId}
          className={`ds-mono text-2xl font-medium text-[var(--fg)] leading-tight truncate ${valueClassName}`}
        >
          {value}
        </span>
        {sub && <span className="ds-mono text-xs text-[var(--fg-3)] truncate">{sub}</span>}
      </div>
      {footer && <div className="mt-3">{footer}</div>}
    </div>
  );
}

interface PageHeaderProps {
  title: string;
  subtitle?: React.ReactNode;
  actions?: React.ReactNode;
}

export function PageHeader({ title, subtitle, actions }: PageHeaderProps) {
  return (
    <div className="flex flex-wrap items-end justify-between gap-4">
      <div className="min-w-0">
        <h1 className="ds-heading text-[24px] leading-tight text-[var(--fg)] m-0">{title}</h1>
        {subtitle && (
          <div className="text-xs text-[var(--fg-2)] mt-1.5 leading-relaxed max-w-2xl">
            {subtitle}
          </div>
        )}
      </div>
      {actions && <div className="flex items-center gap-2 flex-wrap">{actions}</div>}
    </div>
  );
}

interface EmptyStateProps {
  title: string;
  description?: string;
  action?: React.ReactNode;
  className?: string;
}

export function EmptyState({ title, description, action, className = '' }: EmptyStateProps) {
  return (
    <div
      className={[
        'rounded-[var(--radius)] border border-dashed border-[var(--border-strong)] py-10 px-6 text-center',
        className,
      ].join(' ')}
    >
      <div className="text-sm font-medium text-[var(--fg)]">{title}</div>
      {description && (
        <p className="text-xs text-[var(--fg-3)] mt-1.5 max-w-md mx-auto leading-relaxed">
          {description}
        </p>
      )}
      {action && <div className="mt-4 flex justify-center">{action}</div>}
    </div>
  );
}
