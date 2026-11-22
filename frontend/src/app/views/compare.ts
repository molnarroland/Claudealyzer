import { ChangeDetectionStrategy, Component, computed, effect, inject, signal, untracked } from '@angular/core';
import { SnCommandBarImports, SnStatusPage } from '@spartan-native/desktop-ui';
import { HlmBadgeImports } from '@spartan-ng/helm/badge';
import { HlmButtonImports } from '@spartan-ng/helm/button';
import { HlmCardImports } from '@spartan-ng/helm/card';
import { HlmInputImports } from '@spartan-ng/helm/input';
import { HlmTableImports } from '@spartan-ng/helm/table';
import { UsageService, core } from '../lib/api';
import { errText, fmtD, fmtT, fmtUSD, fmtWhen, shortModel } from '../lib/format';
import { AppState, SessionRef, isoDay } from '../lib/state';
import { Segmented } from './shared';

type Better = 'lower' | 'higher' | 'none';
type Row = { label: string; a: number; b: number; fmt: (v: number) => string; better: Better; hint?: string };

const pctFmt = (v: number) => (v * 100).toFixed(0) + '%';
const num = (v: number) => Math.round(v).toLocaleString('en-US');

function shift(day: string, n: number): string {
  const d = new Date(day + 'T00:00:00');
  d.setDate(d.getDate() + n);
  return isoDay(d);
}

/** Period presets: B is the recent period, A the one before. */
function periodPreset(p: string): { aFrom: string; aTo: string; bFrom: string; bTo: string } {
  const now = new Date();
  const today = isoDay(now);
  switch (p) {
    case 'week': {
      const monday = shift(today, -((now.getDay() + 6) % 7));
      return { aFrom: shift(monday, -7), aTo: shift(monday, -1), bFrom: monday, bTo: today };
    }
    case 'month': {
      const first = isoDay(new Date(now.getFullYear(), now.getMonth(), 1));
      return { aFrom: isoDay(new Date(now.getFullYear(), now.getMonth() - 1, 1)), aTo: shift(first, -1), bFrom: first, bTo: today };
    }
    case '30d':
      return { aFrom: shift(today, -59), aTo: shift(today, -30), bFrom: shift(today, -29), bTo: today };
    default:
      return { aFrom: shift(today, -13), aTo: shift(today, -7), bFrom: shift(today, -6), bTo: today };
  }
}

/** Two sessions, or two periods, side by side with the change between them. */
@Component({
  selector: 'cz-compare',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [SnCommandBarImports, SnStatusPage, HlmButtonImports, HlmBadgeImports, HlmCardImports, HlmInputImports, HlmTableImports, Segmented],
  host: { class: 'flex min-h-0 flex-col' },
  template: `
    <sn-command-bar title="Compare" titlePosition="start" collapse="none" class="border-b">
      <span snCommandBarEnd class="flex items-center gap-2 pe-2">
        <cz-segmented label="Compare" [options]="modes" [value]="state.compareMode()" (valueChange)="state.compareMode.set($event)" />
      </span>
      @if (state.compareMode() === 'sessions') {
        <button snCommand label="Swap" icon="lucideRefreshCw" [priority]="1" [disabled]="!state.compareA() || !state.compareB()" (click)="swap()"></button>
      }
    </sn-command-bar>
    <div class="grid min-h-0 flex-1 auto-rows-max content-start gap-4 overflow-auto px-6 pt-4 pb-10 scroll-thin">
      @if (state.compareMode() === 'sessions') {
        <div class="grid grid-cols-2 gap-4">
          @for (side of sides; track side) {
            <label class="grid gap-1 text-xs text-muted-foreground">{{ side === 'a' ? 'Session A' : 'Session B' }}
              <select hlmInput class="h-8 text-[13px] text-foreground" [attr.aria-label]="side === 'a' ? 'Session A' : 'Session B'" (change)="pick(side, $any($event.target).value)">
                <option value="" [selected]="!ref(side)">Choose a session…</option>
                @if (ref(side) && !inList(ref(side)!)) { <option [value]="key(ref(side)!)" selected>{{ ref(side)!.title || ref(side)!.id }}</option> }
                @for (s of choices(); track s.sourceId + s.id) {
                  <option [value]="key(s)" [selected]="isRef(side, s)">{{ s.title }} · {{ fmtWhen(s.startedAt) }} · {{ fmtUSD(s.usage.costUSD) }}</option>
                }
              </select>
            </label>
          }
        </div>
        <p class="m-0 -mt-2 text-xs text-muted-foreground">The lists show the selected project's sessions. Right-click any session in the sidebar and choose “Compare with…” to pick from another project.</p>
      } @else {
        <div class="flex flex-wrap items-end gap-4">
          <label class="grid gap-1 text-xs text-muted-foreground">Preset
            <select hlmInput class="h-8 w-48 text-[13px] text-foreground" aria-label="Period preset" (change)="applyPreset($any($event.target).value)">
              @for (p of presets; track p.id) { <option [value]="p.id" [selected]="preset() === p.id">{{ p.label }}</option> }
            </select>
          </label>
          @for (side of sides; track side) {
            <fieldset class="m-0 flex items-end gap-1.5 border-0 p-0">
              <legend class="mb-1 text-xs text-muted-foreground">{{ side === 'a' ? 'Period A' : 'Period B' }}</legend>
              <input hlmInput type="date" class="h-8 w-36 text-[12.5px]" [attr.aria-label]="'Period ' + side + ' from'" [value]="range()[side === 'a' ? 'aFrom' : 'bFrom']" (change)="setRange(side === 'a' ? 'aFrom' : 'bFrom', $any($event.target).value)" />
              <span class="pb-1.5 text-muted-foreground">–</span>
              <input hlmInput type="date" class="h-8 w-36 text-[12.5px]" [attr.aria-label]="'Period ' + side + ' to'" [value]="range()[side === 'a' ? 'aTo' : 'bTo']" (change)="setRange(side === 'a' ? 'aTo' : 'bTo', $any($event.target).value)" />
            </fieldset>
          }
          <label class="grid gap-1 text-xs text-muted-foreground">Scope
            <cz-segmented label="Scope" [options]="scopes" [(value)]="scope" />
          </label>
        </div>
      }

      @if (err()) {
        <sn-status-page icon="lucideTriangleAlert" title="Comparison failed" [description]="err()!" />
      } @else if (cmp(); as c) {
        <section hlmCard data-size="sm" class="content-start gap-2.5 px-4">
          <div hlmTableContainer class="rounded-lg border">
            <table hlmTable class="text-[12.5px]">
              <thead hlmTHead>
                <tr hlmTr>
                  <th hlmTh class="w-48"></th>
                  <th hlmTh class="text-right"><span class="block max-w-72 truncate" [title]="c.a.label">A · {{ c.a.label }}</span><span class="block font-normal text-muted-foreground">{{ c.a.sub }}</span></th>
                  <th hlmTh class="text-right"><span class="block max-w-72 truncate" [title]="c.b.label">B · {{ c.b.label }}</span><span class="block font-normal text-muted-foreground">{{ c.b.sub }}</span></th>
                  <th hlmTh class="text-right">Change</th>
                </tr>
              </thead>
              <tbody hlmTBody>
                @for (r of rows(); track r.label) {
                  <tr hlmTr>
                    <td hlmTd class="text-muted-foreground" [title]="r.hint ?? ''">{{ r.label }}</td>
                    <td hlmTd class="text-right tabular">{{ r.fmt(r.a) }}</td>
                    <td hlmTd class="text-right tabular">{{ r.fmt(r.b) }}</td>
                    <td hlmTd class="text-right tabular"><span [class]="deltaClass(r)">{{ delta(r) }}</span></td>
                  </tr>
                }
              </tbody>
            </table>
          </div>
        </section>
        <div class="grid grid-cols-2 gap-4">
          <section hlmCard data-size="sm" class="content-start gap-2.5 px-4">
            <h3 class="m-0 text-[13.5px] font-semibold">Findings by rule</h3>
            <div hlmTableContainer class="rounded-lg border">
              <table hlmTable class="text-xs">
                <thead hlmTHead><tr hlmTr><th hlmTh>Rule</th><th hlmTh class="text-right">A</th><th hlmTh class="text-right">B</th><th hlmTh class="text-right">Impact A → B</th></tr></thead>
                <tbody hlmTBody>
                  @for (r of ruleRows(); track r.rule) {
                    <tr hlmTr>
                      <td hlmTd>{{ r.title }}</td>
                      <td hlmTd class="text-right tabular">{{ r.a }}</td>
                      <td hlmTd class="text-right tabular">{{ r.b }}</td>
                      <td hlmTd class="text-right tabular">{{ fmtUSD(r.ia) }} → <span [class]="r.ib > r.ia * 1.1 ? 'text-[var(--crit)]' : r.ib < r.ia * 0.9 ? 'text-[var(--ok)]' : ''">{{ fmtUSD(r.ib) }}</span></td>
                    </tr>
                  } @empty { <tr hlmTr><td hlmTd colspan="4" class="text-muted-foreground">No findings on either side.</td></tr> }
                </tbody>
              </table>
            </div>
            <p class="m-0 text-xs text-muted-foreground">{{ c.kind === 'periods' ? 'Sessions in each period where the rule fired.' : 'Findings in each session.' }}</p>
          </section>
          <section hlmCard data-size="sm" class="content-start gap-2.5 px-4">
            <h3 class="m-0 text-[13.5px] font-semibold">List price by model</h3>
            <div hlmTableContainer class="rounded-lg border">
              <table hlmTable class="text-xs">
                <thead hlmTHead><tr hlmTr><th hlmTh>Model</th><th hlmTh class="text-right">A</th><th hlmTh class="text-right">B</th></tr></thead>
                <tbody hlmTBody>
                  @for (r of modelRows(); track r.model) {
                    <tr hlmTr><td hlmTd class="mono">{{ shortModel(r.model) }}</td><td hlmTd class="text-right tabular">{{ fmtUSD(r.a) }}</td><td hlmTd class="text-right tabular">{{ fmtUSD(r.b) }}</td></tr>
                  }
                </tbody>
              </table>
            </div>
          </section>
        </div>
      } @else if (state.compareMode() === 'sessions') {
        <sn-status-page icon="lucideGitCompare" title="Pick two sessions" description="Choose session A and B above, or right-click sessions in the sidebar and choose “Compare with…”." />
      }
    </div>
  `,
})
export class Compare {
  protected readonly state = inject(AppState);
  protected readonly fmtUSD = fmtUSD;
  protected readonly fmtWhen = fmtWhen;
  protected readonly shortModel = shortModel;
  protected readonly sides = ['a', 'b'] as const;
  protected readonly modes = [
    { id: 'sessions' as const, label: 'Two sessions' },
    { id: 'periods' as const, label: 'Two periods' },
  ];
  protected readonly presets = [
    { id: 'week', label: 'This week vs last week' },
    { id: '7d', label: 'Last 7 days vs the 7 before' },
    { id: 'month', label: 'This month vs last month' },
    { id: '30d', label: 'Last 30 days vs the 30 before' },
    { id: 'custom', label: 'Custom' },
  ];
  protected readonly scopes = [
    { id: 'project' as const, label: 'This project' },
    { id: 'source' as const, label: 'Whole source' },
  ];
  readonly preset = signal('week');
  readonly range = signal(periodPreset('week'));
  readonly scope = signal<'project' | 'source'>('source');
  readonly cmp = signal<core.Comparison | null>(null);
  readonly err = signal<string | null>(null);
  private seq = 0;

  constructor() {
    effect(() => {
      const mode = this.state.compareMode();
      const a = this.state.compareA();
      const b = this.state.compareB();
      const r = this.range();
      const key = this.scope() === 'project' ? (this.state.projectKey() ?? '') : '';
      const args = [this.state.sourceId(), this.state.agent(), this.state.filter().model] as const;
      this.state.scan()?.last;
      untracked(() => {
        const n = ++this.seq;
        this.err.set(null);
        let p: Promise<core.Comparison | null>;
        if (mode === 'sessions') {
          if (!a || !b) return this.cmp.set(null);
          p = UsageService.CompareSessions(a.sourceId, a.id, b.sourceId, b.id);
        } else {
          p = UsageService.ComparePeriods(key, ...args, r.aFrom, r.aTo, r.bFrom, r.bTo);
        }
        p.then((c) => n === this.seq && this.cmp.set(c)).catch((e) => n === this.seq && (this.cmp.set(null), this.err.set(errText(e))));
      });
    });
  }

  /** Sessions to pick from: the selected project's, as in the sidebar. */
  readonly choices = computed(() => this.state.sessions());

  ref(side: 'a' | 'b'): SessionRef | null {
    return side === 'a' ? this.state.compareA() : this.state.compareB();
  }
  key(s: { sourceId: string; id: string }): string {
    return s.sourceId + '/' + s.id;
  }
  isRef(side: 'a' | 'b', s: core.SessionRow): boolean {
    const r = this.ref(side);
    return !!r && r.sourceId === s.sourceId && r.id === s.id;
  }
  inList(r: SessionRef): boolean {
    return this.choices().some((s) => s.sourceId === r.sourceId && s.id === r.id);
  }
  pick(side: 'a' | 'b', v: string): void {
    const s = this.choices().find((x) => this.key(x) === v);
    const ref = s ? { sourceId: s.sourceId, id: s.id, title: s.title } : null;
    (side === 'a' ? this.state.compareA : this.state.compareB).set(ref);
  }
  swap(): void {
    const a = this.state.compareA();
    this.state.compareA.set(this.state.compareB());
    this.state.compareB.set(a);
  }
  applyPreset(p: string): void {
    this.preset.set(p);
    if (p !== 'custom') this.range.set(periodPreset(p));
  }
  setRange(k: 'aFrom' | 'aTo' | 'bFrom' | 'bTo', v: string): void {
    this.preset.set('custom');
    this.range.set({ ...this.range(), [k]: v });
  }

  readonly rows = computed<Row[]>(() => {
    const c = this.cmp();
    if (!c) return [];
    const { a, b } = c;
    const rows: Row[] = [
      { label: 'List price', a: a.usage.costUSD, b: b.usage.costUSD, fmt: fmtUSD, better: 'lower' },
      { label: 'You paid', a: a.effective, b: b.effective, fmt: fmtUSD, better: 'lower' },
    ];
    if (c.kind === 'periods') {
      rows.push(
        { label: 'Sessions', a: a.sessions, b: b.sessions, fmt: num, better: 'none' },
        { label: 'Average per session', a: a.avgUSD, b: b.avgUSD, fmt: fmtUSD, better: 'lower' },
      );
    }
    rows.push(
      { label: 'Prompts', a: a.prompts, b: b.prompts, fmt: num, better: 'none' },
      { label: 'Cost per prompt', a: a.perPrompt, b: b.perPrompt, fmt: fmtUSD, better: 'lower' },
      { label: 'Turns', a: a.turns, b: b.turns, fmt: num, better: 'none' },
      { label: 'Total tokens', a: tokens(a.usage), b: tokens(b.usage), fmt: fmtT, better: 'lower' },
      { label: 'Output tokens', a: a.usage.output, b: b.usage.output, fmt: fmtT, better: 'none' },
      { label: 'Cache hit', a: a.cacheHit, b: b.cacheHit, fmt: pctFmt, better: 'higher', hint: 'Cache read ÷ everything sent in' },
      { label: c.kind === 'periods' ? 'Highest peak context' : 'Peak context', a: a.peakContext, b: b.peakContext, fmt: fmtT, better: 'lower' },
    );
    if (c.kind === 'periods') rows.push({ label: 'Average peak context', a: a.avgPeak, b: b.avgPeak, fmt: fmtT, better: 'lower' });
    rows.push(
      { label: 'Wall time', a: a.wallMs, b: b.wallMs, fmt: (v) => fmtD(v) || '—', better: 'none', hint: 'First to last activity, summed over sessions' },
      { label: 'Subagents', a: a.subagents, b: b.subagents, fmt: num, better: 'none' },
      { label: 'Subagent cost', a: a.subagentUSD, b: b.subagentUSD, fmt: fmtUSD, better: 'lower' },
      { label: 'High findings', a: a.findingsHigh, b: b.findingsHigh, fmt: num, better: 'lower' },
      { label: 'Medium findings', a: a.findingsMed, b: b.findingsMed, fmt: num, better: 'lower' },
    );
    return rows;
  });

  readonly ruleRows = computed(() => {
    const c = this.cmp();
    if (!c) return [];
    const m = new Map<string, { rule: string; title: string; a: number; b: number; ia: number; ib: number }>();
    const add = (rs: core.RuleRow[] | null, side: 'a' | 'b') => {
      for (const r of rs ?? []) {
        const x = m.get(r.rule) ?? { rule: r.rule, title: r.title, a: 0, b: 0, ia: 0, ib: 0 };
        const n = c.kind === 'periods' ? r.sessions : r.findings;
        if (side === 'a') (x.a = n), (x.ia = r.impactUSD);
        else (x.b = n), (x.ib = r.impactUSD);
        m.set(r.rule, x);
      }
    };
    add(c.a.rules, 'a');
    add(c.b.rules, 'b');
    return [...m.values()].sort((x, y) => y.ia + y.ib - (x.ia + x.ib));
  });

  readonly modelRows = computed(() => {
    const c = this.cmp();
    if (!c) return [];
    const keys = new Set([...Object.keys(c.a.byModel ?? {}), ...Object.keys(c.b.byModel ?? {})]);
    return [...keys]
      .map((model) => ({ model, a: c.a.byModel?.[model] ?? 0, b: c.b.byModel?.[model] ?? 0 }))
      .filter((r) => r.a > 0.0005 || r.b > 0.0005)
      .sort((x, y) => y.a + y.b - (x.a + x.b));
  });

  delta(r: Row): string {
    if (r.a === r.b) return '—';
    if (!r.a) return 'new';
    const d = (r.b / r.a - 1) * 100;
    return `${d > 0 ? '+' : ''}${Math.abs(d) >= 100 ? d.toFixed(0) : d.toFixed(1)}%`;
  }
  deltaClass(r: Row): string {
    if (r.better === 'none' || r.a === r.b || !r.a) return 'text-muted-foreground';
    const worse = r.better === 'lower' ? r.b > r.a : r.b < r.a;
    const big = Math.abs(r.b / r.a - 1) >= 0.05;
    if (!big) return 'text-muted-foreground';
    return worse ? 'text-[var(--crit)]' : 'text-[var(--ok)]';
  }
}

function tokens(u: { input: number; cacheWrite5m: number; cacheWrite1h: number; cacheRead: number; output: number }): number {
  return u.input + u.cacheWrite5m + u.cacheWrite1h + u.cacheRead + u.output;
}
