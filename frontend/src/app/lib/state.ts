import { Injectable, computed, signal } from '@angular/core';
import { Events } from '@wailsio/runtime';
import { HistoryService, SourceService, core, m } from './api';
import { errText } from './format';

export type View = 'history' | 'analysis' | 'overview' | 'calendar' | 'costs' | 'settings';

/** Views in sidebar and View-menu order; `key` is the digit of the mod+N shortcut. */
export const VIEWS: { id: View; label: string; icon: string; key: string }[] = [
  { id: 'history', label: 'History', icon: 'lucideMessagesSquare', key: '1' },
  { id: 'analysis', label: 'Session analysis', icon: 'lucideChartColumn', key: '2' },
  { id: 'costs', label: 'Cost explained', icon: 'lucideCircleDollarSign', key: '3' },
  { id: 'overview', label: 'Overview', icon: 'lucideLayoutGrid', key: '4' },
  { id: 'calendar', label: 'Calendar', icon: 'lucideCalendarDays', key: '5' },
  { id: 'settings', label: 'Sources & settings', icon: 'lucideSettings', key: ',' },
];
export type PanelScope = { kind: 'Session' | 'Subagent' | 'Turn'; name: string; usage: m.Usage; turns: m.Turn[] };

/** App-wide UI state. Data is fetched from the Go services on selection changes. */
@Injectable({ providedIn: 'root' })
export class AppState {
  readonly view = signal<View>('history');
  readonly sourceId = signal('all');
  readonly agent = signal('all');
  readonly sources = signal<core.SourceInfo[]>([]);
  readonly projects = signal<core.Project[]>([]);
  readonly projectKey = signal<string | null>(null);
  readonly sessions = signal<core.SessionRow[]>([]);
  /** Sessions of every project opened in the sidebar tree, by project key (the selected one included). */
  readonly projectSessions = signal<Record<string, core.SessionRow[]>>({});
  readonly sessionSort = signal<'recent' | 'cost'>('recent');
  readonly current = signal<{ sourceId: string; id: string } | null>(null);
  readonly detail = signal<core.SessionDetail | null>(null);
  readonly loading = signal(false);
  readonly error = signal<string | null>(null);
  readonly subagentId = signal<string | null>(null);
  readonly subagent = signal<m.Subagent | null>(null);
  readonly turn = signal<number | null>(null);
  readonly sheetStep = signal<string | null>(null);
  readonly panelOpen = signal(true);
  readonly panelTab = signal<'usage' | 'insights'>('usage');
  readonly scan = signal<core.ScanStatus | null>(null);
  readonly scrollToTurn = signal<{ turn: number; n: number } | null>(null);
  /** Bumped by File → Add source…; Settings opens its sheet when it changes. */
  readonly addSourceRequest = signal(0);
  /** Calendar: shown month (YYYY-MM, "" = current), opened day (YYYY-MM-DD) and scope. Kept here so they survive view switches. */
  readonly calMonth = signal('');
  readonly calDay = signal<string | null>(null);
  readonly calScope = signal<'project' | 'source'>('source');

  readonly project = computed(() => this.projects().find((p) => p.key === this.projectKey()) ?? null);
  readonly session = computed(() => this.detail()?.session ?? null);
  /** The transcript being shown: a subagent's, or the session's. */
  readonly turns = computed<m.Turn[]>(() => (this.subagentId() ? this.subagent()?.turns : this.session()?.turns) ?? []);

  readonly scope = computed<PanelScope | null>(() => {
    const s = this.session();
    if (!s) return null;
    const t = this.turn();
    const turns = this.turns();
    if (t != null && turns[t]) {
      const u = { ...turns[t].usage };
      addUsage(u, turns[t].subagentUsage);
      return { kind: 'Turn', name: `Turn ${t + 1} · ${turns[t].prompt}`, usage: u, turns: [turns[t]] };
    }
    const sa = this.subagent();
    if (this.subagentId() && sa) return { kind: 'Subagent', name: sa.description || sa.type, usage: sa.usage, turns };
    return { kind: 'Session', name: s.title, usage: s.usage, turns };
  });

  private seq = 0;

  async init(): Promise<void> {
    Events.On('scan:progress', (e) => this.scan.set(e.data as core.ScanStatus));
    Events.On('index:updated', (e) => {
      this.scan.set(e.data as core.ScanStatus);
      void this.refresh();
    });
    Events.On('session:updated', (e) => {
      const keys = (e.data as string[]) ?? [];
      const c = this.current();
      void this.refreshLists();
      if (c && keys.includes(`${c.sourceId}/${c.id}`)) void this.openSession(c.sourceId, c.id, true);
    });
    this.scan.set(await SourceService.Status());
    await this.refresh();
    await this.applyHash();
  }

  /** Deep links for development and screenshots: #view=analysis&turn=0&step=1&sort=cost, #view=calendar&day=2026-11-10 */
  private async applyHash(): Promise<void> {
    const h = new URLSearchParams(location.hash.slice(1));
    const pn = h.get('project');
    const p = pn && this.projects().find((x) => x.name === pn);
    if (p) await this.selectProject(p.key);
    if (h.get('sort') === 'cost') {
      await this.setSort('cost');
      const s = this.sessions()[0];
      if (s) await this.openSession(s.sourceId, s.id);
    }
    const v = h.get('view') as View | null;
    if (v) this.view.set(v);
    const day = h.get('day');
    if (day && /^\d{4}-\d{2}-\d{2}$/.test(day)) {
      this.calDay.set(day);
      this.calMonth.set(day.slice(0, 7));
    }
    const t = h.get('turn');
    if (t != null) this.goTurn(+t);
    const st = h.get('step');
    if (t != null && st != null) {
      const step = this.turns()[+t]?.steps?.filter((x) => x.kind !== 'thinking' && x.kind !== 'text')[+st];
      if (step) this.openStep(step.id, +t);
    }
  }

  async refresh(): Promise<void> {
    this.sources.set((await SourceService.List()) ?? []);
    await this.refreshLists();
    if (!this.current()) {
      const first = this.sessions()[0];
      if (first) await this.openSession(first.sourceId, first.id);
    }
  }

  async refreshLists(): Promise<void> {
    const ps = (await HistoryService.Projects(this.sourceId(), this.agent())) ?? [];
    this.projects.set(ps);
    if (!this.projectKey() || !ps.some((p) => p.key === this.projectKey())) {
      this.projectKey.set(ps[0]?.key ?? null);
    }
    await this.reloadProjectSessions();
  }

  /** Reloads the sessions of every project already loaded in the tree, plus the selected one. */
  private async reloadProjectSessions(): Promise<void> {
    const keys = new Set(Object.keys(this.projectSessions()).filter((k) => this.projects().some((p) => p.key === k)));
    this.projectSessions.set({});
    const sel = this.projectKey();
    if (sel != null) keys.delete(sel);
    await Promise.all([this.loadSessions(), ...[...keys].map((k) => this.loadProjectSessions(k))]);
  }

  /** Loads one project's sessions into the tree (on expanding it), with the current filters and sort. */
  async loadProjectSessions(key: string): Promise<core.SessionRow[]> {
    const rows = (await HistoryService.Sessions(key, this.sourceId(), this.agent(), this.sessionSort())) ?? [];
    this.projectSessions.update((m) => ({ ...m, [key]: rows }));
    return rows;
  }

  async loadSessions(): Promise<void> {
    const key = this.projectKey();
    if (key == null) {
      this.sessions.set([]);
      return;
    }
    const rows = await this.loadProjectSessions(key);
    if (key === this.projectKey()) this.sessions.set(rows);
  }

  async selectSource(id: string): Promise<void> {
    this.sourceId.set(id);
    this.projectKey.set(null);
    await this.refreshLists();
    const s = this.sessions()[0];
    if (s) await this.openSession(s.sourceId, s.id);
  }

  async selectAgent(a: string): Promise<void> {
    this.agent.set(a);
    await this.refreshLists();
    const c = this.current();
    if (!c || !this.sessions().some((s) => s.sourceId === c.sourceId && s.id === c.id)) {
      const s = this.sessions()[0];
      if (s) await this.openSession(s.sourceId, s.id);
    }
  }

  async selectProject(key: string): Promise<void> {
    this.projectKey.set(key);
    await this.loadSessions();
    const s = this.sessions()[0];
    if (s) await this.openSession(s.sourceId, s.id);
  }

  async setSort(s: 'recent' | 'cost'): Promise<void> {
    this.sessionSort.set(s);
    await this.reloadProjectSessions();
  }

  async openSession(sourceId: string, id: string, keepScope = false): Promise<void> {
    const n = ++this.seq;
    this.current.set({ sourceId, id });
    if (!keepScope) {
      this.turn.set(null);
      this.subagentId.set(null);
      this.subagent.set(null);
      this.sheetStep.set(null);
    }
    this.loading.set(!keepScope);
    this.error.set(null);
    try {
      const d = await HistoryService.Session(sourceId, id);
      if (n !== this.seq) return;
      this.detail.set(d);
      if (keepScope && this.subagentId()) await this.openSubagent(this.subagentId()!);
    } catch (e) {
      if (n === this.seq) this.error.set(errText(e));
    } finally {
      if (n === this.seq) this.loading.set(false);
    }
  }

  async openSubagent(id: string | null): Promise<void> {
    this.turn.set(null);
    this.sheetStep.set(null);
    this.subagentId.set(id);
    this.subagent.set(null);
    const c = this.current();
    if (!id || !c) return;
    try {
      this.subagent.set(await HistoryService.Subagent(c.sourceId, c.id, id));
    } catch (e) {
      this.error.set(errText(e));
    }
  }

  goTurn(turn: number, subagentId: string | null = null): void {
    const go = () => {
      this.turn.set(turn);
      this.view.set('history');
      this.scrollToTurn.set({ turn, n: Date.now() });
    };
    if ((subagentId ?? null) !== this.subagentId()) void this.openSubagent(subagentId).then(go);
    else go();
  }

  /** Open a session of any project (Overview, Calendar, the sidebar tree), optionally at a turn; `show` switches to History. */
  async openFrom(projectKey: string, sourceId: string, id: string, turn: number | null = null, show = true): Promise<void> {
    if (projectKey !== this.projectKey()) {
      this.projectKey.set(projectKey);
      void this.loadSessions();
    }
    if (show) this.view.set('history');
    await this.openSession(sourceId, id);
    if (turn != null) this.goTurn(turn);
  }

  go(v: View): void {
    this.view.set(v);
    this.sheetStep.set(null);
    if (v === 'analysis') this.turn.set(null);
  }

  requestAddSource(): void {
    this.go('settings');
    this.addSourceRequest.update((n) => n + 1);
  }

  openStep(stepId: string, turn: number): void {
    this.turn.set(turn);
    this.sheetStep.set(stepId);
  }
}

export function addUsage(a: m.Usage, b: m.Usage | null | undefined): void {
  if (!b) return;
  for (const k of Object.keys(b) as (keyof m.Usage)[]) {
    (a[k] as number) = ((a[k] as number) ?? 0) + ((b[k] as number) ?? 0);
  }
}
