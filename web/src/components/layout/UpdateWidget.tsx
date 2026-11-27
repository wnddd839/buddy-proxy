import { useCallback, useEffect, useRef, useState } from 'react';
import { Download, RefreshCw, X } from 'lucide-react';
import { updateFetch } from '@/lib/api';
import { useConfirm } from '@/hooks/useConfirm';
import { Button } from '@/components/ui/Button';
import { StatusBadge } from '@/components/ui/StatusBadge';
import type { UpdateState } from '@/types/api';

interface UpdateWidgetProps {
  version: string;
  uiRevision: string;
}

export function UpdateWidget({ version, uiRevision }: UpdateWidgetProps) {
  const { confirm } = useConfirm();
  const [state, setState] = useState<UpdateState>({});
  const [message, setMessage] = useState('');
  const [checking, setChecking] = useState(false);
  const pollingRef = useRef(false);

  const applyState = useCallback((s: UpdateState) => {
    setState(s);
    setMessage(
      s.message ||
        (s.available ? `可更新至 ${s.latest}` : s.latest ? '已是最新版本' : ''),
    );
  }, []);

  // 仅读取本地内存状态（Docker / dev 说明），绝不自动访问 GitHub
  useEffect(() => {
    updateFetch<UpdateState>('/direct-admin/api/update')
      .then(applyState)
      .catch(() => {});
  }, [applyState]);

  const busy =
    checking ||
    state.phase === 'checking' ||
    state.phase === 'downloading' ||
    state.phase === 'restarting';

  const handleCheck = async () => {
    setChecking(true);
    setMessage('正在检查更新…');
    try {
      const s = await updateFetch<UpdateState>(
        '/direct-admin/api/update/check',
        { method: 'POST' },
        30000,
      );
      applyState(s);
    } catch (e: unknown) {
      setMessage(e instanceof Error ? e.message : String(e));
    } finally {
      setChecking(false);
    }
  };

  const handleCancel = async () => {
    try {
      const s = await updateFetch<UpdateState>('/direct-admin/api/update/cancel', {
        method: 'POST',
      });
      applyState(s);
    } catch (e: unknown) {
      setMessage(e instanceof Error ? e.message : String(e));
    }
  };

  const handleInstall = async () => {
    const ok = await confirm({
      title: '更新并重启服务',
      description:
        '更新将排空当前进行中的请求并自动重启，仅替换可执行文件，不会覆盖 .env、账号池或用量数据。继续？',
      confirmLabel: '开始更新并重启',
    });
    if (!ok) return;

    try {
      const before = (await updateFetch<UpdateState>('/direct-admin/api/update')).current;
      const started = await updateFetch<UpdateState>('/direct-admin/api/update/install', {
        method: 'POST',
      });
      applyState(started);
      if (pollingRef.current) return;
      pollingRef.current = true;

      const deadline = Date.now() + 240_000;
      while (Date.now() < deadline) {
        await new Promise((r) => window.setTimeout(r, 1000));
        try {
          const s = await updateFetch<UpdateState>('/direct-admin/api/update');
          if (s.current && before && s.current !== before) {
            setMessage(`更新完成：${s.current}`);
            window.location.reload();
            return;
          }
          if (s.phase === 'error') {
            applyState(s);
            return;
          }
          if (s.phase === 'idle') {
            applyState(s);
            setMessage(
              s.message ||
                '服务已重连但版本未变化，请检查安装目录更新日志（可能已回滚）',
            );
            return;
          }
          applyState(s);
        } catch {
          setMessage('等待服务重新连接…');
        }
      }
      setMessage(
        '重连超时，请检查安装目录更新日志和原程序备份；不要重复启动服务',
      );
    } catch (e: unknown) {
      setMessage(e instanceof Error ? e.message : String(e));
    } finally {
      pollingRef.current = false;
    }
  };

  return (
    <div className="ds-surface-2 p-3 space-y-2">
      <div className="flex items-center justify-between gap-2">
        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-1.5 min-w-0">
            <span
              id="pillVersion"
              title={uiRevision ? `UI · ${uiRevision}` : version}
              className="ds-mono text-xs font-medium text-[var(--fg)] truncate"
            >
              {version || 'dev'}
            </span>
            {state.available && (
              <StatusBadge tone="info" className="!px-1.5 !py-0 !text-[10px]">
                {state.latest || '可更新'}
              </StatusBadge>
            )}
          </div>
          <div className="text-[11px] text-[var(--fg-3)] mt-0.5 truncate">
            SHA-256 校验更新
          </div>
        </div>
        <Button
          id="updateCheck"
          variant="ghost"
          size="xs"
          disabled={busy}
          onClick={handleCheck}
          title="仅点击时访问 GitHub Release 检查更新"
        >
          <RefreshCw className={`w-3 h-3 shrink-0 ${busy ? 'animate-spin' : ''}`} />
          检查更新
        </Button>
      </div>

      {(state.available || state.phase === 'downloading') && (
        <div className="flex items-center gap-1.5">
          {state.available && (
            <Button
              id="updateInstall"
              variant="primary"
              size="xs"
              disabled={busy}
              onClick={handleInstall}
              className="flex-1"
            >
              <Download className="w-3 h-3" />
              更新并重启
            </Button>
          )}
          {state.phase === 'downloading' && (
            <Button
              id="updateCancel"
              variant="danger"
              size="xs"
              onClick={handleCancel}
            >
              <X className="w-3 h-3" />
              取消下载
            </Button>
          )}
        </div>
      )}

      {message && (
        <p
          id="updateMessage"
          role="status"
          aria-live="polite"
          className="text-[11px] leading-snug text-[var(--fg-2)] break-words m-0"
        >
          {message}
        </p>
      )}
    </div>
  );
}
