import { ChangeDetectionStrategy, Component, computed, effect, inject, signal, untracked } from '@angular/core';
import { SnCommandBarImports, SnStatusPage } from '@spartan-native/desktop-ui';
import { HlmBadgeImports } from '@spartan-ng/helm/badge';
import { HlmCardImports } from '@spartan-ng/helm/card';
import { HlmTableImports } from '@spartan-ng/helm/table';
import { BarDatum, StackedBars } from '../charts/stacked-bars';
import { LineChart, LinePoint } from '../charts/line-chart';
import { InsightService, core } from '../lib/api';
import { errText, fmtDay, fmtUSD } from '../lib/format';
import { AppState } from '../lib/state';
import { Kpi, Segmented, TONE } from './shared';

const TREND: Record<string, { label: string; tone: string; text: string }> = {
  better: { label: 'Improving', tone: TONE['ok'], text: 'Fires in fewer sessions than in the 4 weeks before.' },
  worse: { label: 'Worse', tone: TONE['high'], text: 'Fires in more sessions than in the 4 weeks before.' },
  steady: { label: 'Steady', tone: '', text: 'About as often as in the 4 weeks before.' },
  new: { label: 'New', tone: TONE['medium'], text: 'Did not fire in the 4 weeks before.' },
  none: { label: '—', tone: '', text: 'Not enough sessions to compare.' },
};

/** Week by week: how often each insight rule fires, so you can see whether acting on them helps. */
@Component({
  selector: 'cz-trends',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [SnCommandBarImports, SnStatusPage, HlmBadgeImports, HlmCardImports, HlmTableImports, StackedBars, LineChart, Kpi, Segmented],
  host: { class: 'flex min-h-0 flex-col' },
  template: `
    <sn-command-bar [title]="'Insight trends · ' + (t()?.title ?? '')" titlePosition="start" collapse="none" class="border-b">
      <span snCommandBarEnd class="flex items-center gap-2 pe-2">
        <cz-segmented label="Scope" [options]="scopes" [(value)]="scope" />
        <cz-segmented label="Weeks" [options]="ranges" [(value)]="weeks" />
      </span>
    </sn-command-bar>
    <div class="grid min-h-0 flex-1 auto-rows-max content-start gap-4 overflow-auto px-6 pt-4 pb-10 scroll-thin">
    @if (err()) {
      <sn-status-page icon="lucideTriangleAlert" title="Trends failed" [description]="err()!" />
    } @else if (t(); as t) {
      @if (!sessions()) {
        <sn-status-page icon="lucideTrendingDown" title="No sessions in these weeks" description="Pick more weeks, a wider scope, or clear the title-bar filters." />
      } @else {
        <div class="grid grid-cols-4 gap-2.5">
          <cz-kpi label="Sessions" [value]="'' + sessions()" [hint]="weeks() + ' weeks'" />
          <cz-kpi label="With a finding, last 4 weeks" [value]="pct(flagged(true))" [hint]="'before: ' + pct(flagged(false))" />
          <cz-kpi label="Estimated impact, last 4 weeks" [value]="fmtUSD(impact(true))" [hint]="'4 weeks before: ' + fmtUSD(impact(false))" />
          <cz-kpi label="Rules improving / worse" [value]="count('better') + ' / ' + count('worse')" [hint]="t.rules?.length + ' rules fired'" />
        </div>
        <div class="grid grid-cols-2 gap-4">
          <section hlmCard data-size="sm" class="content-start gap-2.5 px-4">
            <h3 class="m-0 text-[13.5px] font-semibold">Sessions with a high or medium finding</h3>
            <cz-line-chart [data]="flaggedLine()" seriesName="Share of sessions" [yFormat]="pctAxis" [valueFormat]="pctAxis" [height]="160" ariaLabel="Share of sessions with findings per week" />
          </section>
          <section hlmCard data-size="sm" class="content-start gap-2.5 px-4">
            <h3 class="m-0 text-[13.5px] font-semibold">Estimated impact per week</h3>
            <cz-stacked-bars [data]="impactBars()" [series]="impactSeries" [height]="160" [yFormat]="usd" [valueFormat]="usdFmt" ariaLabel="Estimated impact of findings per week" />
          </section>
        </div>
        <section hlmCard data-size="sm" class="content-start gap-2.5 px-4">
          <h3 class="m-0 text-[13.5px] font-semibold">Rules</h3>
          <p class="m-0 text-xs text-muted-foreground">Share of each week's sessions where the rule fired. The trend compares the last 4 weeks with the 4 before. Change a threshold in Settings and it re-runs on all history.</p>
          <div hlmTableContainer class="rounded-lg border">
            <table hlmTable class="text-xs">
              <thead hlmTHead><tr hlmTr><th hlmTh>Rule</th><th hlmTh>Per week</th><th hlmTh class="text-right">Last 4 wk</th><th hlmTh class="text-right">4 wk before</th><th hlmTh class="text-right">Sessions</th><th hlmTh class="text-right">Impact</th><th hlmTh>Trend</th></tr></thead>
              <tbody hlmTBody>
                @for (r of t.rules ?? []; track r.rule) {
                  <tr hlmTr>
                    <td hlmTd class="font-medium">{{ r.title }}</td>
                    <td hlmTd>
                      <svg [attr.viewBox]="'0 0 ' + (r.shares?.length ?? 0) * 8 + ' 24'" class="h-6" [style.width.px]="(r.shares?.length ?? 0) * 8" role="img" [attr.aria-label]="r.title + ' per week'">
                        @for (v of r.shares ?? []; track $index) {
                          <rect [attr.x]="$index * 8 + 1" [attr.y]="24 - bar(v)" width="6" [attr.height]="bar(v)" rx="1"
                            [attr.fill]="t.sessions![$index] ? 'var(--s1)' : 'transparent'"><title>{{ weekTitle($index) }}: {{ pct(v) }} ({{ r.counts![$index] }} of {{ t.sessions![$index] }})</title></rect>
                        }
                        <line x1="0" y1="23.5" [attr.x2]="(r.shares?.length ?? 0) * 8" y2="23.5" stroke="var(--border)" />
                      </svg>
                    </td>
                    <td hlmTd class="text-right tabular">{{ pct(r.recent) }}</td>
                    <td hlmTd class="text-right tabular text-muted-foreground">{{ pct(r.before) }}</td>
                    <td hlmTd class="text-right tabular">{{ r.total }}</td>
                    <td hlmTd class="text-right tabular">{{ fmtUSD(r.impactUSD) }}</td>
                    <td hlmTd><span hlmBadge variant="outline" [class]="TREND[r.trend].tone" [title]="TREND[r.trend].text">{{ TREND[r.trend].label }}</span></td>
                  </tr>
                } @empty { <tr hlmTr><td hlmTd colspan="7" class="text-muted-foreground">No rule fired in these weeks.</td></tr> }
              </tbody>
            </table>
          </div>
        </section>
      }
    }
    </div>
  `,
})
export class Trends {
  protected readonly state = inject(AppState);
  protected readonly fmtUSD = fmtUSD;
  protected readonly TREND = TREND;
  protected readonly usd = (v: number) => '$' + (v >= 10 ? Math.round(v) : +v.toFixed(2));
  protected readonly usdFmt = fmtUSD;
  protected readonly pctAxis = (v: number) => Math.round(v * 100) + '%';
  protected readonly impactSeries = [{ key: 'impact', name: 'Estimated impact', color: 'var(--warn)' }];
  protected readonly ranges = [
    { id: 8, label: '8 weeks' },
    { id: 12, label: '12 weeks' },
    { id: 26, label: '26 weeks' },
  ];
  protected readonly scopes = [
    { id: 'project' as const, label: 'This project' },
    { id: 'source' as const, label: 'Whole source' },
  ];
  readonly weeks = signal(12);
  readonly scope = signal<'project' | 'source'>('source');
  readonly t = signal<core.TrendReport | null>(null);
  readonly err = signal<string | null>(null);

  constructor() {
    effect(() => {
      const key = this.scope() === 'project' ? (this.state.projectKey() ?? '') : '';
      const args = [this.state.sourceId(), this.state.agent(), this.weeks(), this.state.filter()] as const;
      this.state.scan()?.last;
      untracked(() => {
        this.err.set(null);
        InsightService.Trends(key, ...args)
          .then((t) => this.t.set(t))
          .catch((e) => this.err.set(errText(e)));
      });
    });
  }

  readonly sessions = computed(() => (this.t()?.sessions ?? []).reduce((a, b) => a + b, 0));
  readonly flaggedLine = computed<LinePoint[]>(() => {
    const t = this.t();
    return (t?.weeks ?? []).map((w, i) => ({ label: fmtDay(w), title: 'Week of ' + fmtDay(w), value: t!.flagged![i] }));
  });
  readonly impactBars = computed<BarDatum[]>(() => {
    const t = this.t();
    return (t?.weeks ?? []).map((w, i) => ({ label: fmtDay(w), title: 'Week of ' + fmtDay(w), values: { impact: t!.impact![i] } }));
  });

  /** Weighted share of flagged sessions over the last 4 weeks (recent) or the 4 before. */
  flagged(recent: boolean): number {
    const t = this.t();
    if (!t) return 0;
    const n = t.weeks?.length ?? 0;
    const [from, to] = recent ? [n - 4, n] : [n - 8, n - 4];
    let s = 0;
    let f = 0;
    for (let i = Math.max(0, from); i < to; i++) {
      s += t.sessions![i];
      f += t.flagged![i] * t.sessions![i];
    }
    return s ? f / s : 0;
  }
  impact(recent: boolean): number {
    const t = this.t();
    if (!t) return 0;
    const n = t.weeks?.length ?? 0;
    const [from, to] = recent ? [n - 4, n] : [n - 8, n - 4];
    return (t.impact ?? []).slice(Math.max(0, from), to).reduce((a, b) => a + b, 0);
  }
  count(trend: string): number {
    return (this.t()?.rules ?? []).filter((r) => r.trend === trend).length;
  }
  pct(v: number): string {
    return Math.round(v * 100) + '%';
  }
  bar(v: number): number {
    return v > 0 ? Math.max(2, v * 22) : 0;
  }
  weekTitle(i: number): string {
    const w = this.t()?.weeks?.[i];
    return w ? 'Week of ' + fmtDay(w) : '';
  }
}
