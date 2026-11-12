import { ChangeDetectionStrategy, Component, computed, effect, inject, signal, untracked } from '@angular/core';
import { SnCommandBarImports, SnNumberBox, SnStatusPage } from '@spartan-native/desktop-ui';
import { HlmAlertImports } from '@spartan-ng/helm/alert';
import { HlmBadgeImports } from '@spartan-ng/helm/badge';
import { HlmButtonImports } from '@spartan-ng/helm/button';
import { HlmCardImports } from '@spartan-ng/helm/card';
import { HlmTableImports } from '@spartan-ng/helm/table';
import { LineChart, LinePoint } from '../charts/line-chart';
import { UsageService, core } from '../lib/api';
import { TOKEN_TYPES, errText, fmtDay, fmtT, fmtUSD } from '../lib/format';
import { AppState } from '../lib/state';
import { AgentTag, Kpi } from './shared';

const COLOR: Record<string, string> = Object.fromEntries(TOKEN_TYPES.map((t) => [t.key, t.color]));

@Component({
  selector: 'cz-costs',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [SnCommandBarImports, SnNumberBox, SnStatusPage, HlmAlertImports, HlmButtonImports, HlmBadgeImports, HlmCardImports, HlmTableImports, LineChart, Kpi, AgentTag],
  host: { class: 'flex min-h-0 flex-col' },
  template: `
    <sn-command-bar [title]="r() ? 'What did “' + r()!.title + '” really cost?' : 'Cost explained'" titlePosition="start" class="border-b">
      @if (r(); as r) {
        <span snCommandBarEnd class="flex items-center gap-1.5 pe-2">
          <cz-agent [agent]="r.agent" /><span hlmBadge variant="secondary">{{ r.sourceName }} · {{ r.effective.label }}</span>
        </span>
      }
      <button snCommand label="Back to chat" icon="lucideArrowLeft" [priority]="2" (click)="state.go('history')"></button>
      <button snCommand label="Session analysis" icon="lucideChartColumn" [priority]="1" [disabled]="!state.current()" (click)="state.go('analysis')"></button>
    </sn-command-bar>
    <div class="grid min-h-0 flex-1 auto-rows-max content-start gap-4 overflow-auto px-6 pt-4 pb-10 scroll-thin">
    @if (r(); as r) {
      @if (r.effective.kind === 'unset') {
        <div hlmAlert class="sn-tone-accent flex items-center justify-between gap-3">
          <p hlmAlertDescription class="m-0"><b>Billing isn't set for {{ r.sourceName }}.</b> Tell the app whether it uses an API key, a subscription or a free tier to see what this cost you.</p>
          <button hlmBtn size="sm" (click)="state.go('settings')">Set billing</button>
        </div>
      }
      <div class="grid grid-cols-3 gap-3">
        <div hlmCard data-size="sm" class="content-start gap-1.5 px-4">
          <span class="cz-label">List price (API-equivalent)</span>
          <span class="tabular text-3xl font-bold tracking-tight">{{ fmtUSD(r.list) }}</span>
          <p class="m-0 text-[12.5px] text-muted-foreground">What these tokens would cost billed per token at the published API rates. Use it to compare sessions. It is not the provider's compute cost, which isn't public.</p>
        </div>
        <div hlmCard data-size="sm" class="content-start gap-1.5 px-4 ring-2 ring-primary">
          <span class="cz-label">What it cost you</span>
          <span class="tabular text-3xl font-bold tracking-tight">{{ r.effective.kind === 'unset' ? '—' : fmtUSD(r.effective.effective) }}</span>
          <p class="m-0 text-[12.5px] text-muted-foreground">{{ youPaid(r) }}</p>
        </div>
        <div hlmCard data-size="sm" class="content-start gap-1.5 px-4">
          <span class="cz-label">Value multiplier</span>
          <span class="tabular text-3xl font-bold tracking-tight">{{ multiplier(r) }}</span>
          <p class="m-0 text-[12.5px] text-muted-foreground">{{ multiplierText(r) }}</p>
        </div>
      </div>
      <div class="grid grid-cols-[minmax(0,3fr)_minmax(0,2fr)] gap-4">
        <section hlmCard data-size="sm" class="content-start gap-3 px-4">
          <h3 class="m-0 text-[13.5px] font-semibold">Why the list price is {{ fmtUSD(r.list) }}</h3>
          <div class="grid gap-1 font-[family-name:var(--font-mono)] text-xs">
            @for (x of r.rows ?? []; track x.key) {
              @if (x.tokens) {
                <div class="grid grid-cols-[10px_100px_minmax(0,1fr)_90px] items-center gap-2.5 tabular">
                  <i class="size-2.5 rounded-[3px]" [style.background]="color(x.key)"></i><span>{{ x.name }}</span>
                  <span>{{ fmtT(x.tokens).padStart(6) }} × \${{ x.rate.toFixed(x.rate < 1 ? 3 : 2) }}/M</span><b class="text-right">{{ fmtUSD(x.cost) }}</b>
                </div>
              }
            }
            <div class="mt-0.5 grid grid-cols-[10px_100px_minmax(0,1fr)_90px] gap-2.5 border-t pt-1.5"><i></i><span>Total</span><span></span><b class="text-right">{{ fmtUSD(r.list) }}</b></div>
          </div>
          @if (r.unpriced) {
            <p class="m-0 text-xs text-[var(--warn)]">{{ fmtT(r.unpriced) }} tokens are from models with no known price and are not included. Add them in Settings → Pricing.</p>
          }
          <ol class="m-0 grid gap-1.5 pl-4.5 text-[13px]">
            <li><b>Every step is one API call, and each call re-sends the whole conversation</b>: the system prompt, tools, and every earlier message and tool output. This session made <b>{{ r.calls.toLocaleString('en-US') }}</b> calls (subagents included) with on average <b>{{ fmtT(r.avgContext) }}</b> tokens of context each, so <b>{{ fmtT(r.totalContext) }}</b> input tokens in total.</li>
            <li><b>Caching makes re-sending cheap.</b> {{ (r.cacheShare * 100).toFixed(0) }}% of that input was read from cache at \${{ r.rate.cacheRead }}/M instead of \${{ r.rate.input }}/M. Without caching, this session would have cost {{ fmtUSD(r.noCache) }}.</li>
            @if (r.rate.cacheWrite1h) {
              <li><b>Cache writes cost extra.</b> New context is written to cache at \${{ r.rate.cacheWrite1h }}/M for a 1-hour lifetime (what Claude Code mostly uses) or \${{ r.rate.cacheWrite5m }}/M for 5 minutes.</li>
            }
            <li><b>Output is the most expensive token</b>, at \${{ r.rate.output }}/M, and it includes thinking.</li>
            <li><b>So the biggest lever is context size × number of steps</b>, not the length of your prompt. See <button hlmBtn variant="link" class="h-auto p-0 text-[13px]" (click)="state.go('analysis')">Session analysis</button> for what drove them.</li>
          </ol>
          @if (r.reported != null) {
            <p class="m-0 text-xs text-muted-foreground">Claude Code's own cost total for this session: {{ fmtUSD(r.reported) }}.</p>
          }
        </section>
        <section hlmCard data-size="sm" class="content-start gap-2.5 px-4">
          <h3 class="m-0 text-[13.5px] font-semibold">Price scenarios for the same session</h3>
          <p class="m-0 text-xs text-muted-foreground">Re-priced from the same token counts. Real compute cost isn't public, so "future" prices are always a scenario you choose.</p>
          <div hlmTableContainer class="rounded-lg border">
            <table hlmTable class="text-[12.5px]">
              <thead hlmTHead><tr hlmTr><th hlmTh>Scenario</th><th hlmTh class="text-right">Cost</th></tr></thead>
              <tbody hlmTBody>
                @for (s of r.scenarios ?? []; track s.name) {
                  <tr hlmTr [class.bg-muted/60]="s.highlight">
                    <td hlmTd class="whitespace-normal" [class.font-medium]="s.highlight">{{ s.name }}<div class="text-[11.5px] font-normal text-muted-foreground">{{ s.note }}</div></td>
                    <td hlmTd class="text-right tabular align-top" [class.font-semibold]="s.highlight">{{ fmtUSD(s.cost) }}</td>
                  </tr>
                }
              </tbody>
            </table>
          </div>
          <div class="flex items-center justify-between gap-3 text-[13px]">
            <span id="cz-factor">Custom price factor</span>
            <sn-number-box style="inline-size: 8rem" ariaLabel="Custom price factor" [min]="0" [max]="10" [step]="0.05" [largeStep]="0.5" [decimals]="2" acceptsExpression
              [value]="factor()" (valueChange)="$event != null && factor.set($event)" />
          </div>
        </section>
      </div>
      @if (r.period; as p) {
        <section hlmCard data-size="sm" class="content-start gap-2.5 px-4">
          <div class="flex items-center justify-between"><h3 class="m-0 text-[13.5px] font-semibold">This billing period: list price used vs. plan fee</h3>
            <span class="text-xs text-muted-foreground">{{ fmtDay(p.start) }} – {{ fmtDay(p.end) }} · all {{ p.sourceName }} sessions</span></div>
          <div class="grid grid-cols-4 gap-2.5">
            <cz-kpi label="List price so far" [value]="fmtUSD(p.list)" [hint]="'day ' + p.day + ' of ' + p.days" />
            <cz-kpi label="You pay" [value]="fmtUSD(p.fee)" hint="flat plan fee" />
            <cz-kpi label="Value so far" [value]="p.multiplier.toFixed(1) + '×'" hint="list price ÷ fee" />
            <cz-kpi label="Projected at renewal" [value]="fmtUSD(p.projected)" hint="at the current pace" />
          </div>
          <div class="flex flex-wrap gap-3.5 text-xs text-muted-foreground">
            <span class="inline-flex items-center gap-1.5"><i class="size-2.5 rounded-[3px] bg-[var(--s1)]"></i>Cumulative list price (dashed: projection)</span>
            <span class="inline-flex items-center gap-1.5"><i class="size-2.5 rounded-[3px] bg-foreground"></i>Plan fee</span>
            @if (p.paidOffDate) { <span>Plan paid for itself on {{ fmtDay(p.paidOffDate) }}</span> }
          </div>
          <cz-line-chart [data]="period()" [height]="200" [ref]="p.fee" [refLabel]="'Plan fee ' + fmtUSD(p.fee)" refColor="var(--foreground)"
            [projectTo]="{ points: p.days, value: p.projected }" [projectionLabel]="fmtUSD(p.projected) + ' projected'"
            seriesName="List price so far" [yFormat]="usd" [valueFormat]="usdFmt" ariaLabel="Cumulative list price this billing period versus plan fee" />
          <p class="m-0 text-xs text-muted-foreground">
            Claude Code transcripts don't record how much of your plan's usage limit is left, so this chart is about money only.
            @if (r.rateLimitPct != null) { This Codex source reports {{ r.rateLimitPct.toFixed(0) }}% of its primary rate limit used. }
          </p>
        </section>
      } @else if (r.rateLimitPct != null) {
        <p class="m-0 text-xs text-muted-foreground">Codex reports {{ r.rateLimitPct.toFixed(0) }}% of its primary rate limit used.</p>
      }
    } @else if (err()) {
      <sn-status-page icon="lucideTriangleAlert" title="The cost report failed" [description]="err()!" />
    } @else if (state.current()) {
      <p class="text-sm text-muted-foreground">Loading…</p>
    } @else {
      <sn-status-page icon="lucideCircleDollarSign" title="No session open" description="Open a session in History to see what it really cost you.">
        <button hlmBtn variant="outline" (click)="state.go('history')">Go to History</button>
      </sn-status-page>
    }
    </div>
  `,
})
export class Costs {
  protected readonly state = inject(AppState);
  protected readonly fmtUSD = fmtUSD;
  protected readonly fmtT = fmtT;
  protected readonly fmtDay = fmtDay;
  protected readonly usd = (v: number) => '$' + Math.round(v);
  protected readonly usdFmt = fmtUSD;
  readonly r = signal<core.CostReport | null>(null);
  readonly err = signal<string | null>(null);
  readonly factor = signal(0.7);

  constructor() {
    effect(() => {
      const c = this.state.current();
      const f = this.factor();
      this.state.sources(); // billing edits
      untracked(() => {
        if (!c) return;
        UsageService.CostReport(c.sourceId, c.id, f)
          .then((r) => this.r.set(r))
          .catch((e) => this.err.set(errText(e)));
      });
    });
  }

  readonly period = computed<LinePoint[]>(() => (this.r()?.period?.points ?? []).map((p) => ({ label: fmtDay(p.date), title: fmtDay(p.date), value: p.cum })));

  color(k: string): string {
    return COLOR[k] ?? 'var(--muted-foreground)';
  }
  youPaid(r: core.CostReport): string {
    const e = r.effective;
    switch (e.kind) {
      case 'api':
        return r.billing.discountPct ? `API key with a ${r.billing.discountPct}% discount.` : 'This source uses an API key, so you are billed the list price.';
      case 'free':
        return 'Free tier: no charge. It still counts towards the free usage limits.';
      case 'subscription':
      case 'team':
        return `Your ${fmtUSD(e.fee)}/month plan × this session's ${(e.share * 100).toFixed(1)}% share of the ${fmtUSD(e.periodList)} list price used in its billing period (${fmtDay(e.periodStart)} – ${fmtDay(e.periodEnd)}).`;
      default:
        return 'Unknown until you set the billing type for this source in Settings.';
    }
  }
  multiplier(r: core.CostReport): string {
    const e = r.effective;
    if (e.kind === 'free') return '∞';
    if (e.kind === 'subscription' || e.kind === 'team') return e.fee ? (e.periodList / e.fee).toFixed(1) + '×' : '—';
    if (e.kind === 'api') return (1 / (1 - (r.billing.discountPct || 0) / 100)).toFixed(1) + '×';
    return '—';
  }
  multiplierText(r: core.CostReport): string {
    const e = r.effective;
    if (e.kind === 'subscription' || e.kind === 'team')
      return `Each $1 of plan fee bought ${fmtUSD(e.periodList / (e.fee || 1))} of API-priced usage in that period. Heavy sessions don't raise your bill, but they do use up your usage limits.`;
    if (e.kind === 'free') return 'Free usage.';
    if (e.kind === 'api') return 'You pay what you use.';
    return 'Set billing in Settings.';
  }
}
