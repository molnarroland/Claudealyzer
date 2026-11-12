import { ChangeDetectionStrategy, Component, computed, effect, inject, signal, untracked } from '@angular/core';
import { SnCommandBarImports, SnStatusPage } from '@spartan-native/desktop-ui';
import { HlmAlertImports } from '@spartan-ng/helm/alert';
import { HlmButtonImports } from '@spartan-ng/helm/button';
import { HlmCardImports } from '@spartan-ng/helm/card';
import { HlmTableImports } from '@spartan-ng/helm/table';
import { BarDatum, StackedBars } from '../charts/stacked-bars';
import { UsageService, core, m } from '../lib/api';
import { errText, fmtD, fmtT, fmtUSD, shortModel, total } from '../lib/format';
import { AppState } from '../lib/state';
import { AgentTag, Kpi, Segmented, TokenBreakdown } from './shared';

type Metric = 'cost' | 'prompts' | 'tokens';
const WEEKDAYS = ['Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat', 'Sun'];

function iso(d: Date): string {
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`;
}
function parseDay(s: string): Date {
  return new Date(s + 'T00:00:00');
}
function addMonths(month: string, n: number): string {
  const [y, m] = month.split('-').map(Number);
  return iso(new Date(y, m - 1 + n, 1)).slice(0, 7);
}

/**
 * Month calendar of prompts, tokens and cost per day. Clicking a day opens it:
 * KPIs, cost by hour, token split, models, sessions and every prompt of that day.
 */
@Component({
  selector: 'cz-calendar',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [SnCommandBarImports, SnStatusPage, HlmAlertImports, HlmButtonImports, HlmCardImports, HlmTableImports, StackedBars, Kpi, AgentTag, TokenBreakdown, Segmented],
  host: { class: 'flex min-h-0 flex-col', '(document:keydown)': 'key($event)' },
  template: `
    <sn-command-bar [title]="state.calDay() ? dayLabel(state.calDay()!) : monthLabel(month())" titlePosition="start" class="border-b">
      @if (state.calDay(); as d) {
        <button snCommand [label]="monthLabel(d.slice(0, 7))" icon="lucideArrowLeft" [priority]="3" (click)="state.calDay.set(null)"></button>
        <sn-command-group label="Day">
          <button snCommand label="Previous day" icon="lucideChevronLeft" hideLabel [priority]="2" (click)="shiftDay(-1)"></button>
          <button snCommand label="Next day" icon="lucideChevronRight" hideLabel [priority]="2" [disabled]="d >= today" (click)="shiftDay(1)"></button>
        </sn-command-group>
      } @else {
        <sn-command-group label="Month">
          <button snCommand label="Previous month" icon="lucideChevronLeft" hideLabel [priority]="2" [disabled]="!!cal()?.first && month() <= cal()!.first" (click)="shiftMonth(-1)"></button>
          <button snCommand label="Next month" icon="lucideChevronRight" hideLabel [priority]="2" [disabled]="month() >= today.slice(0, 7)" (click)="shiftMonth(1)"></button>
        </sn-command-group>
        <button snCommand label="This month" icon="lucideCalendarDays" [priority]="1" [disabled]="month() === today.slice(0, 7)" (click)="state.calMonth.set('')"></button>
      }
      <span snCommandBarEnd class="flex items-center gap-2 pe-2">
        <cz-segmented label="Scope" [options]="scopes" [value]="state.calScope()" (valueChange)="state.calScope.set($event)" />
        @if (!state.calDay()) {
          <cz-segmented label="Shade days by" [options]="metrics" [(value)]="metric" />
        }
      </span>
    </sn-command-bar>
    <div class="grid min-h-0 flex-1 auto-rows-max content-start gap-4 overflow-auto px-6 pt-4 pb-10 scroll-thin">
    @if (cal()?.title) { <span class="-mb-2 text-xs text-muted-foreground">{{ cal()!.title }}</span> }

    @if (state.calDay()) {
      @if (day(); as d) {
        <div class="grid grid-cols-5 gap-2.5">
          <cz-kpi label="Prompts" [value]="'' + d.prompts" [hint]="(d.sessions?.length ?? 0) + ' session' + ((d.sessions?.length ?? 0) === 1 ? '' : 's')" />
          <cz-kpi label="Tokens" [value]="fmtT(total(d.usage))" [hint]="cacheHint(d.usage)" />
          <cz-kpi label="API calls" [value]="d.usage.calls.toLocaleString('en-US')" [hint]="d.prompts ? 'avg ' + (d.usage.calls / d.prompts).toFixed(1) + ' per prompt' : ''" />
          <cz-kpi label="List price" [value]="fmtUSD(d.usage.costUSD)" hint="API prices" />
          <cz-kpi label="What you paid" [value]="fmtUSD(d.effective)" [hint]="d.hasUnset ? 'billing not set for some sources' : 'by your billing settings'" />
        </div>
        <div class="grid grid-cols-[minmax(0,3fr)_minmax(0,2fr)] gap-4">
          <section hlmCard data-size="sm" class="content-start gap-2.5 px-4">
            <div class="flex items-center justify-between"><h3 class="m-0 text-[13.5px] font-semibold">List price by hour</h3>
              <span class="text-xs text-muted-foreground">{{ peakHour(d) }}</span></div>
            <cz-stacked-bars [data]="hours()" [series]="hourSeries" [height]="180" [yFormat]="usd" [valueFormat]="hourFmt" ariaLabel="List price by hour of day" />
          </section>
          <section hlmCard data-size="sm" class="content-start gap-3 px-4">
            <h3 class="m-0 text-[13.5px] font-semibold">Cost by token type</h3>
            <cz-token-breakdown [usage]="d.usage" />
            @if (models().length) {
              <h3 class="m-0 mt-1 text-[13.5px] font-semibold">By model</h3>
              <div class="grid gap-1 text-xs">
                @for (x of models(); track x.name) {
                  <div class="grid grid-cols-[minmax(0,1fr)_auto_auto] gap-2 tabular">
                    <span class="mono truncate" [title]="x.name">{{ shortModel(x.name) }}</span><span>{{ fmtUSD(x.cost) }}</span>
                    <span class="w-9 text-right text-muted-foreground">{{ ((x.cost / (d.usage.costUSD || 1)) * 100).toFixed(0) }}%</span>
                  </div>
                }
              </div>
            }
          </section>
        </div>

        <section hlmCard data-size="sm" class="content-start gap-2.5 px-4">
          <div class="flex items-center justify-between"><h3 class="m-0 text-[13.5px] font-semibold">Sessions active this day</h3><span class="text-xs text-muted-foreground">Numbers are for this day only · click to open</span></div>
          <div hlmTableContainer class="rounded-lg border">
            <table hlmTable class="text-xs">
              <thead hlmTHead><tr hlmTr>
                <th hlmTh>Session</th><th hlmTh>Project</th><th hlmTh>Agent</th>
                <th hlmTh class="text-right">Prompts</th><th hlmTh class="text-right">Tokens</th><th hlmTh class="text-right">List price</th><th hlmTh class="text-right">Paid</th>
              </tr></thead>
              <tbody hlmTBody>
                @for (s of d.sessions ?? []; track s.sourceId + s.id) {
                  <tr hlmTr class="cursor-pointer" (click)="state.openFrom(s.projectKey, s.sourceId, s.id)">
                    <td hlmTd class="max-w-80 truncate" [title]="s.title">{{ s.title }}</td>
                    <td hlmTd class="max-w-40 truncate text-muted-foreground" [title]="s.projectKey">{{ s.project }}</td>
                    <td hlmTd><cz-agent [agent]="s.agent" /></td>
                    <td hlmTd class="text-right tabular">{{ s.dayPrompts }}</td>
                    <td hlmTd class="text-right tabular">{{ fmtT(total(s.dayUsage)) }}</td>
                    <td hlmTd class="text-right tabular"><b>{{ s.noUsageData ? '—' : fmtUSD(s.dayUsage.costUSD) }}</b></td>
                    <td hlmTd class="text-right tabular">{{ fmtUSD(s.dayEffective) }}</td>
                  </tr>
                } @empty { <tr hlmTr><td hlmTd colspan="7" class="text-muted-foreground">No activity on this day.</td></tr> }
              </tbody>
            </table>
          </div>
        </section>

        <section hlmCard data-size="sm" class="content-start gap-2.5 px-4">
          <div class="flex items-center justify-between"><h3 class="m-0 text-[13.5px] font-semibold">Prompts</h3>
            <span class="text-xs text-muted-foreground">In time order · cost includes the turn's subagents · click to open the turn</span></div>
          @if (d.failed) { <p class="m-0 text-xs text-[var(--warn)]">{{ d.failed }} session{{ d.failed === 1 ? '' : 's' }} could not be read, so their prompts are missing.</p> }
          <div hlmTableContainer class="rounded-lg border">
            <table hlmTable class="text-xs">
              <thead hlmTHead><tr hlmTr>
                <th hlmTh>Time</th><th hlmTh>Prompt</th><th hlmTh>Session</th>
                <th hlmTh class="text-right">Steps</th><th hlmTh class="text-right">Took</th><th hlmTh class="text-right">Tokens</th><th hlmTh class="text-right">List price</th>
              </tr></thead>
              <tbody hlmTBody>
                @for (p of d.list ?? []; track p.sourceId + p.sessionId + p.turn) {
                  <tr hlmTr class="cursor-pointer" (click)="state.openFrom(p.projectKey, p.sourceId, p.sessionId, p.turn)">
                    <td hlmTd class="tabular text-muted-foreground">{{ time(p.at) }}</td>
                    <td hlmTd class="max-w-[28rem] truncate" [title]="p.prompt">{{ p.prompt }}</td>
                    <td hlmTd class="max-w-56 truncate text-muted-foreground" [title]="p.session"><cz-agent [agent]="p.agent" /> {{ p.session }}</td>
                    <td hlmTd class="text-right tabular">{{ p.steps }}</td>
                    <td hlmTd class="text-right tabular">{{ fmtD(p.durationMs) }}</td>
                    <td hlmTd class="text-right tabular">{{ fmtT(total(p.usage)) }}</td>
                    <td hlmTd class="text-right tabular"><b>{{ fmtUSD(p.usage.costUSD) }}</b></td>
                  </tr>
                } @empty { <tr hlmTr><td hlmTd colspan="7" class="text-muted-foreground">No prompts on this day.</td></tr> }
              </tbody>
            </table>
          </div>
        </section>
      } @else if (err()) {
        <sn-status-page icon="lucideTriangleAlert" title="This day could not be read" [description]="err()!" />
      } @else {
        <p class="text-sm text-muted-foreground">Reading the day's sessions…</p>
      }
    } @else if (cal(); as c) {
      <div class="grid grid-cols-5 gap-2.5">
        <cz-kpi label="Prompts" [value]="c.prompts.toLocaleString('en-US')" [hint]="c.activeDays ? 'avg ' + (c.prompts / c.activeDays).toFixed(0) + ' per active day' : ''" />
        <cz-kpi label="Tokens" [value]="fmtT(total(c.usage))" [hint]="cacheHint(c.usage)" />
        <cz-kpi label="List price" [value]="fmtUSD(c.usage.costUSD)" [hint]="c.activeDays ? 'avg ' + fmtUSD(c.usage.costUSD / c.activeDays) + ' per active day' : 'API prices'" />
        <cz-kpi label="What you paid" [value]="fmtUSD(c.effective)" [hint]="c.hasUnset ? 'billing not set for some sources' : 'by your billing settings'" />
        <cz-kpi label="Active days" [value]="'' + c.activeDays" [hint]="c.sessions + ' session' + (c.sessions === 1 ? '' : 's')" />
      </div>
      @if (c.hasUnset) {
        <div hlmAlert class="sn-tone-accent flex items-center justify-between gap-3">
          <p hlmAlertDescription class="m-0">Billing isn't set for some sources, so their days count at list price under “What you paid”.</p>
          <button hlmBtn size="sm" variant="outline" (click)="state.go('settings')">Set billing</button>
        </div>
      }
      <section class="grid gap-1.5" aria-label="Month calendar">
        <div class="grid grid-cols-7 gap-1.5">
          @for (w of weekdays; track w) { <span class="cz-label px-1">{{ w }}</span> }
        </div>
        <div class="grid grid-cols-7 gap-1.5" role="grid">
          @for (cell of cells(); track $index) {
            @if (cell; as x) {
              <button role="gridcell" class="relative grid min-h-24 content-start gap-0.5 rounded-lg border px-2 py-1.5 text-left text-[11.5px] tabular transition-shadow"
                [class.cursor-pointer]="!x.future" [class.hover:shadow-md]="!x.future" [class.opacity-45]="x.future"
                [class.!border-primary]="x.date === today" [class.border-2]="x.date === today" [disabled]="x.future"
                [style.background]="shade(x)" [attr.aria-label]="cellLabel(x)" [title]="cellLabel(x)" (click)="open(x.date)">
                <span class="flex items-center justify-between">
                  <b class="text-[13px] font-semibold" [class.text-muted-foreground]="!x.sessions">{{ +x.date.slice(8) }}</b>
                  @if (x.date === today) { <span class="text-[10px] font-medium uppercase tracking-wide text-muted-foreground">Today</span> }
                </span>
                @if (x.sessions) {
                  <span><b class="font-semibold">{{ x.prompts }}</b> <span class="text-muted-foreground">prompt{{ x.prompts === 1 ? '' : 's' }}</span></span>
                  <span><b class="font-semibold">{{ fmtT(total(x.usage)) }}</b> <span class="text-muted-foreground">tokens</span></span>
                  <span><b class="font-semibold">{{ fmtUSD(x.usage.costUSD) }}</b> <span class="text-muted-foreground">list</span></span>
                  @if (paidDiffers(x)) {
                    <span><b class="font-semibold">{{ fmtUSD(x.effective) }}</b> <span class="text-muted-foreground">paid</span></span>
                  }
                }
              </button>
            } @else {
              <span></span>
            }
          }
        </div>
        <div class="flex items-center justify-end gap-2 pt-1 text-[11px] text-muted-foreground">
          <span>Shaded by {{ metricLabel() }}: less</span>
          @for (t of legend; track t) { <i class="size-3 rounded-[3px] border" [style.background]="mix(t)"></i> }
          <span>more · click a day to open it</span>
        </div>
      </section>
    } @else {
      <p class="text-sm text-muted-foreground">{{ state.scan()?.running ? 'Reading history…' : 'Loading…' }}</p>
    }
    </div>
  `,
})
export class Calendar {
  protected readonly state = inject(AppState);
  protected readonly fmtUSD = fmtUSD;
  protected readonly fmtT = fmtT;
  protected readonly fmtD = fmtD;
  protected readonly total = total;
  protected readonly shortModel = shortModel;
  protected readonly weekdays = WEEKDAYS;
  protected readonly today = iso(new Date());
  protected readonly legend = [0.08, 0.3, 0.6, 1];
  protected readonly usd = (v: number) => '$' + (v >= 10 ? Math.round(v) : +v.toFixed(2));
  protected readonly hourFmt = fmtUSD;
  protected readonly hourSeries = [{ key: 'cost', name: 'List price', color: 'var(--s1)' }];
  protected readonly scopes = [
    { id: 'source' as const, label: 'Whole source' },
    { id: 'project' as const, label: 'This project' },
  ];
  protected readonly metrics = [
    { id: 'cost' as const, label: 'Cost' },
    { id: 'prompts' as const, label: 'Prompts' },
    { id: 'tokens' as const, label: 'Tokens' },
  ];

  readonly metric = signal<Metric>('cost');
  readonly cal = signal<core.Calendar | null>(null);
  readonly day = signal<core.DayDetail | null>(null);
  readonly err = signal<string | null>(null);
  /** Changes once per finished scan, not on every progress tick. */
  private readonly lastScan = computed(() => this.state.scan()?.last ?? 0);
  readonly month = computed(() => this.state.calMonth() || this.today.slice(0, 7));
  private readonly projectKey = computed(() => (this.state.calScope() === 'project' ? (this.state.projectKey() ?? '') : ''));
  private daySeq = 0;

  constructor() {
    effect(() => {
      const args = [this.projectKey(), this.state.sourceId(), this.state.agent(), this.month()] as const;
      this.state.sources(); // billing edits
      this.lastScan();
      untracked(() => UsageService.Calendar(...args).then((c) => this.cal.set(c)));
    });
    effect(() => {
      const d = this.state.calDay();
      const args = [this.projectKey(), this.state.sourceId(), this.state.agent()] as const;
      this.state.sources();
      this.lastScan();
      untracked(() => {
        if (!d) return;
        const n = ++this.daySeq;
        if (this.day()?.date !== d) this.day.set(null);
        this.err.set(null);
        UsageService.Day(...args, d)
          .then((r) => n === this.daySeq && this.day.set(r))
          .catch((e) => n === this.daySeq && this.err.set(errText(e)));
      });
    });
  }

  /** Leading blanks so the 1st lands on its weekday (Monday first). */
  readonly cells = computed(() => {
    const days = (this.cal()?.days ?? []).map((d) => ({ ...d, future: d.date > this.today }));
    if (!days.length) return [];
    const lead = (parseDay(days[0].date).getDay() + 6) % 7;
    return [...Array<null>(lead).fill(null), ...days];
  });

  readonly metricLabel = computed(() => ({ cost: 'list price', prompts: 'prompts', tokens: 'tokens' })[this.metric()]);

  readonly hours = computed<BarDatum[]>(() =>
    (this.day()?.hours ?? []).map((h) => ({ label: String(h.hour).padStart(2, '0'), title: `${String(h.hour).padStart(2, '0')}:00 · ${h.prompts} prompt${h.prompts === 1 ? '' : 's'}`, values: { cost: h.costUSD } })),
  );

  readonly models = computed(() =>
    Object.entries(this.day()?.byModel ?? {})
      .map(([name, cost]) => ({ name, cost: cost ?? 0 }))
      .filter((x) => x.cost > 0)
      .sort((a, b) => b.cost - a.cost),
  );

  private value(d: core.CalendarDay): number {
    const m = this.metric();
    return m === 'cost' ? d.usage.costUSD : m === 'prompts' ? d.prompts : total(d.usage);
  }

  /** Sequential single hue, light to dark; capped so cell text keeps its contrast. */
  mix(t: number): string {
    return `color-mix(in oklab, var(--s1) ${Math.round(6 + t * 40)}%, var(--sn-color-surface-solid))`;
  }

  shade(d: core.CalendarDay): string {
    const c = this.cal();
    if (!c || !d.sessions) return 'var(--sn-color-surface-solid)';
    const m = this.metric();
    const max = m === 'cost' ? c.maxCost : m === 'prompts' ? c.maxPrompts : c.maxTokens;
    return this.mix(max ? Math.sqrt(this.value(d) / max) : 0);
  }

  paidDiffers(d: core.CalendarDay): boolean {
    return Math.abs(d.effective - d.usage.costUSD) >= 0.005;
  }

  cellLabel(d: core.CalendarDay): string {
    const base = this.dayLabel(d.date);
    if (!d.sessions) return `${base}: no activity`;
    return `${base}: ${d.prompts} prompts, ${fmtT(total(d.usage))} tokens, ${fmtUSD(d.usage.costUSD)} list price, ${fmtUSD(d.effective)} paid, ${d.sessions} sessions`;
  }

  cacheHint(u: m.Usage): string {
    const t = total(u);
    return t ? `${Math.round((u.cacheRead / t) * 100)}% cache reads` : '';
  }

  peakHour(d: core.DayDetail): string {
    const hs = d.hours ?? [];
    if (!hs.some((h) => h.costUSD > 0)) return '';
    const p = hs.reduce((a, b) => (b.costUSD > a.costUSD ? b : a));
    return `Busiest hour ${String(p.hour).padStart(2, '0')}:00 · ${fmtUSD(p.costUSD)}`;
  }

  monthLabel(m: string): string {
    return parseDay(m + '-01').toLocaleDateString('en-GB', { month: 'long', year: 'numeric' });
  }
  dayLabel(d: string): string {
    return parseDay(d).toLocaleDateString('en-GB', { weekday: 'long', day: 'numeric', month: 'long', year: 'numeric' });
  }
  time(ms: number): string {
    return new Date(ms).toLocaleTimeString('en-GB', { hour: '2-digit', minute: '2-digit' });
  }

  open(date: string): void {
    if (date > this.today) return;
    this.state.calDay.set(date);
  }
  shiftMonth(n: number): void {
    const m = addMonths(this.month(), n);
    this.state.calMonth.set(m === this.today.slice(0, 7) ? '' : m);
  }
  shiftDay(n: number): void {
    const cur = this.state.calDay();
    if (!cur) return;
    const d = parseDay(cur);
    d.setDate(d.getDate() + n);
    const next = iso(d);
    if (next > this.today) return;
    this.state.calDay.set(next);
    const m = next.slice(0, 7);
    this.state.calMonth.set(m === this.today.slice(0, 7) ? '' : m);
  }

  key(e: KeyboardEvent): void {
    const t = e.target as HTMLElement | null;
    if (e.defaultPrevented || e.metaKey || e.ctrlKey || e.altKey) return;
    // Leave arrow keys to fields and to widgets with their own keyboard model (sidebar, toolbars, menus, lists).
    if (t && (t.tagName === 'INPUT' || t.tagName === 'TEXTAREA' || t.tagName === 'SELECT' || t.closest('[role=toolbar],[role=menubar],[role=menu],[role=listbox],[role=grid],[role=group],sn-sidebar-nav'))) return;
    if (e.key === 'Escape' && this.state.calDay()) this.state.calDay.set(null);
    else if (e.key === 'ArrowLeft') this.state.calDay() ? this.shiftDay(-1) : this.shiftMonth(-1);
    else if (e.key === 'ArrowRight') this.state.calDay() ? this.shiftDay(1) : this.month() < this.today.slice(0, 7) && this.shiftMonth(1);
  }
}

