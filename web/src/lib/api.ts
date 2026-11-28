export async function apiRequest<T = unknown>(
  path: string,
  opts: RequestInit = {},
  timeoutMs = 20000,
): Promise<T> {
  const headers: Record<string, string> = {
    'content-type': 'application/json',
    ...((opts.headers as Record<string, string>) || {}),
  };
  // 管理接口都是本机/内网调用：20s 足够。没有超时时请求一挂死，
  // 页面就永远停在「加载中…」，既不报错也不恢复。
  const ctrl = new AbortController();
  const timer = window.setTimeout(() => ctrl.abort(), timeoutMs);
  try {
    const res = await fetch(path, {
      ...opts,
      headers,
      credentials: 'same-origin',
      signal: opts.signal ?? ctrl.signal,
    });
    const text = await res.text();
    let data: Record<string, unknown> = {};
    if (text) {
      try {
        data = JSON.parse(text) as Record<string, unknown>;
      } catch {
        throw new Error(
          res.ok
            ? `响应不是 JSON（可能是网关错误页）：${text.slice(0, 120)}`
            : `HTTP ${res.status}（响应不是 JSON，可能是网关错误页）：${text.slice(0, 120)}`,
        );
      }
    }
    // 200 + {"ok":false} 也是失败：这类响应如果当成功，界面会显示
    // 「已保存」并继续用旧数据，属于本分支最忌讳的静默失效。
    if (data && typeof data === 'object' && (data as { ok?: unknown }).ok === false) {
      const err = (data as { error?: unknown }).error;
      throw new Error(typeof err === 'string' && err ? err : '请求返回 ok:false');
    }
    if (!res.ok) {
      const errObj = data?.error as { message?: string } | string | undefined;
      const msg =
        (typeof errObj === 'object' && errObj?.message) ||
        (typeof data?.message === 'string' && data.message) ||
        (typeof errObj === 'string' && errObj) ||
        `HTTP ${res.status}`;
      throw new Error(msg);
    }
    return data as T;
  } catch (e) {
    if ((e as { name?: string })?.name === 'AbortError') {
      throw new Error(`请求超时（${Math.round(timeoutMs / 1000)}s）：${path}`);
    }
    throw e;
  } finally {
    window.clearTimeout(timer);
  }
}

/**
 * 更新请求单独使用 AbortController 超时控制：
 * 重启交接期间旧连接可能长时间挂起，默认 3 秒用于本地轮询，检查 Release 时放宽至 30 秒。
 */
export async function updateFetch<T = unknown>(
  path: string,
  opts: RequestInit = {},
  timeoutMs = 3000,
): Promise<T> {
  const ctrl = new AbortController();
  const timer = window.setTimeout(() => ctrl.abort(), timeoutMs);
  try {
    const res = await fetch(path, {
      ...opts,
      credentials: 'same-origin',
      signal: ctrl.signal,
    });
    const text = await res.text();
    let data: Record<string, unknown> = {};
    if (text) {
      try {
        data = JSON.parse(text) as Record<string, unknown>;
      } catch {
        throw new Error(`响应不是 JSON（可能是网关错误页）：${text.slice(0, 120)}`);
      }
    }
    if (!res.ok) {
      const errObj = data?.error as { message?: string } | string | undefined;
      const msg =
        (typeof errObj === 'object' && errObj?.message) ||
        (typeof data?.message === 'string' && data.message) ||
        (typeof errObj === 'string' && errObj) ||
        `HTTP ${res.status}`;
      throw new Error(msg);
    }
    return data as T;
  } catch (e: unknown) {
    if (e instanceof DOMException && e.name === 'AbortError') {
      throw new Error(`请求超时（${Math.round(timeoutMs / 1000)} 秒）`);
    }
    if (e instanceof Error && e.name === 'AbortError') {
      throw new Error(`请求超时（${Math.round(timeoutMs / 1000)} 秒）`);
    }
    throw e;
  } finally {
    window.clearTimeout(timer);
  }
}

export async function copyToClipboard(text: string): Promise<void> {
  const value = String(text || '');
  if (!value) throw new Error('内容为空');
  if (navigator.clipboard && window.isSecureContext) {
    await navigator.clipboard.writeText(value);
    return;
  }
  const ta = document.createElement('textarea');
  ta.value = value;
  ta.style.position = 'fixed';
  ta.style.opacity = '0';
  document.body.appendChild(ta);
  ta.select();
  document.execCommand('copy');
  ta.remove();
}
