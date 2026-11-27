import React from 'react';
import type { Tone } from '@/lib/domain';

interface StatusBadgeProps {
  id?: string;
  tone?: Tone;
  dot?: boolean;
  pulse?: boolean;
  title?: string;
  className?: string;
  children: React.ReactNode;
}

const TONE_CLASS: Record<Tone, string> = {
  ok: 'bg-[var(--ok-bg)] text-[var(--ok)]',
  warn: 'bg-[var(--warn-bg)] text-[var(--warn)]',
  err: 'bg-[var(--err-bg)] text-[var(--err)]',
  info: 'bg-[var(--info-bg)] text-[var(--info)]',
  neutral: 'bg-[var(--surface-2)] text-[var(--fg-2)] border border-[var(--border)]',
};

const DOT_COLOR: Record<Tone, string> = {
  ok: 'bg-[var(--ok)]',
  warn: 'bg-[var(--warn)]',
  err: 'bg-[var(--err)]',
  info: 'bg-[var(--info)]',
  neutral: 'bg-[var(--fg-3)]',
};

export function StatusBadge({
  id,
  tone = 'neutral',
  dot = false,
  pulse = false,
  title,
  className = '',
  children,
}: StatusBadgeProps) {
  return (
    <span
      id={id}
      title={title}
      className={[
        'inline-flex items-center gap-1.5 px-2 py-0.5 rounded-full text-[11px] font-medium leading-[1.45] whitespace-nowrap shrink-0',
        TONE_CLASS[tone],
        className,
      ].join(' ')}
    >
      {dot && (
        <span
          className={[
            'w-1.5 h-1.5 rounded-full shrink-0',
            DOT_COLOR[tone],
            pulse && tone === 'ok' ? 'ds-pulse-ok' : '',
          ].join(' ')}
        />
      )}
      {children}
    </span>
  );
}
