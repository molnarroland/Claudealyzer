import { ChangeDetectionStrategy, Component, computed, effect, inject, input, signal, untracked } from '@angular/core';
import { SnCommandBarImports, SnModalSheet, SnNumberBox, SnSettingsImports, SnStatusPage } from '@spartan-native/desktop-ui';
import { HlmBadgeImports } from '@spartan-ng/helm/badge';
import { HlmButtonImports } from '@spartan-ng/helm/button';
import { HlmCardImports } from '@spartan-ng/helm/card';
import { HlmInputImports } from '@spartan-ng/helm/input';
import { HlmSwitchImports } from '@spartan-ng/helm/switch';
import { BudgetService, NotifyService, SourceService, core } from '../lib/api';
import { AGENTS, errText, fmtDay, fmtUSD, fmtWhen } from '../lib/format';
import { AppState } from '../lib/state';
import { AgentTag, TONE } from './shared';

const LEVEL_COLOR: Record<string, string> = { ok: 'var(--s1)', warn: 'var(--warn)', over: 'var(--crit)' };
const WEEKDAYS = ['Rolling 7 days', 'Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday', 'Sunday'];

function time(ms: number): string {
  return new Date(ms).toLocaleTimeString('en-GB', { hour: '2-digit', minute: '2-digit' });
}

/** A progress bar with a projection tick and a warning mark. */
@Component({
  selector: 'cz-meter',
  changeDetection: ChangeDetectionStrategy.OnPush,
  host: { class: 'block' },
  template: `
    <div class="relative h-2.5 overflow-hidden rounded-full bg-muted" role="meter" [attr.aria-valuenow]="pct()" aria-valuemin="0" aria-valuemax="100" [attr.aria-label]="label()">
      <div class="h-full rounded-full transition-[width]" [style.width.%]="min(pct(), 100)" [style.background]="color()"></div>
      @if (projected() > pct() && projected() > 0) {
        <div class="absolute top-0 h-full w-0.5 bg-foreground/50" [style.left.%]="min(projected(), 100)" [title]="'At this pace: ' + projected().toFixed(0) + '%'"></div>
      }
    </div>
  `,
})
export class Meter {
  readonly value = input.required<{ pct: number; projected?: number; level?: string; label?: string }>();
  readonly pct = computed(() => Math.max(0, this.value().pct || 0));
  readonly projected = computed(() => Math.max(0, this.value().projected || 0));
  readonly color = computed(() => LEVEL_COLOR[this.value().level ?? 'ok'] ?? 'var(--s1)');
  readonly label = computed(() => this.value().label ?? '');
  protected readonly min = Math.min;
}

/**
 * Spending guard rails: budgets per scope and period, the 5-hour and weekly usage windows of each
 * source, and when to be alerted (in the app, and as an OS notification).
 */
@Component({
  selector: 'cz-budgets',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [
    SnCommandBarImports, SnModalSheet, SnNumberBox, SnSettingsImports, SnStatusPage, HlmBadgeImports, HlmButtonImports, HlmCardImports, HlmInputImports,
    HlmSwitchImports, AgentTag, Meter,
  ],
  host: { class: 'flex min-h-0 flex-col' },
  template: `
    <sn-command-bar title="Budgets & limits" titlePosition="start" class="border-b">
      <button snCommand label="Add budget" icon="lucidePlus" variant="accent" [priority]="2" (click)="edit(null)"></button>
    </sn-command-bar>
    <div class="grid min-h-0 flex-1 auto-rows-max content-start gap-5 overflow-auto px-7 pt-4 pb-10 scroll-thin">
      <section hlmCard data-size="sm" class="content-start gap-3 px-4">
        <div class="flex items-center justify-between"><h3 class="m-0 text-[13.5px] font-semibold">Budgets</h3>
          <span class="text-xs text-muted-foreground">Projection marks where the period ends at the pace so far</span></div>
        @for (b of status(); track b.budget.id) {
          <div class="grid gap-1.5 rounded-lg border px-3 py-2.5">
            <div class="flex items-center gap-2 text-[13px]">
              <b class="font-semibold">{{ b.budget.name || periodName(b.budget.period) + ' budget' }}</b>
              <span class="text-xs text-muted-foreground">{{ b.scope }} · {{ periodName(b.budget.period) }} · {{ b.budget.basis === 'paid' ? 'what you pay' : 'list price' }}</span>
              @if (b.level !== 'ok') { <span hlmBadge [class]="b.level === 'over' ? TONE['high'] : TONE['medium']">{{ b.level === 'over' ? 'Over budget' : 'Close to the limit' }}</span> }
              @if (b.budget.notify) { <span class="text-xs text-muted-foreground" title="Sends alerts">· alerts on</span> }
              <span class="ml-auto flex gap-1">
                <button hlmBtn variant="outline" size="xs" (click)="edit(b.budget)">Edit…</button>
                <button hlmBtn variant="ghost" size="xs" (click)="remove(b.budget)">Remove</button>
              </span>
            </div>
            <cz-meter [value]="{ pct: b.pct, projected: b.budget.limitUSD ? (b.projected / b.budget.limitUSD) * 100 : 0, level: b.level, label: b.budget.name || 'Budget' }" />
            <div class="flex flex-wrap justify-between gap-2 text-xs text-muted-foreground tabular">
              <span><b class="text-foreground">{{ fmtUSD(b.spent) }}</b> of {{ fmtUSD(b.budget.limitUSD) }} ({{ b.pct.toFixed(0) }}%)</span>
              <span>{{ fmtDay(b.periodStart) }} – {{ fmtDay(lastDay(b.periodEnd)) }} · at this pace {{ fmtUSD(b.projected) }}</span>
            </div>
          </div>
        } @empty {
          <sn-status-page icon="lucideGauge" title="No budgets yet" description="Set a daily, weekly or monthly limit for everything, one source or one project. You get an alert when spending gets close.">
            <button hlmBtn variant="outline" (click)="edit(null)">Add budget</button>
          </sn-status-page>
        }
      </section>

      <section hlmCard data-size="sm" class="content-start gap-3 px-4">
        <h3 class="m-0 text-[13.5px] font-semibold">Usage limits</h3>
        <p class="m-0 text-xs text-muted-foreground">
          Subscriptions limit usage per 5-hour window and per week, but don't publish the limits in tokens or dollars. Windows here are rebuilt from your history:
          a window opens with the first request after the previous one closed. Enter the API list price you can use per window to see how close you are; your largest window so far is a good first guess.
        </p>
        @for (l of limits(); track l.sourceId) {
          <div class="grid gap-2.5 rounded-lg border px-3 py-2.5">
            <div class="flex items-center gap-2 text-[13px]">
              <b class="font-semibold">{{ l.name }}</b><cz-agent [agent]="l.agent" />
              <span class="text-xs text-muted-foreground">{{ billingLabel(l.billing) }}</span>
              @if (l.rateLimitPct != null) {
                <span hlmBadge variant="outline" [title]="'Reported by ' + (AGENTS[l.agent] ?? l.agent) + ' ' + fmtWhen(l.rateLimitAt)">{{ l.rateLimitPct.toFixed(0) }}% of the primary limit used (reported)</span>
              }
              <button hlmBtn variant="outline" size="xs" class="ml-auto" (click)="editLimits(l)">Set limits…</button>
            </div>
            <div class="grid grid-cols-2 gap-4">
              <div class="grid content-start gap-1.5">
                <span class="text-xs font-medium">5-hour window</span>
                @if (l.block; as w) {
                  <cz-meter [value]="{ pct: w.limit ? w.pct : 0, projected: w.limit ? (w.projected / w.limit) * 100 : 0, level: w.level, label: '5-hour window' }" />
                  <span class="text-xs text-muted-foreground tabular">
                    <b class="text-foreground">{{ fmtUSD(w.spent) }}</b>{{ w.limit ? ' of ' + fmtUSD(w.limit) + ' (' + w.pct.toFixed(0) + '%)' : '' }} · {{ time(w.start) }} – {{ time(w.end) }} · {{ fmtUSD(w.burnPerHour) }}/h · at this pace {{ fmtUSD(w.projected) }}
                  </span>
                } @else {
                  <span class="text-xs text-muted-foreground">No window open.{{ l.lastBlock ? ' The last one used ' + fmtUSD(l.lastBlock.spent) + ', ' + fmtWhen(l.lastBlock.start) + '.' : '' }}</span>
                }
                @if (l.blocks?.length) {
                  <div class="flex h-10 items-end gap-px" role="img" aria-label="Recent 5-hour windows">
                    @for (b of l.blocks ?? []; track b.start) {
                      <i class="block min-w-1 flex-1 rounded-t-[2px]" [style.height.%]="barPct(b.spent, l)" [style.background]="l.billing.limit5hUSD && b.spent >= l.billing.limit5hUSD ? 'var(--crit)' : 'var(--s1)'"
                        [title]="fmtWhen(b.start) + ': ' + fmtUSD(b.spent)"></i>
                    }
                  </div>
                  <span class="text-[11px] text-muted-foreground tabular">Last {{ l.blocks?.length }} windows · largest {{ fmtUSD(l.maxBlock) }} · average {{ fmtUSD(l.avgBlock) }}</span>
                }
              </div>
              <div class="grid content-start gap-1.5">
                <span class="text-xs font-medium">Week ({{ l.billing.weekResetDay ? 'resets ' + WEEKDAYS[l.billing.weekResetDay] : 'rolling 7 days' }})</span>
                <cz-meter [value]="{ pct: l.week.limit ? l.week.pct : 0, projected: l.week.limit ? (l.week.projected / l.week.limit) * 100 : 0, level: l.week.level, label: 'Week' }" />
                <span class="text-xs text-muted-foreground tabular">
                  <b class="text-foreground">{{ fmtUSD(l.week.spent) }}</b>{{ l.week.limit ? ' of ' + fmtUSD(l.week.limit) + ' (' + l.week.pct.toFixed(0) + '%)' : '' }}
                  · since {{ fmtWhen(l.week.start) }}{{ l.billing.weekResetDay ? ' · at this pace ' + fmtUSD(l.week.projected) : '' }}
                </span>
                <span class="text-[11px] text-muted-foreground tabular">Previous week {{ fmtUSD(l.prevWeek) }} · largest week {{ fmtUSD(l.maxWeek) }}</span>
              </div>
            </div>
          </div>
        } @empty { <p class="m-0 text-sm text-muted-foreground">No sources.</p> }
      </section>

      <sn-settings-group title="Alerts" description="Alerts always show inside the app. Turn on system notifications to see them while the app is in the background.">
        @if (alerts(); as a) {
          <sn-settings-row title="System notifications" [subtitle]="notifyHint()">
            <hlm-switch [checked]="a.notify" aria-label="System notifications" (checkedChange)="patchAlerts({ notify: $event })" />
          </sn-settings-row>
          <sn-settings-row title="Warn at" subtitle="% of a budget or limit">
            <sn-number-box style="inline-size: 9rem" ariaLabel="Warn at percent" [min]="1" [max]="100" [step]="5" [decimals]="0" [value]="a.warnPct" (valueChange)="patchAlerts({ warnPct: $event ?? 80 })" />
          </sn-settings-row>
          <sn-settings-row title="Expensive session" subtitle="Alert when one session's list price passes this (USD, 0 = off)">
            <sn-number-box style="inline-size: 9rem" ariaLabel="Session alert in USD" [min]="0" [step]="1" [largeStep]="10" [decimals]="2" [value]="a.sessionUSD" (valueChange)="patchAlerts({ sessionUSD: $event ?? 0 })" />
          </sn-settings-row>
        }
      </sn-settings-group>

      @if (recent().length) {
        <section hlmCard data-size="sm" class="content-start gap-2 px-4">
          <h3 class="m-0 text-[13.5px] font-semibold">Recent alerts</h3>
          @for (a of recent(); track a.at + a.id) {
            <div class="grid gap-0.5 border-l-[3px] pl-2.5 text-[12.5px]" [style.border-left-color]="a.level === 'over' ? 'var(--crit)' : 'var(--warn)'">
              <span><b>{{ a.title }}</b> <span class="text-xs text-muted-foreground">{{ fmtWhen(a.at) }}</span></span>
              <span class="text-muted-foreground">{{ a.body }}</span>
            </div>
          }
        </section>
      }
    </div>

    <sn-modal-sheet [title]="draft()?.id ? 'Edit budget' : 'Add budget'" [open]="!!draft()" (openChange)="!$event && draft.set(null)">
      @if (draft(); as d) {
        <div class="grid gap-3 text-[13px]">
          <label class="grid grid-cols-[120px_minmax(0,1fr)] items-center gap-2.5">Name
            <input hlmInput class="h-9" placeholder="e.g. Client X, monthly" [value]="d.name" (input)="patch({ name: $any($event.target).value })" /></label>
          <label class="grid grid-cols-[120px_minmax(0,1fr)] items-center gap-2.5">Source
            <select hlmInput class="h-9" (change)="patch({ sourceId: $any($event.target).value })">
              <option value="" [selected]="!d.sourceId">All sources</option>
              @for (s of state.sources(); track s.id) { <option [value]="s.id" [selected]="d.sourceId === s.id">{{ s.name }} · {{ AGENTS[s.agent] ?? s.agent }}</option> }
            </select></label>
          <label class="grid grid-cols-[120px_minmax(0,1fr)] items-center gap-2.5">Project
            <select hlmInput class="h-9" (change)="patch({ projectKey: $any($event.target).value })">
              <option value="" [selected]="!d.projectKey">Every project</option>
              @for (p of state.projects(); track p.key) { <option [value]="p.key" [selected]="d.projectKey === p.key">{{ p.name }} · {{ p.path }}</option> }
              @if (d.projectKey && !hasProject(d.projectKey)) { <option [value]="d.projectKey" selected>{{ d.projectKey }}</option> }
            </select></label>
          <label class="grid grid-cols-[120px_minmax(0,1fr)] items-center gap-2.5">Period
            <select hlmInput class="h-9" (change)="patch({ period: $any($event.target).value })">
              <option value="day" [selected]="d.period === 'day'">Day</option>
              <option value="week" [selected]="d.period === 'week'">Week (Monday to Sunday)</option>
              <option value="month" [selected]="d.period === 'month'">Calendar month</option>
            </select></label>
          <div class="grid grid-cols-[120px_minmax(0,1fr)] items-center gap-2.5"><span>Limit (USD)</span>
            <sn-number-box ariaLabel="Limit in USD" [min]="0" [step]="5" [largeStep]="50" [decimals]="2" [value]="d.limitUSD" (valueChange)="patch({ limitUSD: $event ?? 0 })" /></div>
          <label class="grid grid-cols-[120px_minmax(0,1fr)] items-center gap-2.5">Count
            <select hlmInput class="h-9" (change)="patch({ basis: $any($event.target).value })">
              <option value="list" [selected]="d.basis !== 'paid'">List price (API-equivalent)</option>
              <option value="paid" [selected]="d.basis === 'paid'">What you pay (by each source's billing)</option>
            </select></label>
          <div class="grid grid-cols-[120px_minmax(0,1fr)] items-center gap-2.5"><span>Alerts</span>
            <span class="flex items-center gap-2"><hlm-switch [checked]="d.notify" aria-label="Alert for this budget" (checkedChange)="patch({ notify: $event })" />
              <span class="text-xs text-muted-foreground">at the warning level and when it is used up</span></span></div>
          @if (sheetErr()) { <p class="m-0 text-xs text-[var(--crit)]">{{ sheetErr() }}</p> }
        </div>
      }
      <button snSheetCancel hlmBtn variant="outline">Cancel</button>
      <button snSheetPrimary hlmBtn [disabled]="!draft()?.limitUSD" (click)="saveDraft()">Save</button>
    </sn-modal-sheet>

    <sn-modal-sheet [title]="'Usage limits for ' + (limitDraft()?.name ?? '')" [open]="!!limitDraft()" (openChange)="!$event && limitDraft.set(null)">
      @if (limitDraft(); as l) {
        <p class="m-0 mb-3 text-[13px] text-muted-foreground">In API list price. Leave at 0 to only track usage. Largest 5-hour window so far: {{ fmtUSD(l.maxBlock) }}; largest week: {{ fmtUSD(l.maxWeek) }}.</p>
        <div class="grid gap-3 text-[13px]">
          <div class="grid grid-cols-[150px_minmax(0,1fr)] items-center gap-2.5"><span>Per 5-hour window</span>
            <sn-number-box ariaLabel="Limit per 5-hour window in USD" [min]="0" [step]="5" [largeStep]="50" [decimals]="2" [value]="l.billing.limit5hUSD" (valueChange)="patchLimit({ limit5hUSD: $event ?? 0 })" /></div>
          <div class="grid grid-cols-[150px_minmax(0,1fr)] items-center gap-2.5"><span>Per week</span>
            <sn-number-box ariaLabel="Limit per week in USD" [min]="0" [step]="10" [largeStep]="100" [decimals]="2" [value]="l.billing.limitWeekUSD" (valueChange)="patchLimit({ limitWeekUSD: $event ?? 0 })" /></div>
          <label class="grid grid-cols-[150px_minmax(0,1fr)] items-center gap-2.5">Week resets
            <select hlmInput class="h-9" (change)="patchLimit({ weekResetDay: +$any($event.target).value })">
              @for (w of WEEKDAYS; track $index) { <option [value]="$index" [selected]="(l.billing.weekResetDay || 0) === $index">{{ $index ? w + ' 00:00' : w }}</option> }
            </select></label>
        </div>
      }
      <button snSheetCancel hlmBtn variant="outline">Cancel</button>
      <button snSheetPrimary hlmBtn (click)="saveLimits()">Save</button>
    </sn-modal-sheet>
  `,
})
export class Budgets {
  protected readonly state = inject(AppState);
  protected readonly fmtUSD = fmtUSD;
  protected readonly fmtDay = fmtDay;
  protected readonly fmtWhen = fmtWhen;
  protected readonly time = time;
  protected readonly AGENTS = AGENTS;
  protected readonly TONE = TONE;
  protected readonly WEEKDAYS = WEEKDAYS;
  readonly budgets = signal<core.Budget[]>([]);
  readonly status = signal<core.BudgetStatus[]>([]);
  readonly limits = signal<core.SourceLimits[]>([]);
  readonly alerts = signal<core.AlertSettings | null>(null);
  readonly recent = signal<core.Alert[]>([]);
  readonly notifyOK = signal<boolean | null>(null);
  readonly draft = signal<core.Budget | null>(null);
  readonly sheetErr = signal('');
  readonly limitDraft = signal<(core.SourceLimits & { billing: core.Billing }) | null>(null);
  private saveTimer: ReturnType<typeof setTimeout> | undefined;

  constructor() {
    void NotifyService.Available().then((ok) => this.notifyOK.set(ok));
    effect(() => {
      this.state.scan()?.last; // refresh after rescans
      this.state.sources(); // billing edits
      this.state.toasts(); // a new alert
      untracked(() => void this.load());
    });
  }

  readonly notifyHint = computed(() =>
    this.notifyOK() === false ? 'Not available in this build (on macOS the app must be a signed .app bundle)' : 'Budgets, limits and the expensive-session alert',
  );

  async load(): Promise<void> {
    const [b, s, l, a, r] = await Promise.all([BudgetService.Budgets(), BudgetService.Status(), BudgetService.Limits(), BudgetService.AlertSettings(), BudgetService.RecentAlerts()]);
    this.budgets.set(b ?? []);
    this.status.set(s ?? []);
    this.limits.set(l ?? []);
    if (!this.saveTimer) this.alerts.set(a);
    this.recent.set(r ?? []);
  }

  periodName(p: string): string {
    return p === 'day' ? 'Daily' : p === 'week' ? 'Weekly' : 'Monthly';
  }
  lastDay(end: string): string {
    const d = new Date(end + 'T00:00:00');
    d.setDate(d.getDate() - 1);
    return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`;
  }
  hasProject(k: string): boolean {
    return this.state.projects().some((p) => p.key === k);
  }
  billingLabel(b: core.Billing): string {
    switch (b.kind) {
      case 'subscription':
      case 'team':
        return `${b.kind === 'team' ? 'Team seat' : 'Subscription'}${b.plan ? ' ' + b.plan : ''}`;
      case 'api':
        return 'API key (usage limits rarely apply)';
      case 'free':
        return 'Free tier';
      default:
        return 'Billing not set';
    }
  }
  barPct(v: number, l: core.SourceLimits): number {
    const top = Math.max(l.maxBlock, l.billing.limit5hUSD || 0, 0.0001);
    return Math.max(2, (v / top) * 100);
  }

  edit(b: core.Budget | null): void {
    this.sheetErr.set('');
    this.draft.set(
      b ? { ...b } : { id: '', name: '', sourceId: '', projectKey: this.state.projectKey() ?? '', period: 'month', limitUSD: 100, basis: 'list', notify: true },
    );
  }
  patch(p: Partial<core.Budget>): void {
    const d = this.draft();
    if (d) this.draft.set({ ...d, ...p });
  }
  async saveDraft(): Promise<void> {
    const d = this.draft();
    if (!d) return;
    const next = d.id ? this.budgets().map((b) => (b.id === d.id ? d : b)) : [...this.budgets(), d];
    try {
      await BudgetService.SaveBudgets(next);
      this.draft.set(null);
      await this.load();
    } catch (e) {
      this.sheetErr.set(errText(e));
    }
  }
  async remove(b: core.Budget): Promise<void> {
    await BudgetService.SaveBudgets(this.budgets().filter((x) => x.id !== b.id));
    await this.load();
  }

  patchAlerts(p: Partial<core.AlertSettings>): void {
    const a = this.alerts();
    if (!a) return;
    const next = { ...a, ...p };
    this.alerts.set(next);
    clearTimeout(this.saveTimer);
    this.saveTimer = setTimeout(async () => {
      this.saveTimer = undefined;
      try {
        const allowed = await BudgetService.SaveAlertSettings(next);
        if (next.notify && p.notify && !allowed) this.state.toast('System notifications are not allowed', 'Alerts will still show inside the app. Check the notification settings of your system.', 'warn');
      } catch (e) {
        this.state.toast('Could not save the alert settings', errText(e), 'error');
      }
    }, 400);
  }

  editLimits(l: core.SourceLimits): void {
    this.limitDraft.set({ ...l, billing: { ...l.billing } });
  }
  patchLimit(p: Partial<core.Billing>): void {
    const l = this.limitDraft();
    if (l) this.limitDraft.set({ ...l, billing: { ...l.billing, ...p } });
  }
  async saveLimits(): Promise<void> {
    const l = this.limitDraft();
    const src = this.state.sources().find((s) => s.id === l?.sourceId);
    if (!l || !src) return;
    try {
      await SourceService.Update({ id: src.id, name: src.name, agent: src.agent, homeDir: src.homeDir, billing: l.billing });
      this.limitDraft.set(null);
      this.state.sources.set((await SourceService.List()) ?? []);
      await this.load();
    } catch (e) {
      this.state.toast('Could not save the limits', errText(e), 'error');
    }
  }
}
