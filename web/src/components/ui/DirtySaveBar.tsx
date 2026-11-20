import { Button } from './Button';

interface DirtySaveBarProps {
  visible: boolean;
  saving?: boolean;
  message: string;
  subMessage?: string;
  onReset?: () => void;
  onSave: () => void;
  saveLabel?: string;
  saveButtonId?: string;
}

export function DirtySaveBar({
  visible,
  saving = false,
  message,
  subMessage,
  onReset,
  onSave,
  saveLabel = '保存并生效',
  saveButtonId,
}: DirtySaveBarProps) {
  if (!visible) return null;

  return (
    <div className="sticky bottom-4 z-30 mt-4 ds-page-enter">
      <div className="bg-[var(--surface)] border border-[var(--border-strong)] rounded-[var(--radius)] shadow-[var(--shadow-lg)] px-4 py-3 flex flex-wrap items-center justify-between gap-3">
        <div className="flex items-center gap-2.5 min-w-0">
          <span className="w-2 h-2 rounded-full bg-[var(--warn)] shrink-0" />
          <div className="text-xs min-w-0">
            <span className="font-medium text-[var(--fg)]">{message}</span>
            {subMessage && (
              <span className="text-[var(--fg-3)] ml-2 hidden sm:inline">{subMessage}</span>
            )}
          </div>
        </div>
        <div className="flex items-center gap-2 shrink-0">
          {onReset && (
            <Button variant="ghost" size="sm" disabled={saving} onClick={onReset}>
              放弃更改
            </Button>
          )}
          <Button
            id={saveButtonId}
            variant="primary"
            size="sm"
            loading={saving}
            onClick={onSave}
          >
            {saving ? '保存中…' : saveLabel}
          </Button>
        </div>
      </div>
    </div>
  );
}
