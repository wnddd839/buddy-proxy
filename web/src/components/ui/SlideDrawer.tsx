import React, { useEffect } from 'react';
import { X } from 'lucide-react';

interface SlideDrawerProps {
  open: boolean;
  onClose: () => void;
  title: string;
  subtitle?: string;
  widthClass?: string;
  children: React.ReactNode;
}

export function SlideDrawer({
  open,
  onClose,
  title,
  subtitle,
  widthClass = 'max-w-lg',
  children,
}: SlideDrawerProps) {
  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose();
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [open, onClose]);

  if (!open) return null;

  return (
    <div
      className="fixed inset-0 z-50 flex justify-end bg-black/40 backdrop-blur-[2px]"
      onMouseDown={(e) => {
        if (e.target === e.currentTarget) onClose();
      }}
    >
      <div
        role="dialog"
        aria-modal="true"
        className={`w-full ${widthClass} h-full bg-[var(--surface)] border-l border-[var(--border)] shadow-[var(--shadow-lg)] flex flex-col`}
      >
        <div className="px-5 py-4 border-b border-[var(--border)] flex items-start justify-between gap-4 shrink-0">
          <div className="min-w-0">
            <h3 className="ds-heading text-lg text-[var(--fg)] m-0">{title}</h3>
            {subtitle && (
              <p className="text-xs text-[var(--fg-3)] mt-1 leading-relaxed">{subtitle}</p>
            )}
          </div>
          <button
            type="button"
            aria-label="关闭"
            onClick={onClose}
            className="p-1.5 rounded-[var(--radius-sm)] text-[var(--fg-3)] hover:text-[var(--fg)] hover:bg-[var(--surface-hover)] transition cursor-pointer shrink-0"
          >
            <X className="w-4 h-4" />
          </button>
        </div>
        <div className="p-5 overflow-y-auto flex-1">{children}</div>
      </div>
    </div>
  );
}
