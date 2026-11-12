import { ChangeDetectionStrategy, Component, ElementRef, computed, effect, inject, signal, untracked, viewChild } from '@angular/core';
import { SnContextMenu, SnInspectorImports, SnMenuItem, SnStatusPage } from '@spartan-native/desktop-ui';
import { SnDialogActionsImports, SnSelectableImports } from '@spartan-native/helm';
import { HlmBadgeImports } from '@spartan-ng/helm/badge';
import { HlmBreadcrumbImports } from '@spartan-ng/helm/breadcrumb';
import { HlmButtonImports } from '@spartan-ng/helm/button';
import { HlmCollapsibleImports } from '@spartan-ng/helm/collapsible';
import { HlmSheetImports } from '@spartan-ng/helm/sheet';
import { HlmSkeletonImports } from '@spartan-ng/helm/skeleton';
import { HlmTableImports } from '@spartan-ng/helm/table';
import { HistoryService, core, m } from '../lib/api';
import { AGENTS, ctx, errText, fmtD, fmtT, fmtUSD, fmtWhen, shortModel, total } from '../lib/format';
import { AppState } from '../lib/state';
import { AgentTag, FindingsList, Kpi, Segmented, TONE, TokenBreakdown } from './shared';

const KIND_LABEL: Record<string, string> = {
  thinking: 'thinking', text: 'note', shell: 'shell', read: 'read', edit: 'edit', search: 'search', agent: 'agent', tool: 'tool', system: 'system',
};

const key = (s: { sourceId: string; id: string }) => `${s.sourceId}/${s.id}`;

// ---------- sessions column ----------

@Component({
  selector: 'cz-sessions',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [SnSelectableImports, SnContextMenu, AgentTag, Segmented],
  host: { class: 'flex min-h-0 flex-col border-r' },
  template: `
    <div class="grid gap-2 border-b p-3">
      <div class="flex items-center justify-between gap-2">
        <b class="truncate text-[13px]" [title]="state.project()?.path ?? ''">Sessions <span class="font-normal text-muted-foreground">{{ state.sessions().length }}</span></b>
        <cz-segmented label="Sort sessions" [options]="tabs" [value]="state.sessionSort()" (valueChange)="state.setSort($event)" />
      </div>
    </div>
    <div snSelectable [multiple]="false" [selection]="selection()" (selectionChange)="select($event)" (activated)="analyze($event)"
      aria-label="Sessions" class="grid min-h-0 content-start gap-px overflow-auto p-2 scroll-thin">
      @for (s of state.sessions(); track key(s)) {
        <div snSelectableItem [value]="key(s)" [snSelectableLabel]="s.title" [snContextMenu]="menuFor(s)" class="text-[13px]">
          <!-- the skin lays rows out as a flex line; the two-line content is its own grid -->
          <div class="grid w-full min-w-0 grid-cols-[minmax(0,1fr)_auto] gap-x-2 gap-y-0.5 py-1.5">
          <span class="truncate" [title]="s.title">{{ s.title }}</span>
          <span class="sub tabular text-xs text-muted-foreground">{{ s.noUsageData ? '—' : fmtUSD(s.usage.costUSD) }}</span>
          <span class="sub col-span-2 flex min-w-0 items-center gap-1 overflow-hidden whitespace-nowrap text-[11px] text-muted-foreground">
            <cz-agent [agent]="s.agent" />·
            <span class="truncate">{{ fmtWhen(s.endedAt || s.startedAt) }} · {{ s.turnCount }} turn{{ s.turnCount === 1 ? '' : 's' }}{{ s.subagentCount ? ' · ' + s.subagentCount + ' agents' : '' }}</span>
            @if (s.findingsHigh) {
              <span class="size-1.5 shrink-0 rounded-full bg-[var(--crit)]" [title]="s.findingsHigh + ' high findings'"></span>
            }
          </span>
          </div>
        </div>
      } @empty {
        <p class="px-2 text-[13px] text-muted-foreground">No sessions.</p>
      }
    </div>
  `,
})
export class SessionsColumn {
  protected readonly state = inject(AppState);
  protected readonly fmtUSD = fmtUSD;
  protected readonly fmtWhen = fmtWhen;
  protected readonly key = key;
  protected readonly tabs = [
    { id: 'recent' as const, label: 'Recent' },
    { id: 'cost' as const, label: 'Cost' },
  ];
  readonly selection = computed(() => {
    const c = this.state.current();
    return c ? [key(c)] : [];
  });

  select(keys: readonly string[]): void {
    const s = this.state.sessions().find((x) => key(x) === keys[0]);
    const c = this.state.current();
    if (s && (!c || key(c) !== keys[0] || this.state.subagentId())) void this.state.openSession(s.sourceId, s.id);
  }
  /** Enter or double-click: open the session's analysis. */
  analyze(k: string): void {
    const s = this.state.sessions().find((x) => key(x) === k);
    if (s) void this.state.openSession(s.sourceId, s.id).then(() => this.state.go('analysis'));
  }
  menuFor(s: core.SessionRow): SnMenuItem[] {
    const open = (then?: () => void) => () => void this.state.openSession(s.sourceId, s.id).then(then);
    return [
      { id: 'open', label: 'Open', action: open(() => this.state.go('history')) },
      { id: 'analyze', label: 'Session analysis', action: open(() => this.state.go('analysis')) },
      { id: 'costs', label: 'Cost explained', action: open(() => this.state.go('costs')) },
      { type: 'separator' },
      { id: 'copy-id', label: 'Copy session ID', action: () => void navigator.clipboard.writeText(s.id) },
    ];
  }
}

// ---------- chat ----------

@Component({
  selector: 'cz-chat',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [SnStatusPage, HlmButtonImports, HlmBadgeImports, HlmBreadcrumbImports, HlmCollapsibleImports, HlmSkeletonImports, AgentTag],
  host: { class: 'flex min-h-0 min-w-0 flex-col' },
  template: `
    <header class="flex min-h-11 min-w-0 items-center gap-2 border-b px-4 py-2">
        @if (state.project()) {
        <nav hlmBreadcrumb class="min-w-0 overflow-hidden">
          <ol hlmBreadcrumbList class="flex-nowrap overflow-hidden whitespace-nowrap">
            <li hlmBreadcrumbItem class="shrink-0"><a hlmBreadcrumbLink class="cursor-pointer" (click)="state.go('overview')">{{ state.project()?.name }}</a></li>
            @if (s()) { <li hlmBreadcrumbSeparator></li> }
            @if (state.subagentId()) {
              <li hlmBreadcrumbItem><a hlmBreadcrumbLink class="max-w-56 cursor-pointer truncate" (click)="state.openSubagent(null)">{{ s()?.title }}</a></li>
              <li hlmBreadcrumbSeparator></li>
              <li hlmBreadcrumbItem><span hlmBreadcrumbPage class="max-w-64 truncate">{{ state.subagent()?.description }}</span>
                <span hlmBadge variant="secondary">{{ state.subagent()?.type }}</span></li>
            } @else {
              <li hlmBreadcrumbItem class="min-w-0"><span hlmBreadcrumbPage class="block truncate" [title]="s()?.title ?? ''">{{ s()?.title }}</span></li>
            }
          </ol>
        </nav>
        }
        @if (s(); as ss) {
          <span class="flex shrink-0 items-center gap-1.5">
            <cz-agent [agent]="ss.agent" />
            <span hlmBadge variant="secondary">{{ shortModel(state.subagent()?.model || ss.mainModel) }}</span>
            @if (ss.gitBranch) { <span hlmBadge variant="outline" class="mono">{{ ss.gitBranch }}</span> }
          </span>
        }
    </header>
    <div #body class="grid min-h-0 flex-1 auto-rows-max content-start gap-6 overflow-auto px-5 pt-4 pb-16 scroll-thin">
      @if (state.loading()) {
        @for (i of [1, 2, 3]; track i) {
          <div class="grid gap-2"><hlm-skeleton class="ml-auto h-10 w-2/3" /><hlm-skeleton class="h-8 w-full" /><hlm-skeleton class="h-14 w-5/6" /></div>
        }
      } @else if (state.error()) {
        <sn-status-page icon="lucideTriangleAlert" title="This session could not be read" [description]="state.error()!" />
      } @else {
        @if (state.subagentId()) {
          <div class="text-xs text-muted-foreground">Subagent transcript · started from turn {{ (parentTurn() ?? 0) + 1 }} of the parent session</div>
        }
        @for (t of state.turns(); track $index; let i = $index) {
          <div class="-m-2 grid cursor-pointer gap-2 rounded-lg border border-transparent p-2 hover:bg-muted/40" [id]="'turn-' + i"
            [class.!border-border]="state.turn() === i" [class.bg-muted/50]="state.turn() === i" (click)="selectTurn(i)">
            @if (t.prompt) {
              <div class="selectable max-w-[78%] justify-self-end whitespace-pre-wrap rounded-[14px_14px_4px_14px] bg-primary px-3 py-2 text-[13.5px] text-primary-foreground">{{ clip(t.prompt) }}</div>
            }
            <div hlmCollapsible [expanded]="open().has(i)" class="overflow-hidden rounded-lg border bg-card" (click)="$event.stopPropagation()">
              <button hlmCollapsibleTrigger class="flex w-full cursor-pointer items-center gap-2 px-3 py-2 text-left text-[12.5px] text-muted-foreground" (click)="toggle(i)">
                <span class="inline-block transition-transform" [class.rotate-90]="open().has(i)">›</span>
                <b class="font-medium text-foreground">Worked for {{ fmtD(t.durationMs) || '—' }}</b>
                · {{ (t.steps ?? []).length }} steps{{ agentsIn(t) ? ' · ' + agentsIn(t) + ' subagents' : '' }}
                <span class="ml-auto flex items-center gap-2 tabular">
                  @if (t.compacted) { <span hlmBadge variant="outline">compacted</span> }
                  <span>peak ctx {{ fmtT(t.peakContext) }}</span>
                  <span hlmBadge variant="secondary">{{ fmtUSD(t.usage.costUSD + t.subagentUsage.costUSD) }}</span>
                </span>
              </button>
              @if (open().has(i)) {
                <div hlmCollapsibleContent class="grid border-t">
                  @for (st of t.steps ?? []; track st.id) {
                    <div class="grid grid-cols-[62px_minmax(0,1fr)_auto] items-center gap-2.5 border-t border-border/60 px-3 py-1 text-[12.5px] first:border-t-0"
                      [class.bg-[color-mix(in_srgb,var(--s3)_7%,transparent)]]="st.kind === 'agent'"
                      [class.cursor-pointer]="clickable(st)" [class.hover:bg-accent]="clickable(st)"
                      [attr.role]="clickable(st) ? 'button' : null" [attr.tabindex]="clickable(st) ? 0 : null"
                      (click)="clickable(st) && state.openStep(st.id, i)" (keydown.enter)="clickable(st) && state.openStep(st.id, i)">
                      <span class="mono text-[11px] text-muted-foreground">{{ KIND_LABEL[st.kind] }}</span>
                      <span class="truncate" [class.mono]="!prose(st)" [class.italic]="prose(st)" [class.text-muted-foreground]="prose(st)" [class.text-[var(--crit)]]="st.isError" [title]="st.title">{{ st.title }}</span>
                      <span class="flex items-center gap-1.5 tabular text-[11.5px] text-muted-foreground">
                        @if (st.category) {
                          <span hlmBadge [class]="st.durationMs > 120000 ? TONE['high'] : st.durationMs > 30000 ? TONE['medium'] : ''" variant="outline">{{ st.category === 'heavy' ? 'CPU' : st.category }}</span>
                        }
                        @if (st.resultTokens > 20000) { <span hlmBadge [class]="TONE['medium']">{{ fmtT(st.resultTokens) }} tok</span> }
                        @if (st.isError) { <span hlmBadge [class]="TONE['high']">error</span> }
                        {{ fmtD(st.durationMs) }}
                        @if (st.subagentId) {
                          <button hlmBtn variant="outline" size="xs" (click)="$event.stopPropagation(); state.openSubagent(st.subagentId)">Open</button>
                        } @else if (st.kind === 'agent') {
                          <span hlmBadge variant="secondary">subagent</span>
                        }
                      </span>
                    </div>
                  } @empty {
                    <div class="px-3 py-1.5 text-[12.5px] text-muted-foreground">No intermediate steps.</div>
                  }
                </div>
              }
            </div>
            @if (t.answer) {
              <div class="flex items-start gap-2.5">
                <span class="mono grid size-[22px] shrink-0 place-items-center rounded-md bg-muted text-[10px] font-bold text-muted-foreground">{{ initial() }}</span>
                <div class="grid max-w-[92%] gap-1.5 text-[13.5px]">
                  <p class="selectable m-0 whitespace-pre-wrap">{{ clip(t.answer) }}</p>
                  <div class="flex flex-wrap gap-1.5 text-[11.5px] text-muted-foreground">
                    <span>{{ fmtT(t.usage.output) }} out</span>·<span>{{ fmtT(t.usage.cacheRead) }} cache read</span>·<span>{{ t.usage.calls }} calls</span>
                  </div>
                </div>
              </div>
            }
          </div>
        } @empty {
          <sn-status-page icon="lucideMessagesSquare" title="No session open" description="Pick a project in the sidebar, then a session from the list." />
        }
      }
    </div>
  `,
})
export class Chat {
  protected readonly state = inject(AppState);
  protected readonly fmtD = fmtD;
  protected readonly fmtT = fmtT;
  protected readonly fmtUSD = fmtUSD;
  protected readonly shortModel = shortModel;
  protected readonly KIND_LABEL = KIND_LABEL;
  protected readonly TONE = TONE;
  private readonly body = viewChild<ElementRef<HTMLElement>>('body');
  readonly open = signal(new Set<number>());
  readonly s = this.state.session;
  readonly initial = computed(() => (AGENTS[this.state.session()?.agent ?? 'claude'] ?? 'C')[0]);
  readonly parentTurn = computed(() => {
    const id = this.state.subagentId();
    return this.state.session()?.subagents?.find((a) => a.id === id)?.parentTurn ?? null;
  });

  constructor() {
    // Reset expansion when the transcript changes; expand + scroll when asked.
    effect(() => {
      this.state.turns();
      untracked(() => this.open.set(new Set(this.state.turn() != null ? [this.state.turn()!] : [])));
    });
    effect(() => {
      const req = this.state.scrollToTurn();
      if (!req) return;
      untracked(() => {
        const s = new Set(this.open());
        s.add(req.turn);
        this.open.set(s);
        setTimeout(() => this.body()?.nativeElement.querySelector('#turn-' + req.turn)?.scrollIntoView({ block: 'start' }), 30);
      });
    });
  }

  toggle(i: number): void {
    const s = new Set(this.open());
    if (s.has(i)) s.delete(i);
    else s.add(i);
    this.open.set(s);
    this.state.turn.set(i);
  }
  selectTurn(i: number): void {
    this.state.turn.set(this.state.turn() === i ? null : i);
  }
  agentsIn(t: m.Turn): number {
    return (t.steps ?? []).filter((s) => s.kind === 'agent').length;
  }
  prose(st: m.Step): boolean {
    return st.kind === 'thinking' || st.kind === 'text' || st.kind === 'system';
  }
  clickable(st: m.Step): boolean {
    return !this.prose(st) || st.input.length > 200;
  }
  clip(s: string): string {
    return s.length > 6000 ? s.slice(0, 6000) + '\n…' : s;
  }
}

// ---------- usage & insights panel (inspector content) ----------

@Component({
  selector: 'cz-panel',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [SnInspectorImports, HlmButtonImports, HlmTableImports, TokenBreakdown, FindingsList, Segmented],
  host: { class: 'grid w-full min-w-0 content-start gap-4' },
  template: `
    @if (state.scope(); as sc) {
      <div class="grid gap-2">
        <span class="truncate text-[13px] font-semibold" [title]="sc.name">{{ sc.name }}</span>
        @if (state.turn() != null) {
          <button hlmBtn variant="outline" size="xs" class="justify-self-start" (click)="state.turn.set(null)">Show whole {{ state.subagentId() ? 'subagent' : 'session' }}</button>
        }
        <cz-segmented label="Panel" [options]="tabs()" [value]="state.panelTab()" (valueChange)="state.panelTab.set($event)" />
      </div>
      <sn-inspector-section title="List price (API-equivalent)">
        <div class="grid gap-0.5">
          <span class="tabular text-[1.75rem] leading-tight font-bold tracking-tight">{{ sc.usage.unpricedTokens && !sc.usage.costUSD ? 'Price unknown' : fmtUSD(sc.usage.costUSD) }}</span>
          <span class="text-xs text-muted-foreground">{{ effectiveHint(sc.usage.costUSD) }}</span>
          <button hlmBtn variant="link" size="xs" class="h-auto justify-self-start p-0" (click)="state.go('costs')">What does this mean?</button>
        </div>
      </sn-inspector-section>
      <sn-inspector-section title="Usage">
        <sn-inspector-property label="Total tokens"><span class="tabular">{{ fmtT(total(sc.usage)) }}</span></sn-inspector-property>
        <sn-inspector-property label="Output"><span class="tabular">{{ fmtT(sc.usage.output) }}{{ sc.usage.reasoning ? ' · ' + fmtT(sc.usage.reasoning) + ' thinking' : '' }}</span></sn-inspector-property>
        <sn-inspector-property label="Peak context"><span class="tabular">{{ fmtT(peak()) }}</span></sn-inspector-property>
        <sn-inspector-property label="Cache hit"><span class="tabular">{{ cacheHit(sc.usage) }}</span></sn-inspector-property>
        <sn-inspector-property label="API calls"><span class="tabular">{{ sc.usage.calls }}</span></sn-inspector-property>
      </sn-inspector-section>
      @if (state.panelTab() === 'usage') {
        <sn-inspector-section title="Cost share by token type">
          <cz-token-breakdown [usage]="sc.usage" [hideCacheWrite]="!sc.usage.cacheWrite5m && !sc.usage.cacheWrite1h" />
        </sn-inspector-section>
        @if (models().length) {
          <sn-inspector-section title="Models">
            <div hlmTableContainer class="rounded-lg border">
              <table hlmTable class="text-xs">
                <thead hlmTHead><tr hlmTr><th hlmTh>Model</th><th hlmTh class="text-right">Tokens</th><th hlmTh class="text-right">Cost</th></tr></thead>
                <tbody hlmTBody>
                  @for (r of models(); track r.model) {
                    <tr hlmTr><td hlmTd class="mono">{{ r.model }}</td><td hlmTd class="text-right tabular">{{ fmtT(r.tokens) }}</td><td hlmTd class="text-right tabular">{{ r.unpriced ? '—' : fmtUSD(r.cost) }}</td></tr>
                  }
                </tbody>
              </table>
            </div>
          </sn-inspector-section>
        }
        @if (sc.kind === 'Session' && state.session()?.reportedCost != null) {
          <p class="m-0 text-xs text-muted-foreground">Claude Code's own cost-state total is {{ fmtUSD(state.session()!.reportedCost!) }} ({{ diff(sc.usage.costUSD, state.session()!.reportedCost!) }}).</p>
        }
        @if (sc.kind === 'Session' && state.session()?.subagentUsage?.costUSD) {
          <p class="m-0 text-xs text-muted-foreground">Includes {{ state.session()!.subagentCount }} subagents: {{ fmtUSD(state.session()!.subagentUsage.costUSD) }}.</p>
        }
        @for (w of state.session()?.warnings ?? []; track w) {
          <p class="m-0 text-xs text-[var(--warn)]">Parsed with warnings: {{ w }}</p>
        }
        <button hlmBtn variant="outline" size="sm" (click)="state.go('analysis')">Open full analysis</button>
      } @else {
        <cz-findings [findings]="findings()" [showLinks]="state.turn() == null" empty="No findings for this scope." />
      }
    }
  `,
})
export class Panel {
  protected readonly state = inject(AppState);
  protected readonly fmtT = fmtT;
  protected readonly fmtUSD = fmtUSD;
  protected readonly total = total;
  readonly peak = computed(() => Math.max(0, ...(this.state.scope()?.turns ?? []).map((t) => t.peakContext)));
  readonly findings = computed(() => {
    const d = this.state.detail();
    if (!d) return [];
    const sub = this.state.subagentId();
    const t = this.state.turn();
    if (t != null) return (d.allFindings ?? []).filter((f) => f.turn === t && (f.subagentId || null) === sub);
    if (sub) return (d.findings ?? []).filter((f) => f.subagentId === sub);
    return d.findings ?? [];
  });
  readonly countBad = computed(() => this.findings().filter((f) => f.severity !== 'ok').length);
  readonly tabs = computed(() => [
    { id: 'usage' as const, label: 'Usage' },
    { id: 'insights' as const, label: this.countBad() ? `Insights (${this.countBad()})` : 'Insights' },
  ]);
  readonly models = computed(() => {
    const sc = this.state.scope();
    const s = this.state.session();
    if (!sc || !s) return [];
    if (sc.kind === 'Session') {
      return Object.entries(s.byModel ?? {})
        .map(([model, u]) => ({ model, tokens: total(u!), cost: u!.costUSD, unpriced: u!.unpricedTokens > 0 && !u!.costUSD }))
        .sort((a, b) => b.tokens - a.tokens);
    }
    const ms = new Set<string>();
    for (const t of sc.turns) for (const x of t.models ?? []) ms.add(x);
    return [...ms].map((model) => ({ model, tokens: ms.size === 1 ? total(sc.usage) : 0, cost: ms.size === 1 ? sc.usage.costUSD : 0, unpriced: false }));
  });
  cacheHit(u: m.Usage): string {
    const c = ctx(u);
    return c ? Math.round((u.cacheRead / c) * 100) + '%' : '—';
  }
  diff(a: number, b: number): string {
    const d = (a / b - 1) * 100;
    return `estimate ${d >= 0 ? '+' : ''}${d.toFixed(1)}%`;
  }
  effectiveHint(list: number): string {
    const d = this.state.detail();
    if (!d) return '';
    const b = d.billing;
    const e = d.effective;
    switch (b.kind) {
      case 'api':
        return b.discountPct ? `API key: you paid ${fmtUSD(list * (1 - b.discountPct / 100))}` : 'API key: billed per token, you paid this';
      case 'free':
        return 'Free tier: no charge';
      case 'subscription':
      case 'team': {
        if (!e.periodList) return 'Subscription';
        const eff = (b.fee * list) / e.periodList;
        return `≈ ${fmtUSD(eff)} of your plan${eff > 0 ? ` · ${(list / eff).toFixed(1)}× value` : ''}`;
      }
      default:
        return 'Billing not set for this source';
    }
  }
}

// ---------- step detail sheet ----------

@Component({
  selector: 'cz-step-sheet',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [HlmSheetImports, HlmButtonImports, HlmBadgeImports, SnDialogActionsImports, AgentTag, FindingsList],
  template: `
    <hlm-sheet side="right" [state]="state.sheetStep() ? 'open' : 'closed'" (closed)="state.sheetStep.set(null)">
      <hlm-sheet-content *hlmSheetPortal="let ctx" class="!w-[35rem] !max-w-[92vw] gap-0 p-0">
        @if (detail(); as d) {
          <hlm-sheet-header class="grid gap-1 border-b p-4 pr-12">
            <span class="cz-label">Step {{ d.step.index + 1 }} of {{ d.turnSteps }} · turn {{ d.turn + 1 }} · {{ d.step.tool || d.step.kind }}</span>
            <h3 hlmSheetTitle class="mono selectable text-sm break-all">{{ d.step.title }}</h3>
            <div class="flex flex-wrap items-center gap-1.5">
              @if (d.step.category) { <span hlmBadge [class]="TONE['medium']">{{ d.step.category === 'heavy' ? 'CPU-heavy' : d.step.category }}</span> }
              @if (d.step.exitCode != null) { <span hlmBadge [class]="d.step.exitCode ? TONE['high'] : ''" variant="secondary">exit {{ d.step.exitCode }}</span> }
              @if (d.step.isError) { <span hlmBadge [class]="TONE['high']">error</span> }
              <cz-agent [agent]="d.agent" />
            </div>
          </hlm-sheet-header>
          <div class="grid min-h-0 content-start gap-4 overflow-auto p-4 scroll-thin">
            <dl class="m-0 grid grid-cols-[150px_minmax(0,1fr)] gap-x-3 gap-y-1.5 text-[12.5px]">
              <dt class="text-muted-foreground">Started</dt><dd class="m-0 tabular">{{ started(d.step.startedAt) }}</dd>
              <dt class="text-muted-foreground">Wall time</dt>
              <dd class="m-0 tabular"><b>{{ fmtD(d.step.durationMs) || '—' }}</b>{{ d.step.durationMs && d.turnDurationMs ? ' · ' + pctOf(d.step.durationMs, d.turnDurationMs) + ' of the turn' : '' }}</dd>
              <dt class="text-muted-foreground">Tokens into context</dt>
              <dd class="m-0 tabular">{{ fmtT(d.step.resultTokens) }} <span class="text-muted-foreground">(re-sent on {{ d.laterCalls }} later calls)</span></dd>
              <dt class="text-muted-foreground">Estimated cost</dt><dd class="m-0 tabular">{{ fmtUSD(d.estCostUSD) }} <span class="text-muted-foreground">(cache write + later cache reads)</span></dd>
              <dt class="text-muted-foreground">Same step</dt>
              <dd class="m-0">
                @if (d.repeats > 1) { <span hlmBadge [class]="TONE['medium']">{{ d.repeats }}× in this turn</span> {{ fmtD(d.repeatsMs) }} total } @else { Ran once in this turn }
                @if (d.sessionRepeats > d.repeats) { <span class="text-muted-foreground"> · {{ d.sessionRepeats }}× in the session</span> }
              </dd>
            </dl>
            @if (d.step.input) {
              <div class="grid gap-1.5"><span class="cz-label">{{ d.step.kind === 'shell' ? 'Command' : 'Input' }}</span><pre class="out">{{ d.step.input }}</pre></div>
            }
            <div class="grid gap-1.5">
              <span class="cz-label">Output{{ d.output.length > 20000 ? ' (tail)' : '' }}</span>
              <pre class="out">{{ tail(d.output) || '(no output)' }}</pre>
            </div>
            @if (d.findings?.length) {
              <cz-findings [findings]="d.findings!" [showLinks]="false" />
            }
          </div>
          <hlm-sheet-footer snDialogButtons class="!flex-row justify-end gap-2 border-t p-3">
            <button hlmBtn variant="outline" size="sm" (click)="copy(d.step.input || d.step.title)">{{ copied() ? 'Copied' : 'Copy ' + (d.step.kind === 'shell' ? 'command' : 'input') }}</button>
            <button hlmBtn size="sm" snDefaultAction (click)="state.sheetStep.set(null)">Close</button>
          </hlm-sheet-footer>
        } @else if (err()) {
          <p class="p-4 text-sm text-[var(--crit)]">{{ err() }}</p>
        } @else {
          <p class="p-4 text-sm text-muted-foreground">Loading…</p>
        }
      </hlm-sheet-content>
    </hlm-sheet>
  `,
})
export class StepSheet {
  protected readonly state = inject(AppState);
  protected readonly fmtD = fmtD;
  protected readonly fmtT = fmtT;
  protected readonly fmtUSD = fmtUSD;
  protected readonly TONE = TONE;
  readonly detail = signal<core.StepDetail | null>(null);
  readonly err = signal<string | null>(null);
  readonly copied = signal(false);

  constructor() {
    effect(() => {
      const id = this.state.sheetStep();
      const c = this.state.current();
      const sub = this.state.subagentId() ?? '';
      untracked(() => {
        this.detail.set(null);
        this.err.set(null);
        this.copied.set(false);
        if (!id || !c) return;
        HistoryService.Step(c.sourceId, c.id, sub, id)
          .then((d) => this.state.sheetStep() === id && this.detail.set(d))
          .catch((e) => this.err.set(errText(e)));
      });
    });
  }
  started(ms: number): string {
    return ms ? new Date(ms).toLocaleString('en-GB') : '—';
  }
  pctOf(a: number, b: number): string {
    return Math.round((a / b) * 100) + '%';
  }
  tail(s: string): string {
    return s.length > 20000 ? '…\n' + s.slice(-20000) : s;
  }
  copy(s: string): void {
    void navigator.clipboard.writeText(s).then(() => this.copied.set(true));
  }
}

// ---------- history screen ----------

@Component({
  selector: 'cz-history',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [SnInspectorImports, SessionsColumn, Chat, Panel, StepSheet],
  host: { class: 'grid min-h-0 min-w-0 grid-rows-[minmax(0,1fr)]' },
  template: `
    <sn-inspector-layout class="h-full">
      <div class="grid min-h-0 min-w-0 flex-1 grid-cols-[16rem_minmax(0,1fr)]">
        <cz-sessions />
        <cz-chat />
      </div>
      <sn-inspector [title]="inspectorTitle()" [open]="state.panelOpen()" (openChange)="state.panelOpen.set($event)" style="inline-size: 21rem">
        <cz-panel />
      </sn-inspector>
    </sn-inspector-layout>
    <cz-step-sheet />
  `,
})
export class History {
  protected readonly state = inject(AppState);
  readonly inspectorTitle = computed(() => {
    const sc = this.state.scope();
    if (!sc) return 'Usage & insights';
    return sc.kind === 'Turn' ? `Turn ${(this.state.turn() ?? 0) + 1}` : sc.kind;
  });
}
