import type React from 'react';

export interface SegmentOption<T extends string> {
  value: T;
  label: string;
  icon?: React.ReactNode;
  activeTone?: 'default' | 'ok' | 'info' | 'warn';
  badge?: string | number;
  id?: string;
  dataAttrs?: Record<string, string>;
}

interface SegmentedControlProps<T extends string> {
  value: T;
  onChange: (value: T) => void;
  options: Array<SegmentOption<T>>;
  id?: string;
  ariaLabel?: string;
  size?: 'xs' | 'sm';
  wrap?: boolean;
  disabled?: boolean;
  className?: string;
}

const ACTIVE_ICON_TONE: Record<NonNullable<SegmentOption<string>['activeTone']>, string> = {
  default: 'text-[var(--fg)]',
  ok: 'text-[var(--ok)]',
  info: 'text-[var(--info)]',
  warn: 'text-[var(--warn)]',
};

export function SegmentedControl<T extends string>({
  value,
  onChange,
  options,
  id,
  ariaLabel,
  size = 'sm',
  wrap = false,
  disabled = false,
  className = '',
}: SegmentedControlProps<T>) {
  return (
    <div
      id={id}
      role="group"
      aria-label={ariaLabel}
      className={[
        'inline-flex items-center gap-0.5 p-[3px] rounded-[calc(var(--radius-sm)+2px)] bg-[var(--seg-track)] border border-[var(--border)] max-w-full shrink-0',
        wrap ? 'flex-wrap' : 'flex-nowrap',
        disabled ? 'opacity-60 pointer-events-none' : '',
        className,
      ].join(' ')}
    >
      {options.map((opt) => {
        const active = opt.value === value;
        const iconTone = ACTIVE_ICON_TONE[opt.activeTone || 'default'];
        return (
          <button
            key={opt.value}
            id={opt.id}
            type="button"
            disabled={disabled}
            aria-pressed={active}
            onClick={() => onChange(opt.value)}
            {...(opt.dataAttrs || {})}
            className={[
              'rounded-[var(--radius-sm)] border transition-all duration-150 whitespace-nowrap shrink-0 cursor-pointer inline-flex items-center justify-center gap-1.5 leading-none',
              size === 'xs' ? 'px-2.5 py-1 text-[11px]' : 'px-2.5 py-1.5 text-xs',
              active
                ? 'active bg-[var(--seg-active)] text-[var(--fg)] font-medium border-[var(--seg-active-border)] shadow-[var(--seg-shadow)]'
                : 'border-transparent text-[var(--fg-2)] hover:text-[var(--fg)]',
            ].join(' ')}
          >
            {opt.icon && (
              <span
                className={[
                  'inline-flex items-center shrink-0 transition-colors',
                  active ? iconTone : 'text-[var(--fg-3)]',
                ].join(' ')}
              >
                {opt.icon}
              </span>
            )}
            <span>{opt.label}</span>
            {opt.badge !== undefined && (
              <span
                className={[
                  'ds-mono text-[10px]',
                  active ? 'text-[var(--fg-2)]' : 'text-[var(--fg-3)]',
                ].join(' ')}
              >
                {opt.badge}
              </span>
            )}
          </button>
        );
      })}
    </div>
  );
}
