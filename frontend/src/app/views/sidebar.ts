import { ChangeDetectionStrategy, Component, computed, effect, inject, input, linkedSignal, signal, untracked } from '@angular/core';
import { NgIcon } from '@ng-icons/core';
import { SnContextMenu, SnMenuItem, SnSidebarNavImports, SnTooltip, SnTree, SnTreeItem, SnWindowControls } from '@spartan-native/desktop-ui';
import { HlmButtonImports } from '@spartan-ng/helm/button';
import { HlmInputImports } from '@spartan-ng/helm/input';
import { HlmProgressImports } from '@spartan-ng/helm/progress';
import { ExportService, core } from '../lib/api';
import { AGENTS, errText, fmtUSD, fmtWhen, shortModel } from '../lib/format';
import { AppState, RangePreset, VIEWS, presetRange } from '../lib/state';
import { Segmented } from './shared';
import { version } from '../../../package.json';

/** Source picker, agent, model and date filters: they scope every view, so they live in the window's title bar. */
@Component({
  selector: 'cz-scope-filters',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [NgIcon, HlmInputImports, HlmButtonImports, SnTooltip],
  host: { class: 'flex items-center gap-2' },
  template: `
    <select hlmInput class="h-7 w-32 text-[13px]" aria-label="Model filter" [snTooltip]="'Only sessions that used this model'" (change)="state.setFilter({ model: $any($event.target).value })">
      <option value="" [selected]="!state.filter().model">All models</option>
      @for (mi of state.models(); track mi.model) {
        <option [value]="mi.model" [selected]="state.filter().model === mi.model">{{ shortModel(mi.model) }} · {{ mi.sessions }}</option>
      }
      @if (state.filter().model && !hasModel()) { <option [value]="state.filter().model" selected>{{ shortModel(state.filter().model) }}</option> }
    </select>
    <select hlmInput class="h-7 w-28 text-[13px]" aria-label="Date range" [snTooltip]="'Only sessions active in this range'" (change)="setPreset($any($event.target).value)">
      @for (p of presets; track p.id) { <option [value]="p.id" [selected]="state.rangePreset() === p.id">{{ p.label }}</option> }
    </select>
    @if (state.rangePreset() === 'custom') {
      <input hlmInput type="date" class="h-7 w-32 text-[12px]" aria-label="From" [value]="state.filter().from" (change)="state.setFilter({ from: $any($event.target).value })" />
      <input hlmInput type="date" class="h-7 w-32 text-[12px]" aria-label="To" [value]="state.filter().to" (change)="state.setFilter({ to: $any($event.target).value })" />
    }
    @if (state.filtered()) {
      <button hlmBtn variant="ghost" size="icon-sm" snTooltip="Clear filters" aria-label="Clear filters" (click)="state.clearFilter()"><ng-icon name="lucideFilterX" /></button>
    }
    <select hlmInput class="h-7 w-44 text-[13px]" aria-label="Source" (change)="state.selectSource($any($event.target).value)">
      <option value="all" [selected]="state.sourceId() === 'all'">All sources · {{ state.sources().length }} folders</option>
      @for (s of state.sources(); track s.id) {
        <option [value]="s.id" [selected]="state.sourceId() === s.id">{{ s.name }} · {{ AGENTS[s.agent] ?? s.agent }} · {{ s.dirShort }}</option>
      }
    </select>
    @if (agents().length > 2) {
      <select hlmInput class="h-7 w-32 text-[13px]" aria-label="Agent filter" (change)="state.selectAgent($any($event.target).value)">
        @for (a of agents(); track a.id) { <option [value]="a.id" [selected]="state.agent() === a.id">{{ a.id === 'all' ? 'All agents' : a.label }}</option> }
      </select>
    }
  `,
})
export class ScopeFilters {
  protected readonly state = inject(AppState);
  protected readonly AGENTS = AGENTS;
  protected readonly shortModel = shortModel;
  protected readonly presets: { id: RangePreset; label: string }[] = [
    { id: 'any', label: 'Any time' },
    { id: 'today', label: 'Today' },
    { id: '7d', label: 'Last 7 days' },
    { id: '30d', label: 'Last 30 days' },
    { id: 'month', label: 'This month' },
    { id: 'lastMonth', label: 'Last month' },
    { id: 'custom', label: 'Custom…' },
  ];
  readonly hasModel = computed(() => this.state.models().some((m) => m.model === this.state.filter().model));
  setPreset(p: RangePreset): void {
    if (p === 'custom') {
      const cur = this.state.filter();
      const r = cur.from || cur.to ? { from: cur.from, to: cur.to } : presetRange('30d');
      void this.state.setFilter(r, 'custom');
      return;
    }
    void this.state.setFilter(presetRange(p), p);
  }
  readonly agents = computed(() => {
    const have = new Set(this.state.sources().map((s) => s.agent as string));
    return [{ id: 'all', label: 'All' }, ...['claude', 'codex', 'gemini', 'opencode'].filter((a) => have.has(a)).map((a) => ({ id: a, label: AGENTS[a] }))];
  });
}

/** The current view's own commands, in the title bar (History: analysis, cost, the usage panel). */
@Component({
  selector: 'cz-view-commands',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [NgIcon, HlmButtonImports, SnTooltip],
  host: { class: 'contents' },
  template: `
    @if (state.view() !== 'search') {
      <button hlmBtn variant="ghost" size="icon-sm" snTooltip="Search transcripts (⌘K)" aria-label="Search transcripts" (click)="state.openSearch()"><ng-icon name="lucideSearch" /></button>
    }
    @if (state.view() === 'history') {
      <span role="toolbar" aria-label="Session commands" class="flex items-center gap-0.5">
        <button hlmBtn variant="ghost" size="icon-sm" snTooltip="Export as Markdown" aria-label="Export session as Markdown" [disabled]="!state.current()" (click)="exportMd()"><ng-icon name="lucideDownload" /></button>
        <button hlmBtn variant="ghost" size="icon-sm" snTooltip="Session analysis" aria-label="Session analysis" [disabled]="!state.current()" (click)="state.go('analysis')"><ng-icon name="lucideChartColumn" /></button>
        <button hlmBtn variant="ghost" size="icon-sm" snTooltip="Cost explained" aria-label="Cost explained" [disabled]="!state.current()" (click)="state.go('costs')"><ng-icon name="lucideCircleDollarSign" /></button>
        <button hlmBtn variant="ghost" size="icon-sm" [snTooltip]="state.panelOpen() ? 'Hide usage & insights' : 'Show usage & insights'" aria-label="Usage and insights panel"
          [attr.aria-pressed]="state.panelOpen()" (click)="state.panelOpen.set(!state.panelOpen())"><ng-icon name="lucidePanelRight" /></button>
      </span>
    }
  `,
})
export class ViewCommands {
  protected readonly state = inject(AppState);
  exportMd(): void {
    const c = this.state.current();
    if (c) void this.state.exported(ExportService.SessionMarkdown(c.sourceId, c.id, true), 'Markdown');
  }
}

const clip = (t = '') => (t.length > 32 ? t.slice(0, 31) + '…' : t);

/** Tree values: a project folder, or a session leaf. */
const pv = (key: string) => 'p:' + key;
const sv = (s: { sourceId: string; id: string }) => `s:${s.sourceId}/${s.id}`;

/**
 * App navigation as one sidebar (the macOS 26 skin draws it as a single inset glass panel):
 * the views, then the projects as a tree (folders) with their sessions (leaves), then settings at the foot.
 */
@Component({
  selector: 'cz-sidebar',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [NgIcon, SnSidebarNavImports, SnTooltip, SnTree, SnTreeItem, SnContextMenu, SnWindowControls, HlmInputImports, HlmProgressImports, Segmented],
  host: { class: 'flex min-h-0' },
  template: `
    <!-- Only the project tree scrolls: views, the search field and settings stay put. -->
    <sn-sidebar-nav aria-label="Navigation" class="sn-sidebar min-h-0" style="overflow: hidden; inline-size: 18rem">
      @if (windowChrome() !== 'none') {
        <!-- Mac look: the window's top strip. Draggable; the traffic lights are the OS's own ('native') or drawn here. -->
        <div class="window-strip flex h-7 shrink-0 items-center px-1.5">
          @if (windowChrome() === 'drawn') { <sn-window-controls /> }
        </div>
      }
      <sn-sidebar-section class="shrink-0" [style.margin-block-start]="windowChrome() === 'none' ? '0.5rem' : null">
        @for (v of views; track v.id) {
          <button snSidebarItem [icon]="v.icon" [active]="state.view() === v.id" [attr.aria-current]="state.view() === v.id ? 'page' : null" (click)="state.go(v.id)">
            {{ v.label }}
          </button>
        }
      </sn-sidebar-section>

      <sn-sidebar-section title="Projects" style="display: flex; flex-direction: column; flex: 1 1 0; min-height: 0">
        <div class="mx-1 mb-1 flex shrink-0 items-center gap-1.5">
          <input hlmInput type="search" class="h-7 min-w-0 flex-1 text-[13px]" placeholder="Filter projects" aria-label="Filter projects and sessions"
            [value]="q()" (input)="q.set($any($event.target).value)" (keydown.enter)="q().trim() && state.openSearch(q().trim())" />
          <cz-segmented label="Sort sessions" [options]="sorts" [value]="state.sessionSort()" (valueChange)="state.setSort($event)" />
        </div>
        <div class="min-h-0 flex-1 overflow-x-hidden overflow-y-auto scroll-thin">
          <sn-tree aria-label="Projects and sessions" [selection]="sel()" (selectionChange)="select($event)" (activated)="activate($event)">
            @for (p of filtered(); track p.key) {
              <sn-tree-item [value]="pv(p.key)" [label]="p.name" icon="lucideFolder" [expandable]="p.sessionCount > 0"
                [expanded]="isOpen(p.key)" (expandedChange)="setOpen(p.key, $event)" [snTooltip]="projectTip(p)"
                [snContextMenu]="projectMenu(p)" [attr.data-slot]="'sn-tree-item'">
                <span snTreeItemEnd class="tabular text-[11px] opacity-70">{{ p.unpriced && !p.costUSD ? '—' : fmtUSD(p.costUSD) }}</span>
                @for (s of p.sessions; track sv(s)) {
                  <!-- snContextMenu sets its own data-slot, which would drop the tree item's styles: bind it back -->
                  <sn-tree-item [value]="sv(s)" [label]="s.title || 'Untitled session'" [snContextMenu]="menuFor(s)" [attr.data-slot]="'sn-tree-item'" [snTooltip]="sessionTip(s)">
                    <span snTreeItemEnd class="flex items-center gap-1.5">
                      @if (s.findingsHigh) {
                        <span class="size-1.5 shrink-0 rounded-full bg-[var(--crit)]" [attr.aria-label]="s.findingsHigh + ' high findings'"></span>
                      }
                      <span class="tabular text-[11px] opacity-70">{{ s.noUsageData ? '—' : fmtUSD(s.usage.costUSD) }}</span>
                    </span>
                  </sn-tree-item>
                }
              </sn-tree-item>
            }
          </sn-tree>
          @if (q().trim()) {
            <button type="button" class="mx-1 mt-1 flex w-[calc(100%-0.5rem)] cursor-pointer items-center gap-1.5 rounded-md px-2 py-1 text-left text-[12.5px] text-muted-foreground hover:bg-accent"
              (click)="state.openSearch(q().trim())">
              <ng-icon name="lucideSearch" class="shrink-0" /> <span class="truncate">Search all transcripts for “{{ q().trim() }}”</span>
            </button>
          }
          @if (!filtered().length) {
            <p class="m-0 px-2 py-1 text-[13px] text-muted-foreground">
              @if (state.scan()?.running) { Reading history… } @else if (q()) { Nothing matches. } @else { No sessions yet. Add a source in Settings. }
            </p>
          }
        </div>
      </sn-sidebar-section>

      @if (state.scan()?.running) {
        <div class="grid shrink-0 gap-1 px-2 pb-1 text-xs text-muted-foreground">
          <span>Reading {{ state.scan()!.source }} · {{ state.scan()!.done }} / {{ state.scan()!.total }}</span>
          <hlm-progress class="h-1" [value]="(state.scan()!.done / (state.scan()!.total || 1)) * 100"><hlm-progress-indicator /></hlm-progress>
        </div>
      }
      <sn-sidebar-section class="shrink-0 border-t pt-1">
        <button snSidebarItem [icon]="settings.icon" [active]="state.view() === 'settings'" [attr.aria-current]="state.view() === 'settings' ? 'page' : null" (click)="state.go('settings')">
          {{ settings.label }}
        </button>
        <span class="tabular block px-2 pb-1 text-[11px] text-muted-foreground">Claudealyzer v{{ version }}</span>
      </sn-sidebar-section>
    </sn-sidebar-nav>
  `,
})
export class Sidebar {
  protected readonly state = inject(AppState);
  /** Mac look only: the sidebar's top strip holds the traffic lights (the OS's own, or drawn). */
  readonly windowChrome = input<'none' | 'native' | 'drawn'>('none');
  protected readonly fmtUSD = fmtUSD;
  protected readonly version = version;
  protected readonly views = VIEWS.filter((v) => v.id !== 'settings' && v.id !== 'search');
  protected readonly settings = VIEWS.find((v) => v.id === 'settings')!;
  readonly q = signal('');
  protected readonly pv = pv;
  protected readonly sv = sv;
  protected readonly sorts = [
    { id: 'recent' as const, label: 'Recent' },
    { id: 'cost' as const, label: 'Cost' },
  ];
  /** Projects the user opened in the tree; the selected project is always opened. */
  private readonly open = signal(new Set<string>());

  constructor() {
    effect(() => {
      const k = this.state.projectKey();
      if (k != null) untracked(() => this.setOpen(k, true));
    });
  }

  /** Projects with the sessions to show under each; a search matches project names and paths, and the titles of loaded sessions. */
  readonly filtered = computed(() => {
    const q = this.q().trim().toLowerCase();
    const all = this.state.projectSessions();
    return this.state.projects().flatMap((p) => {
      const sessions = all[p.key] ?? [];
      if (!q || p.name.toLowerCase().includes(q) || p.path.toLowerCase().includes(q)) return [{ ...p, sessions, hits: false }];
      const hit = sessions.filter((s) => s.title.toLowerCase().includes(q));
      return hit.length ? [{ ...p, sessions: hit, hits: true }] : [];
    });
  });

  /** The open session's leaf; the selected project's folder while it is closed or no session of it is open. */
  private readonly treeSelection = computed(() => {
    const c = this.state.current();
    const k = this.state.projectKey();
    const shown = k != null && this.open().has(k) && (this.state.projectSessions()[k] ?? []).some((s) => c && s.sourceId === c.sourceId && s.id === c.id);
    if (c && shown) return [sv(c)];
    return k != null ? [pv(k)] : [];
  });
  readonly sel = linkedSignal(() => this.treeSelection());

  isOpen(key: string): boolean {
    return this.open().has(key) || (!!this.q() && !!this.filtered().find((p) => p.key === key)?.hits);
  }
  setOpen(key: string, open: boolean): void {
    if (this.open().has(key) === open) return;
    const s = new Set(this.open());
    if (open) s.add(key);
    else s.delete(key);
    this.open.set(s);
    if (open && !this.state.projectSessions()[key]) void this.state.loadProjectSessions(key);
  }

  private find(v: string): { project: string; row?: core.SessionRow } | null {
    if (v.startsWith('p:')) return { project: v.slice(2) };
    for (const [project, rows] of Object.entries(this.state.projectSessions())) {
      const row = rows.find((s) => sv(s) === v);
      if (row) return { project, row };
    }
    return null;
  }

  /** A folder selects the project (which opens it and its latest session); a leaf opens that session. */
  async select(values: readonly string[]): Promise<void> {
    const hit = values[0] ? this.find(values[0]) : null;
    if (hit?.row) {
      const c = this.state.current();
      if (!c || sv(c) !== values[0] || this.state.subagentId()) await this.state.openFrom(hit.project, hit.row.sourceId, hit.row.id, null, false);
      if (!['history', 'analysis', 'costs'].includes(this.state.view())) this.state.go('history');
    } else if (hit && hit.project !== this.state.projectKey()) {
      // Not when the tree moved the selection up because the user closed the folder.
      await this.state.selectProject(hit.project);
    }
    // The tree moved its own selection; put it back on what the app shows.
    this.sel.set(this.treeSelection());
  }
  /** Enter or double-click on a session: open its analysis. */
  async activate(v: string): Promise<void> {
    const hit = this.find(v);
    if (!hit?.row) return;
    await this.state.openFrom(hit.project, hit.row.sourceId, hit.row.id, null, false);
    this.state.go('analysis');
  }

  menuFor(s: core.SessionRow): SnMenuItem[] {
    const open = (then: () => void) => () => void this.state.openFrom(s.projectKey, s.sourceId, s.id, null, false).then(then);
    return [
      { id: 'open', label: 'Open', action: open(() => this.state.go('history')) },
      { id: 'analyze', label: 'Session analysis', action: open(() => this.state.go('analysis')) },
      { id: 'costs', label: 'Cost explained', action: open(() => this.state.go('costs')) },
      { id: 'compare', label: this.state.compareA() && !this.state.compareB() ? 'Compare with “' + clip(this.state.compareA()!.title) + '”' : 'Compare with…', action: () => this.state.compareWith({ sourceId: s.sourceId, id: s.id, title: s.title }) },
      { type: 'separator' },
      { id: 'export-md', label: 'Export as Markdown…', action: () => void this.state.exported(ExportService.SessionMarkdown(s.sourceId, s.id, true), 'Markdown') },
      { id: 'export-json', label: 'Export as JSON…', action: () => void this.state.exported(ExportService.SessionJSON(s.sourceId, s.id), 'JSON') },
      { id: 'copy-md', label: 'Copy as Markdown', action: () => void this.copyMarkdown(s) },
      { id: 'copy-id', label: 'Copy session ID', action: () => void navigator.clipboard.writeText(s.id) },
    ];
  }

  projectMenu(p: core.Project): SnMenuItem[] {
    const select = (v: 'project' | 'overview' | 'trends') => () => void this.state.selectProject(p.key).then(() => this.state.go(v));
    return [
      { id: 'dashboard', label: 'Project dashboard', action: select('project') },
      { id: 'overview', label: 'Overview', action: select('overview') },
      { id: 'trends', label: 'Insight trends', action: select('trends') },
      { type: 'separator' },
      { id: 'export', label: 'Export sessions as CSV…', action: () => void this.state.exported(ExportService.SessionsCSV(p.key, this.state.sourceId(), this.state.agent(), 0, this.state.filter()), 'Sessions CSV') },
    ];
  }

  private async copyMarkdown(s: core.SessionRow): Promise<void> {
    try {
      await navigator.clipboard.writeText(await ExportService.SessionMarkdownText(s.sourceId, s.id, false));
      this.state.toast('Transcript copied', 'Prompts and answers as Markdown', 'ok');
    } catch (e) {
      this.state.toast('Could not copy the transcript', errText(e), 'error');
    }
  }

  sessionTip(s: core.SessionRow): string {
    const bits = [AGENTS[s.agent] ?? s.agent, fmtWhen(s.endedAt || s.startedAt), `${s.turnCount} turn${s.turnCount === 1 ? '' : 's'}`];
    if (s.subagentCount) bits.push(`${s.subagentCount} agents`);
    if (s.findingsHigh) bits.push(`${s.findingsHigh} high findings`);
    return `${s.title}\n${bits.join(' · ')}`;
  }
  projectTip(p: { path: string; sessionCount: number; agents: string[] | null }): string {
    const agents = (p.agents ?? []).map((a) => AGENTS[a] ?? a).join(', ');
    return `${p.path}\n${p.sessionCount} session${p.sessionCount === 1 ? '' : 's'}${agents ? ' · ' + agents : ''}`;
  }
}
