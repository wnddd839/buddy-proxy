import React from 'react';

export type ButtonVariant = 'primary' | 'ghost' | 'subtle' | 'danger' | 'dangerSolid';
export type ButtonSize = 'xs' | 'sm' | 'md';

interface ButtonProps extends React.ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: ButtonVariant;
  size?: ButtonSize;
  loading?: boolean;
}

const SIZE_CLASS: Record<ButtonSize, string> = {
  xs: 'px-2 py-1 text-[11px] gap-1',
  sm: 'px-3 py-1.5 text-xs gap-1.5',
  md: 'px-4 py-2 text-[13px] gap-2',
};

const VARIANT_STYLE: Record<ButtonVariant, string> = {
  primary:
    'bg-[var(--accent)] text-[var(--accent-fg)] border border-[var(--accent)] font-medium hover:opacity-90',
  ghost:
    'bg-transparent text-[var(--fg-2)] border border-[var(--border-strong)] hover:bg-[var(--surface-hover)] hover:text-[var(--fg)]',
  subtle:
    'bg-[var(--surface-2)] text-[var(--fg-2)] border border-[var(--border)] hover:bg-[var(--surface-hover)] hover:text-[var(--fg)]',
  danger:
    'bg-transparent text-[var(--err)] border border-[var(--border-strong)] hover:bg-[var(--err-bg)] hover:border-[var(--err)]',
  dangerSolid:
    'bg-[var(--err)] text-white border border-[var(--err)] font-medium hover:opacity-90',
};

export function Button({
  variant = 'ghost',
  size = 'sm',
  loading = false,
  disabled,
  className = '',
  type = 'button',
  children,
  ...rest
}: ButtonProps) {
  return (
    <button
      type={type}
      disabled={disabled || loading}
      className={[
        'inline-flex items-center justify-center rounded-[var(--radius-sm)] transition-all duration-150 select-none cursor-pointer whitespace-nowrap shrink-0',
        'disabled:opacity-45 disabled:cursor-not-allowed active:scale-[0.99]',
        'focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-[var(--accent)]',
        SIZE_CLASS[size],
        VARIANT_STYLE[variant],
        className,
      ].join(' ')}
      {...rest}
    >
      {children}
    </button>
  );
}
