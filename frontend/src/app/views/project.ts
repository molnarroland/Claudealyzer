import { ChangeDetectionStrategy, Component, computed, effect, inject, signal, untracked } from '@angular/core';
import { SnCommandBarImports, SnStatusPage } from '@spartan-native/desktop-ui';
import { SnSelectableImports } from '@spartan-native/helm';
import { HlmBadgeImports } from '@spartan-ng/helm/badge';
import { HlmButtonImports } from '@spartan-ng/helm/button';
import { HlmCardImports } from '@spartan-ng/helm/card';
import { HlmTableImports } from '@spartan-ng/helm/table';
import { BarDatum, StackedBars } from '../charts/stacked-bars';
import { ExportService, UsageService, core } from '../lib/api';
import { AGENTS, AGENT_COLOR, errText, fmtDay, fmtT, fmtUSD, fmtWhen, shortModel } from '../lib/format';
import { AppState } from '../lib/state';
import { AgentTag, Kpi, Segmented, TONE } from './shared';

/** One project at a glance: cost over time, top sessions, most-edited files and recurring findings. */
@Component({
  selector: 'cz-project',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [SnCommandBarImports, SnStatusPage, SnSelectableImports, HlmButtonImports, HlmBadgeImports, HlmCardImports, HlmTableImports, StackedBars, Kpi, AgentTag, Segmented],
  host: { class: 'flex min-h-0 flex-col' },
  template: `
    <sn-command-bar [title]="r()?.name ?? 'Project dashboard'" titlePosition="start" collapse="none" class="border-b">
      <span snCommandBarEnd class="flex items-center gap-2 pe-2">
        <cz-segmented label="Range" [options]="ranges" [(value)]="days" />
      </span>
      <button snCommand label="Insight trends" icon="lucideTrendingDown" [priority]="1" [disabled]="!state.projectKey()" (click)="state.go('trends')"></button>
      <button snCommand label="Export CSV" icon="lucideDownload" [priority]="0" [disabled]="!state.projectKey()" (click)="exportCsv()"></button>
    </sn-command-bar>
    <div class="grid min-h-0 flex-1 auto-rows-max content-start gap-4 overflow-auto px-6 pt-4 pb-10 scroll-thin">
    @if (r(); as r) {
      <span class="mono -mb-2 flex items-center gap-2 text-xs text-muted-foreground">{{ r.path }}
        @for (a of r.agents ?? []; track a) { <cz-agent [agent]="a" /> }</span>
      @if (!r.sessions) {
        <sn-status-page icon="lucideSearchX" title="No sessions in this range" description="Pick a longer range, or clear the title-bar filters." />
      } @else {
        <div class="grid grid-cols-6 gap-2.5">
          <cz-kpi label="List price" [value]="fmtUSD(r.usage.costUSD)" [hint]="rangeLabel()" />
          <cz-kpi label="You paid" [value]="fmtUSD(r.effective)" [hint]="r.hasUnset ? 'some billing not set' : 'by your billing'" />
          <cz-kpi label="Sessions" [value]="'' + r.sessions" [hint]="r.prompts + ' prompts'" />
          <cz-kpi label="Average per session" [value]="fmtUSD(r.avgUSD)" [hint]="r.activeDays + ' active days'" />
          <cz-kpi label="Cache hit" [value]="(r.cacheHit * 100).toFixed(0) + '%'" [hint]="fmtT(r.usage.cacheRead) + ' read'" />
          <cz-kpi label="Sessions with high findings" [value]="'' + r.highSessions" [hint]="'of ' + r.sessions" />
        </div>
        <section hlmCard data-size="sm" class="content-start gap-2.5 px-4">
          <div class="flex items-center justify-between gap-3">
            <h3 class="m-0 text-[13.5px] font-semibold">Cost over time</h3>
            <span class="flex items-center gap-3">
              <span class="flex flex-wrap gap-3.5 text-xs text-muted-foreground">
                @for (s of series(); track s.key) { <span class="inline-flex items-center gap-1.5"><i class="size-2.5 rounded-[3px]" [style.background]="s.color"></i>{{ s.name }}</span> }
              </span>
              <cz-segmented label="Group by" [options]="grains" [(value)]="grain" />
            </span>
          </div>
          <cz-stacked-bars [data]="bars()" [series]="series()" [height]="190" [yFormat]="usd" [valueFormat]="usdFmt" ariaLabel="Project cost over time" />
          <p class="m-0 text-xs text-muted-foreground">First session {{ fmtWhen(r.firstActive) }} · last {{ fmtWhen(r.lastActive) }}</p>
        </section>
        <div class="grid grid-cols-[minmax(0,3fr)_minmax(0,2fr)] gap-4">
          <section hlmCard data-size="sm" class="content-start gap-2.5 px-4">
            <div class="flex items-center justify-between"><h3 class="m-0 text-[13.5px] font-semibold">Most expensive sessions</h3><span class="text-xs text-muted-foreground">double-click opens it</span></div>
            <div hlmTableContainer class="rounded-lg border">
              <table hlmTable snSelectable="grid" [multiple]="false" [(selection)]="picked" (activated)="openKey($event)" aria-label="Top sessions" class="text-xs">
                <thead hlmTHead><tr hlmTr><th hlmTh>Session</th><th hlmTh>When</th><th hlmTh class="text-right">Turns</th><th hlmTh class="text-right">List price</th><th hlmTh>Findings</th></tr></thead>
                <tbody hlmTBody>
                  @for (s of r.topSessions ?? []; track s.sourceId + s.id) {
                    <tr hlmTr snSelectableItem [value]="s.sourceId + '/' + s.id" [snSelectableLabel]="s.title">
                      <td hlmTd class="max-w-72 truncate" [title]="s.title">{{ s.title }}</td>
                      <td hlmTd class="whitespace-nowrap">{{ fmtWhen(s.startedAt) }}</td>
                      <td hlmTd class="text-right tabular">{{ s.turnCount }}</td>
                      <td hlmTd class="text-right tabular"><b>{{ fmtUSD(s.usage.costUSD) }}</b></td>
                      <td hlmTd>
                        @if (s.findingsHigh) { <span hlmBadge [class]="TONE['high']">{{ s.findingsHigh }} high</span> }
                        @if (s.findingsMed) { <span hlmBadge class="ml-1" [class]="TONE['medium']">{{ s.findingsMed }}</span> }
                      </td>
                    </tr>
                  }
                </tbody>
              </table>
            </div>
          </section>
          <section hlmCard data-size="sm" class="content-start gap-2.5 px-4">
            <h3 class="m-0 text-[13.5px] font-semibold">Recurring findings</h3>
            <p class="m-0 text-xs text-muted-foreground">How many sessions each rule fired in, with the estimated impact summed.</p>
            <div class="grid gap-1.5">
              @for (x of r.rules ?? []; track x.rule) {
                <div class="grid grid-cols-[minmax(0,1fr)_minmax(0,90px)_64px] items-center gap-2 text-[12.5px]">
                  <span class="truncate" [title]="x.title">{{ x.title }} @if (x.high) { <span hlmBadge [class]="TONE['high']">{{ x.high }} high</span> }</span>
                  <div class="h-2.5 overflow-hidden rounded-[3px] bg-muted" [title]="x.sessions + ' of ' + r.sessions + ' sessions'">
                    <div class="h-full rounded-r-[3px]" [style.width.%]="x.share * 100" [style.background]="x.high ? 'var(--crit)' : 'var(--warn)'"></div>
                  </div>
                  <span class="text-right tabular text-muted-foreground">{{ x.sessions }}× · {{ fmtUSD(x.impactUSD) }}</span>
                </div>
              } @empty { <p class="m-0 text-sm text-muted-foreground">No findings. Nice.</p> }
            </div>
          </section>
        </div>
        <div class="grid grid-cols-[minmax(0,3fr)_minmax(0,2fr)] gap-4">
          <section hlmCard data-size="sm" class="content-start gap-2.5 px-4">
            <h3 class="m-0 text-[13.5px] font-semibold">Most-edited files</h3>
            <div hlmTableContainer class="rounded-lg border">
              <table hlmTable class="text-xs">
                <thead hlmTHead><tr hlmTr><th hlmTh>File</th><th hlmTh class="text-right">Edits</th><th hlmTh class="text-right">Sessions</th></tr></thead>
                <tbody hlmTBody>
                  @for (f of r.files ?? []; track f.path) {
                    <tr hlmTr><td hlmTd class="mono selectable max-w-96 truncate" [title]="f.path">{{ f.path }}</td><td hlmTd class="text-right tabular">{{ f.edits }}</td><td hlmTd class="text-right tabular">{{ f.sessions }}</td></tr>
                  } @empty { <tr hlmTr><td hlmTd colspan="3" class="text-muted-foreground">No edits recorded.</td></tr> }
                </tbody>
              </table>
            </div>
          </section>
          <section hlmCard data-size="sm" class="content-start gap-2.5 px-4">
            <h3 class="m-0 text-[13.5px] font-semibold">Models</h3>
            <div hlmTableContainer class="rounded-lg border">
              <table hlmTable class="text-xs">
                <thead hlmTHead><tr hlmTr><th hlmTh>Model</th><th hlmTh class="text-right">Tokens</th><th hlmTh class="text-right">List price</th></tr></thead>
                <tbody hlmTBody>
                  @for (x of r.models ?? []; track x.model) {
                    <tr hlmTr><td hlmTd class="mono">{{ shortModel(x.model) }}</td><td hlmTd class="text-right tabular">{{ fmtT(x.tokens) }}</td><td hlmTd class="text-right tabular">{{ fmtUSD(x.costUSD) }}</td></tr>
                  }
                </tbody>
              </table>
            </div>
            <p class="m-0 text-xs text-muted-foreground">Model totals cover whole sessions, also outside the range.</p>
          </section>
        </div>
      }
    } @else if (err()) {
      <sn-status-page icon="lucideTriangleAlert" title="Dashboard failed" [description]="err()!" />
    } @else if (!state.projectKey()) {
      <sn-status-page icon="lucideFolderKanban" title="No project selected" description="Pick a project in the sidebar." />
    }
    </div>
  `,
})
export class ProjectDashboard {
  protected readonly state = inject(AppState);
  protected readonly fmtUSD = fmtUSD;
  protected readonly fmtT = fmtT;
  protected readonly fmtWhen = fmtWhen;
  protected readonly shortModel = shortModel;
  protected readonly usd = (v: number) => '$' + (v >= 10 ? Math.round(v) : +v.toFixed(2));
  protected readonly usdFmt = fmtUSD;
  protected readonly TONE = TONE;
  readonly r = signal<core.ProjectReport | null>(null);
  readonly err = signal<string | null>(null);
  readonly picked = signal<readonly string[]>([]);
  readonly days = signal(90);
  readonly grain = signal<'day' | 'week'>('day');
  protected readonly ranges = [
    { id: 30, label: '30 days' },
    { id: 90, label: '90 days' },
    { id: 0, label: 'All' },
  ];
  protected readonly grains = [
    { id: 'day' as const, label: 'Day' },
    { id: 'week' as const, label: 'Week' },
  ];

  constructor() {
    effect(() => {
      const key = this.state.projectKey();
      const args = [this.state.sourceId(), this.state.agent(), this.days(), this.state.filter()] as const;
      this.state.scan()?.last; // refresh after rescans
      untracked(() => {
        this.err.set(null);
        if (!key) return this.r.set(null);
        UsageService.Project(key, ...args)
          .then((r) => this.r.set(r))
          .catch((e) => this.err.set(errText(e)));
      });
    });
  }

  readonly rangeLabel = computed(() => (this.state.filter().from || this.state.filter().to ? 'filtered range' : this.days() ? `last ${this.days()} days` : 'all time'));
  readonly series = computed(() => (this.r()?.agents ?? []).map((a) => ({ key: a, name: AGENTS[a] ?? a, color: AGENT_COLOR[a] ?? 'var(--s4)' })));
  readonly bars = computed<BarDatum[]>(() => {
    const r = this.r();
    if (!r) return [];
    if (this.grain() === 'day') return (r.days ?? []).map((d) => ({ label: fmtDay(d.date), title: fmtDay(d.date), values: (d.byAgent ?? {}) as Record<string, number> }));
    // Weeks: sum the days per agent.
    const weeks = new Map<string, Record<string, number>>();
    for (const d of r.days ?? []) {
      const dt = new Date(d.date + 'T00:00:00');
      dt.setDate(dt.getDate() - ((dt.getDay() + 6) % 7));
      const k = `${dt.getFullYear()}-${String(dt.getMonth() + 1).padStart(2, '0')}-${String(dt.getDate()).padStart(2, '0')}`;
      const w = weeks.get(k) ?? {};
      for (const [a, v] of Object.entries(d.byAgent ?? {})) w[a] = (w[a] ?? 0) + (v ?? 0);
      weeks.set(k, w);
    }
    return [...weeks].map(([k, v]) => ({ label: fmtDay(k), title: 'Week of ' + fmtDay(k), values: v }));
  });

  openKey(k: string): void {
    const s = this.r()?.topSessions?.find((x) => x.sourceId + '/' + x.id === k);
    if (s) void this.state.openFrom(s.projectKey, s.sourceId, s.id);
  }

  exportCsv(): void {
    const key = this.state.projectKey();
    if (key) void this.state.exported(ExportService.SessionsCSV(key, this.state.sourceId(), this.state.agent(), this.days(), this.state.filter()), 'Sessions CSV');
  }
}
