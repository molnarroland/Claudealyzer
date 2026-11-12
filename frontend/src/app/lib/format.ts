export const AGENTS: Record<string, string> = { claude: 'Claude', codex: 'Codex', gemini: 'Gemini', opencode: 'OpenCode' };
export const AGENT_COLOR: Record<string, string> = { claude: 'var(--s1)', codex: 'var(--s2)', gemini: 'var(--s3)', opencode: 'var(--s4)' };

/** Token types in a fixed order: color follows the type, never its rank. */
export const TOKEN_TYPES = [
  { key: 'input', cost: 'costInput', name: 'Input', color: 'var(--s1)' },
  { key: 'cacheWrite', cost: 'costCacheWrite', name: 'Cache write', color: 'var(--s2)' },
  { key: 'cacheRead', cost: 'costCacheRead', name: 'Cache read', color: 'var(--s3)' },
  { key: 'output', cost: 'costOutput', name: 'Output', color: 'var(--s4)' },
] as const;

export function fmtT(n: number | null | undefined): string {
  const v = n ?? 0;
  if (v >= 1e9) return (v / 1e9).toFixed(1) + 'B';
  if (v >= 1e6) return (v / 1e6).toFixed(1) + 'M';
  if (v >= 1e3) return Math.round(v / 1e3) + 'k';
  return String(Math.round(v));
}

export function fmtUSD(n: number | null | undefined): string {
  const v = n ?? 0;
  if (v === 0) return '$0';
  if (Math.abs(v) >= 1000) return '$' + Math.round(v).toLocaleString('en-US');
  if (Math.abs(v) >= 100) return '$' + v.toFixed(0);
  if (Math.abs(v) < 0.01) return '<$0.01';
  return '$' + v.toFixed(2);
}

export function fmtD(ms: number | null | undefined): string {
  const s = (ms ?? 0) / 1000;
  if (s >= 3600) return `${Math.floor(s / 3600)}h ${String(Math.floor((s % 3600) / 60)).padStart(2, '0')}m`;
  if (s >= 60) return `${Math.floor(s / 60)}m ${String(Math.round(s % 60)).padStart(2, '0')}s`;
  if (s === 0) return '';
  return `${s.toFixed(s < 10 ? 1 : 0)}s`;
}

export function fmtWhen(ms: number | null | undefined): string {
  if (!ms) return '';
  const d = new Date(ms);
  const now = new Date();
  const time = d.toLocaleTimeString('en-GB', { hour: '2-digit', minute: '2-digit' });
  const day = (x: Date) => new Date(x.getFullYear(), x.getMonth(), x.getDate()).getTime();
  const diff = (day(now) - day(d)) / 864e5;
  if (diff === 0) return `Today ${time}`;
  if (diff === 1) return `Yesterday ${time}`;
  const sameYear = d.getFullYear() === now.getFullYear();
  return d.toLocaleDateString('en-GB', { day: 'numeric', month: 'short', ...(sameYear ? {} : { year: 'numeric' }) }) + ' ' + time;
}

export function fmtDay(iso: string): string {
  const d = new Date(iso + 'T00:00:00');
  return d.toLocaleDateString('en-GB', { day: 'numeric', month: 'short' });
}

export function pct(a: number, b: number): string {
  if (!b) return '—';
  return Math.round((a / b) * 100) + '%';
}

export function total(u: { input: number; cacheWrite5m: number; cacheWrite1h: number; cacheRead: number; output: number } | null | undefined): number {
  if (!u) return 0;
  return u.input + u.cacheWrite5m + u.cacheWrite1h + u.cacheRead + u.output;
}

export function ctx(u: { input: number; cacheWrite5m: number; cacheWrite1h: number; cacheRead: number } | null | undefined): number {
  if (!u) return 0;
  return u.input + u.cacheWrite5m + u.cacheWrite1h + u.cacheRead;
}

export function shortModel(m: string | null | undefined): string {
  return (m ?? '').replace(/^claude-/, '');
}

export function errText(e: unknown): string {
  if (e && typeof e === 'object' && 'message' in e) return String((e as { message: unknown }).message);
  return String(e);
}
