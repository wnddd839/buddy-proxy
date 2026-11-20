import { useToast } from '@/hooks/useToast';
import { X } from 'lucide-react';

export function ToastViewport() {
  const { toasts, dismissToast } = useToast();

  return (
    <div
      id="toast"
      role="status"
      aria-live="polite"
      className="fixed right-5 bottom-5 z-[60] flex flex-col gap-2 max-w-[min(420px,calc(100vw-32px))] pointer-events-none"
    >
      {toasts.map((t) => {
        const isErr = t.kind === 'error';
        return (
          <div
            key={t.id}
            className={[
              'pointer-events-auto px-3.5 py-2.5 rounded-[var(--radius-sm)] text-xs font-medium shadow-[var(--shadow-lg)] flex items-center justify-between gap-3 ds-page-enter',
              isErr
                ? 'bg-[var(--surface)] text-[var(--err)] border border-[var(--err)]'
                : 'bg-[var(--accent)] text-[var(--accent-fg)] border border-[var(--accent)]',
            ].join(' ')}
          >
            <span className="leading-snug break-words">{t.message}</span>
            <button
              type="button"
              aria-label="关闭通知"
              onClick={() => dismissToast(t.id)}
              className="opacity-70 hover:opacity-100 cursor-pointer shrink-0"
            >
              <X className="w-3.5 h-3.5" />
            </button>
          </div>
        );
      })}
    </div>
  );
}
