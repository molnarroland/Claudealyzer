import { ChangeDetectionStrategy, Component, computed, effect, inject, signal, untracked } from '@angular/core';
import { SnCommandBarImports, SnStatusPage } from '@spartan-native/desktop-ui';
import { SnSelectableImports } from '@spartan-native/helm';
import { HlmBadgeImports } from '@spartan-ng/helm/badge';
import { HlmButtonImports } from '@spartan-ng/helm/button';
import { HlmCardImports } from '@spartan-ng/helm/card';
import { HlmTableImports } from '@spartan-ng/helm/table';
import { BarDatum, StackedBars } from '../charts/stacked-bars';
import { LineChart, LinePoint } from '../charts/line-chart';
import { InsightService, UsageService, core } from '../lib/api';
import { AGENTS, AGENT_COLOR, TOKEN_TYPES, errText, fmtD, fmtDay, fmtT, fmtUSD, fmtWhen, shortModel, total } from '../lib/format';
import { AppState } from '../lib/state';
import { AgentTag, FindingsList, Kpi, Segmented, TONE } from './shared';

@Component({
  selector: 'cz-analysis',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [SnCommandBarImports, SnStatusPage, HlmButtonImports, HlmBadgeImports, HlmCardImports, HlmTableImports, StackedBars, LineChart, Kpi, AgentTag, FindingsList],
  host: { class: 'flex min-h-0 flex-col' },
  template: `
    <sn-command-bar [title]="a()?.title ?? 'Session analysis'" titlePosition="start" class="border-b">
      @if (a(); as a) {
        <span snCommandBarEnd class="flex items-center gap-1.5 pe-2">
          <cz-agent [agent]="a.agent" /><span hlmBadge variant="secondary">{{ shortModel(a.model) }}</span>
          @if (a.isSubagent) { <span hlmBadge variant="outline">subagent</span> }
        </span>
      }
      <button snCommand label="Back to chat" icon="lucideArrowLeft" [priority]="2" (click)="state.go('history')"></button>
      <button snCommand label="Cost explained" icon="lucideCircleDollarSign" [priority]="1" [disabled]="!state.current()" (click)="state.go('costs')"></button>
    </sn-command-bar>
    <div class="grid min-h-0 flex-1 auto-rows-max content-start gap-4 overflow-auto px-6 pt-4 pb-10 scroll-thin">
    @if (a(); as a) {
      <div class="grid grid-cols-6 gap-2.5">
        <cz-kpi label="List price" [value]="fmtUSD(a.usage.costUSD)" hint="API-equivalent" />
        <cz-kpi label="Total tokens" [value]="fmtT(total(a.usage))" [hint]="cacheShare(a) + ' cache read'" />
        <cz-kpi label="Wall time" [value]="fmtD(a.wallMs) || '—'" [hint]="a.turnCount + (a.turnCount === 1 ? ' turn' : ' turns')" />
        <cz-kpi label="Tool time" [value]="a.wallMs ? ((a.toolMs / a.wallMs) * 100).toFixed(0) + '%' : '—'" [hint]="fmtD(a.toolMs)" />
        <cz-kpi label="Peak context" [value]="fmtT(a.peakContext)" [hint]="'turn ' + (a.peakTurn + 1)" />
        <cz-kpi label="Subagent share" [value]="a.isSubagent ? '—' : (a.subagentShare * 100).toFixed(0) + '%'" [hint]="a.isSubagent ? 'this is a subagent' : fmtUSD(a.subagentUSD)" />
      </div>
      <div class="grid grid-cols-[minmax(0,3fr)_minmax(0,2fr)] gap-4">
        <section hlmCard data-size="sm" class="content-start gap-2.5 px-4">
          <div class="flex items-center justify-between"><h3 class="m-0 text-[13.5px] font-semibold">Cost per turn</h3><span class="text-xs text-muted-foreground">click a bar to open that turn</span></div>
          <div class="flex flex-wrap gap-3.5 text-xs text-muted-foreground">
            @for (s of series(); track s.key) {
              <span class="inline-flex items-center gap-1.5"><i class="size-2.5 rounded-[3px]" [style.background]="s.color"></i>{{ s.name }} <b class="font-medium text-foreground tabular">{{ fmtUSD(seriesTotal(a, s.key)) }}</b></span>
            }
          </div>
          <cz-stacked-bars [data]="bars()" [series]="series()" [yFormat]="usd" [valueFormat]="usdFmt" [clickable]="true" (pick)="state.goTurn($event, state.subagentId())" ariaLabel="Cost per turn by token type" />
          <h3 class="m-0 mt-1.5 text-[13.5px] font-semibold">Peak context per turn</h3>
          <cz-line-chart [data]="peaks()" [ref]="a.contextLimit" refLabel="threshold" seriesName="Peak context" [yFormat]="tok" [valueFormat]="tok" [clickable]="true" (pick)="state.goTurn($event, state.subagentId())" ariaLabel="Peak context per turn" />
        </section>
        <section hlmCard data-size="sm" class="content-start gap-2.5 px-4">
          <h3 class="m-0 text-[13.5px] font-semibold">Cost drivers</h3>
          <p class="m-0 text-xs text-muted-foreground">Ranked by severity and estimated impact. Each one links to its evidence.</p>
          <cz-findings [findings]="drivers()" />
        </section>
      </div>
      <div class="grid grid-cols-2 gap-4">
        <section hlmCard data-size="sm" class="content-start gap-2.5 px-4">
          <h3 class="m-0 text-[13.5px] font-semibold">Where the time went</h3>
          <p class="m-0 text-xs text-muted-foreground">Wall time per category. Subagents run in parallel, so categories can add up to more than the session.</p>
          <div class="grid gap-2">
            @for (b of a.time ?? []; track b.name) {
              <div class="grid grid-cols-[120px_minmax(0,1fr)_72px] items-center gap-2.5 text-[12.5px]">
                <span>{{ b.name }}</span>
                <div class="h-3 overflow-hidden rounded-[3px] bg-muted"><div class="h-full rounded-r-[4px] bg-[var(--s1)]" [style.width.%]="(b.ms / timeMax(a)) * 100"></div></div>
                <span class="text-right tabular text-muted-foreground">{{ fmtD(b.ms) }}</span>
              </div>
            } @empty { <p class="text-sm text-muted-foreground">No timing data.</p> }
          </div>
        </section>
        <section hlmCard data-size="sm" class="content-start gap-2.5 px-4">
          <h3 class="m-0 text-[13.5px] font-semibold">Slowest commands</h3>
          <div hlmTableContainer class="rounded-lg border">
            <table hlmTable class="text-xs">
              <thead hlmTHead><tr hlmTr><th hlmTh>Command</th><th hlmTh class="text-right">Runs</th><th hlmTh class="text-right">Total</th><th hlmTh class="text-right">Longest</th><th hlmTh></th></tr></thead>
              <tbody hlmTBody>
                @for (c of a.commands ?? []; track c.command) {
                  <tr hlmTr>
                    <td hlmTd class="mono max-w-72 truncate" [title]="c.command">{{ c.command }}</td>
                    <td hlmTd class="text-right tabular">@if (c.runs > 1) { <span hlmBadge [class]="TONE['medium']">{{ c.runs }}×</span> } @else { 1 }</td>
                    <td hlmTd class="text-right tabular">{{ fmtD(c.totalMs) }}</td>
                    <td hlmTd class="text-right tabular">{{ fmtD(c.maxMs) }}</td>
                    <td hlmTd><button hlmBtn variant="link" size="xs" class="h-auto p-0" (click)="openCmd(c)">turn {{ c.turn + 1 }}</button></td>
                  </tr>
                } @empty { <tr hlmTr><td hlmTd colspan="5" class="text-muted-foreground">No shell commands.</td></tr> }
              </tbody>
            </table>
          </div>
        </section>
      </div>
      <section hlmCard data-size="sm" class="content-start gap-2.5 px-4">
        <h3 class="m-0 text-[13.5px] font-semibold">Most-read files</h3>
        <div hlmTableContainer class="rounded-lg border">
          <table hlmTable class="text-xs">
            <thead hlmTHead><tr hlmTr><th hlmTh>File</th><th hlmTh class="text-right">Reads</th><th hlmTh class="text-right">Tokens into context</th><th hlmTh class="text-right">Re-sent cost (est.)</th></tr></thead>
            <tbody hlmTBody>
              @for (f of a.files ?? []; track f.path) {
                <tr hlmTr>
                  <td hlmTd class="mono selectable">{{ f.path }}</td>
                  <td hlmTd class="text-right tabular">@if (f.reads > 2) { <span hlmBadge [class]="TONE['medium']">{{ f.reads }}</span> } @else { {{ f.reads }} }</td>
                  <td hlmTd class="text-right tabular">{{ fmtT(f.tokens) }}</td>
                  <td hlmTd class="text-right tabular">{{ fmtUSD(f.resentUSD) }}</td>
                </tr>
              } @empty { <tr hlmTr><td hlmTd colspan="4" class="text-muted-foreground">No file reads.</td></tr> }
            </tbody>
          </table>
        </div>
      </section>
    } @else if (err()) {
      <sn-status-page icon="lucideTriangleAlert" title="Analysis failed" [description]="err()!" />
    } @else if (state.current()) {
      <p class="text-sm text-muted-foreground">Analyzing…</p>
    } @else {
      <sn-status-page icon="lucideChartColumn" title="No session open" description="Open a session in History to see what drove its cost and time.">
        <button hlmBtn variant="outline" (click)="state.go('history')">Go to History</button>
      </sn-status-page>
    }
    </div>
  `,
})
export class Analysis {
  protected readonly state = inject(AppState);
  protected readonly fmtD = fmtD;
  protected readonly fmtT = fmtT;
  protected readonly fmtUSD = fmtUSD;
  protected readonly shortModel = shortModel;
  protected readonly total = total;
  protected readonly usd = (v: number) => (v >= 10 ? '$' + Math.round(v) : '$' + (v % 1 ? v.toFixed(2) : v));
  protected readonly usdFmt = fmtUSD;
  protected readonly tok = fmtT;
  protected readonly TONE = TONE;
  readonly a = signal<core.Analysis | null>(null);
  readonly err = signal<string | null>(null);

  constructor() {
    effect(() => {
      const c = this.state.current();
      const sub = this.state.subagentId() ?? '';
      this.state.detail(); // reload after live updates
      untracked(() => {
        this.err.set(null);
        if (!c) return;
        InsightService.Analysis(c.sourceId, c.id, sub)
          .then((a) => this.a.set(a))
          .catch((e) => this.err.set(errText(e)));
      });
    });
  }

  readonly series = computed(() =>
    TOKEN_TYPES.filter((t) => t.key !== 'cacheWrite' || this.a()?.hasCacheWrite).map((t) => ({ key: t.key, name: t.name, color: t.color })),
  );
  readonly bars = computed<BarDatum[]>(() =>
    (this.a()?.turns ?? []).map((t) => ({
      label: `T${t.turn + 1}`,
      title: `Turn ${t.turn + 1}`,
      values: { input: t.input, cacheWrite: t.cacheWrite, cacheRead: t.cacheRead, output: t.output },
    })),
  );
  readonly peaks = computed<LinePoint[]>(() => (this.a()?.turns ?? []).map((t) => ({ label: `T${t.turn + 1}`, title: `Turn ${t.turn + 1}`, value: t.peak })));
  readonly drivers = computed(() => (this.a()?.findings ?? []).filter((f) => f.severity !== 'ok').slice(0, 6));

  seriesTotal(a: core.Analysis, key: string): number {
    return (a.turns ?? []).reduce((s, t) => s + ((t as unknown as Record<string, number>)[key] ?? 0), 0);
  }
  cacheShare(a: core.Analysis): string {
    const t = total(a.usage);
    return t ? Math.round((a.usage.cacheRead / t) * 100) + '%' : '—';
  }
  timeMax(a: core.Analysis): number {
    return Math.max(1, ...(a.time ?? []).map((b) => b.ms));
  }
  openCmd(c: core.CommandRow): void {
    this.state.goTurn(c.turn, this.state.subagentId());
    this.state.openStep(c.stepId, c.turn);
  }
}

@Component({
  selector: 'cz-overview',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [SnCommandBarImports, SnSelectableImports, HlmButtonImports, HlmBadgeImports, HlmCardImports, HlmTableImports, StackedBars, Kpi, AgentTag, Segmented],
  host: { class: 'flex min-h-0 flex-col' },
  template: `
    <sn-command-bar [title]="o()?.title ?? 'Overview'" titlePosition="start" collapse="none" class="border-b">
      <span snCommandBarEnd class="flex items-center gap-2 pe-2">
        <cz-segmented label="Scope" [options]="scopes" [(value)]="scope" />
        <cz-segmented label="Range" [options]="ranges" [(value)]="days" />
      </span>
    </sn-command-bar>
    <div class="grid min-h-0 flex-1 auto-rows-max content-start gap-4 overflow-auto px-6 pt-4 pb-10 scroll-thin">
    @if (o()?.path) { <span class="mono -mb-2 text-xs text-muted-foreground">{{ o()!.path }}</span> }
    @if (o(); as o) {
      <div class="grid grid-cols-4 gap-2.5">
        <cz-kpi [label]="'List price, ' + rangeLabel()" [value]="fmtUSD(o.totalUSD)" hint="API prices" />
        <cz-kpi label="Sessions" [value]="'' + o.sessions" [hint]="agentNames(o.agents)" />
        <cz-kpi label="Average per session" [value]="fmtUSD(o.avgUSD)" [hint]="'median ' + fmtUSD(o.medianUSD)" />
        <cz-kpi label="Sessions with high findings" [value]="'' + o.highSessions" [hint]="'of ' + o.sessions" />
      </div>
      <section hlmCard data-size="sm" class="content-start gap-2.5 px-4">
        <div class="flex items-center justify-between gap-3">
          <h3 class="m-0 text-[13.5px] font-semibold">Daily list price by agent</h3>
          <div class="flex flex-wrap gap-3.5 text-xs text-muted-foreground">
            @for (s of series(); track s.key) {
              <span class="inline-flex items-center gap-1.5"><i class="size-2.5 rounded-[3px]" [style.background]="s.color"></i>{{ s.name }} <b class="font-medium text-foreground tabular">{{ fmtUSD(agentTotal(s.key)) }}</b></span>
            }
          </div>
        </div>
        <cz-stacked-bars [data]="bars()" [series]="series()" [height]="200" [yFormat]="usd" [valueFormat]="usdFmt" ariaLabel="Daily list price by agent" />
      </section>
      <section hlmCard data-size="sm" class="content-start gap-2.5 px-4">
        <div class="flex items-center justify-between"><h3 class="m-0 text-[13.5px] font-semibold">Sessions</h3><span class="text-xs text-muted-foreground">Sorted by {{ sortLabel() }} · double-click or Enter opens a session</span></div>
        <div hlmTableContainer class="rounded-lg border">
          <table hlmTable snSelectable="grid" [multiple]="false" [(selection)]="picked" (activated)="openKey($event)" aria-label="Sessions" class="text-xs">
            <thead hlmTHead>
              <tr hlmTr>
                @for (h of headers; track h.id) {
                  <th hlmTh [class.text-right]="h.num" [attr.aria-sort]="sort() === h.sort ? 'descending' : null">
                    @if (h.sort) {
                      <button class="cursor-pointer font-medium" (click)="sort.set(h.sort)">{{ h.label }}{{ sort() === h.sort ? ' ↓' : '' }}</button>
                    } @else { {{ h.label }} }
                  </th>
                }
              </tr>
            </thead>
            <tbody hlmTBody>
              @for (r of rows(); track r.sourceId + r.id) {
                <tr hlmTr snSelectableItem [value]="r.sourceId + '/' + r.id" [snSelectableLabel]="r.title">
                  <td hlmTd class="max-w-80 truncate" [title]="r.title">{{ r.title }}</td>
                  <td hlmTd><cz-agent [agent]="r.agent" /></td>
                  <td hlmTd class="text-muted-foreground">{{ r.sourceName }}</td>
                  <td hlmTd class="whitespace-nowrap">{{ fmtWhen(r.startedAt) }}</td>
                  <td hlmTd class="text-right tabular">{{ r.turnCount }}</td>
                  <td hlmTd class="text-right tabular">{{ fmtT(total(r.usage)) }}</td>
                  <td hlmTd class="text-right tabular">{{ fmtT(r.peakContext) }}</td>
                  <td hlmTd class="text-right tabular"><b>{{ r.noUsageData ? '—' : fmtUSD(r.usage.costUSD) }}</b></td>
                  <td hlmTd>
                    @if (r.findingsHigh) { <span hlmBadge [class]="TONE['high']">{{ r.findingsHigh }} high</span> }
                    @if (r.findingsMed) { <span hlmBadge class="ml-1" [class]="TONE['medium']">{{ r.findingsMed }} medium</span> }
                    @if (!r.findingsHigh && !r.findingsMed) { <span hlmBadge [class]="TONE['ok']">OK</span> }
                  </td>
                </tr>
              } @empty { <tr hlmTr><td hlmTd colspan="9" class="text-muted-foreground">No sessions in this range.</td></tr> }
            </tbody>
          </table>
        </div>
      </section>
    }
    </div>
  `,
})
export class Overview {
  protected readonly state = inject(AppState);
  protected readonly fmtUSD = fmtUSD;
  protected readonly fmtT = fmtT;
  protected readonly fmtWhen = fmtWhen;
  protected readonly total = total;
  protected readonly usd = (v: number) => '$' + (v >= 10 ? Math.round(v) : +v.toFixed(2));
  protected readonly usdFmt = fmtUSD;
  protected readonly TONE = TONE;
  readonly picked = signal<readonly string[]>([]);
  readonly o = signal<core.Overview | null>(null);
  readonly days = signal(30);
  readonly scope = signal<'project' | 'source'>('project');
  readonly sort = signal<'cost' | 'recent' | 'turns' | 'peak'>('cost');
  protected readonly ranges = [
    { id: 7, label: '7 days' },
    { id: 30, label: '30 days' },
    { id: 0, label: 'All' },
  ];
  protected readonly scopes = [
    { id: 'project' as const, label: 'This project' },
    { id: 'source' as const, label: 'Whole source' },
  ];
  protected readonly headers = [
    { id: 'title', label: 'Session', sort: null, num: false },
    { id: 'agent', label: 'Agent', sort: null, num: false },
    { id: 'src', label: 'Source', sort: null, num: false },
    { id: 'when', label: 'When', sort: 'recent' as const, num: false },
    { id: 'turns', label: 'Turns', sort: 'turns' as const, num: true },
    { id: 'tok', label: 'Tokens', sort: null, num: true },
    { id: 'peak', label: 'Peak ctx', sort: 'peak' as const, num: true },
    { id: 'cost', label: 'List price', sort: 'cost' as const, num: true },
    { id: 'f', label: 'Findings', sort: null, num: false },
  ];

  constructor() {
    effect(() => {
      const key = this.scope() === 'project' ? (this.state.projectKey() ?? '') : '';
      const src = this.state.sourceId();
      const ag = this.state.agent();
      const d = this.days();
      this.state.scan(); // refresh after rescans
      untracked(() => UsageService.Overview(key, src, ag, d).then((o) => this.o.set(o)));
    });
  }

  readonly rangeLabel = computed(() => (this.days() ? `last ${this.days()} days` : 'all time'));
  readonly sortLabel = computed(() => ({ cost: 'list price', recent: 'date', turns: 'turns', peak: 'peak context' })[this.sort()]);
  readonly series = computed(() => (this.o()?.agents ?? []).map((a) => ({ key: a, name: AGENTS[a] ?? a, color: AGENT_COLOR[a] ?? 'var(--s4)' })));
  readonly bars = computed<BarDatum[]>(() => (this.o()?.days ?? []).map((d) => ({ label: fmtDay(d.date), title: fmtDay(d.date), values: (d.byAgent ?? {}) as Record<string, number> })));
  readonly rows = computed(() => {
    const rs = [...(this.o()?.rows ?? [])];
    const by = this.sort();
    rs.sort((a, b) =>
      by === 'recent' ? b.startedAt - a.startedAt : by === 'turns' ? b.turnCount - a.turnCount : by === 'peak' ? b.peakContext - a.peakContext : b.usage.costUSD - a.usage.costUSD,
    );
    return rs.slice(0, 300);
  });
  agentTotal(k: string): number {
    return (this.o()?.byAgent ?? {})[k] ?? 0;
  }
  agentNames(as: string[] | null): string {
    return (as ?? []).map((a) => AGENTS[a] ?? a).join(' · ');
  }
  openKey(k: string): void {
    const r = this.rows().find((x) => x.sourceId + '/' + x.id === k);
    if (r) void this.state.openFrom(r.projectKey, r.sourceId, r.id);
  }
}
