import React, { useEffect } from 'react';
import { X } from 'lucide-react';

interface ModalProps {
  open: boolean;
  onClose: () => void;
  title: string;
  subtitle?: string;
  id?: string;
  titleId?: string;
  maxWidth?: string;
  children: React.ReactNode;
}

export function Modal({
  open,
  onClose,
  title,
  subtitle,
  id,
  titleId,
  maxWidth = 'max-w-xl',
  children,
}: ModalProps) {
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
      id={id}
      className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-black/45 backdrop-blur-[2px]"
      onMouseDown={(e) => {
        if (e.target === e.currentTarget) onClose();
      }}
    >
      <div
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
        className={`w-full ${maxWidth} max-h-[88vh] flex flex-col bg-[var(--surface)] border border-[var(--border-strong)] rounded-[var(--radius)] shadow-[var(--shadow-lg)] overflow-hidden ds-page-enter`}
      >
        <div className="px-5 py-4 border-b border-[var(--border)] flex items-start justify-between gap-4">
          <div className="min-w-0">
            <h3 id={titleId} className="ds-heading text-lg text-[var(--fg)] m-0">
              {title}
            </h3>
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
