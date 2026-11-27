import React, { createContext, useCallback, useContext, useRef, useState } from 'react';
import { Modal } from '@/components/ui/Modal';
import { Button } from '@/components/ui/Button';

export interface ConfirmOptions {
  title: string;
  description: string;
  confirmLabel?: string;
  cancelLabel?: string;
  danger?: boolean;
}

interface ConfirmContextValue {
  confirm: (opts: ConfirmOptions) => Promise<boolean>;
}

const ConfirmContext = createContext<ConfirmContextValue | null>(null);

export function ConfirmProvider({ children }: { children: React.ReactNode }) {
  const [pending, setPending] = useState<ConfirmOptions | null>(null);
  const resolverRef = useRef<((value: boolean) => void) | null>(null);

  const confirm = useCallback((opts: ConfirmOptions) => {
    return new Promise<boolean>((resolve) => {
      resolverRef.current = resolve;
      setPending(opts);
    });
  }, []);

  const settle = useCallback((result: boolean) => {
    setPending(null);
    const resolve = resolverRef.current;
    resolverRef.current = null;
    if (resolve) resolve(result);
  }, []);

  return (
    <ConfirmContext.Provider value={{ confirm }}>
      {children}
      <Modal
        open={!!pending}
        onClose={() => settle(false)}
        title={pending?.title || '请确认操作'}
        maxWidth="max-w-md"
      >
        <p className="text-[13px] leading-relaxed whitespace-pre-line text-[var(--fg-2)]">
          {pending?.description}
        </p>
        <div className="mt-5 flex items-center justify-end gap-2">
          <Button variant="ghost" onClick={() => settle(false)}>
            {pending?.cancelLabel || '取消'}
          </Button>
          <Button
            variant={pending?.danger ? 'dangerSolid' : 'primary'}
            onClick={() => settle(true)}
          >
            {pending?.confirmLabel || '确认继续'}
          </Button>
        </div>
      </Modal>
    </ConfirmContext.Provider>
  );
}

export function useConfirm() {
  const ctx = useContext(ConfirmContext);
  if (!ctx) throw new Error('useConfirm must be used within ConfirmProvider');
  return ctx;
}
