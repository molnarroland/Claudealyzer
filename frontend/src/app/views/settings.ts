import { ChangeDetectionStrategy, Component, computed, effect, inject, signal, untracked } from '@angular/core';
import { SnCommandBarImports, SnContextMenu, SnMenuItem, SnModalSheet, SnNumberBox, SnSettingsImports } from '@spartan-native/desktop-ui';
import { SnDialogActionsImports, SnSelectableImports } from '@spartan-native/helm';
import { AppearanceState, NATIVE_HOST, SCHEME_LABELS, SKIN_OPTIONS, SpartanNativeTheme, TEXT_SCALE_OPTIONS } from '@spartan-native/theme';
import { HlmBadgeImports } from '@spartan-ng/helm/badge';
import { HlmButtonImports } from '@spartan-ng/helm/button';
import { HlmCardImports } from '@spartan-ng/helm/card';
import { HlmInputImports } from '@spartan-ng/helm/input';
import { HlmLabelImports } from '@spartan-ng/helm/label';
import { HlmTableImports } from '@spartan-ng/helm/table';
import { InsightService, PricingService, SourceService, ad, core, ins, m, pr } from '../lib/api';
import { AGENTS, errText, fmtUSD } from '../lib/format';
import { AppState } from '../lib/state';
import { AgentTag, TONE } from './shared';

type ThresholdKey = keyof ins.Thresholds;
type NumKey = 'input' | 'cacheWrite5m' | 'cacheWrite1h' | 'cacheRead' | 'output';
const SYSTEM = 'system';

@Component({
  selector: 'cz-settings',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [
    SnCommandBarImports,
    SnSettingsImports,
    SnNumberBox,
    SnModalSheet,
    SnContextMenu,
    SnSelectableImports,
    SnDialogActionsImports,
    HlmButtonImports,
    HlmBadgeImports,
    HlmCardImports,
    HlmInputImports,
    HlmLabelImports,
    HlmTableImports,
    AgentTag,
  ],
  host: { class: 'flex min-h-0 flex-col' },
  template: `
    <sn-command-bar title="Sources & settings" titlePosition="start" class="border-b">
      @if (msg()) {
        <span snCommandBarEnd role="status" class="pe-2 text-xs" [class.text-[var(--crit)]]="msgErr()" [class.text-muted-foreground]="!msgErr()">{{ msg() }}</span>
      }
      <button snCommand label="Rescan" icon="lucideRefreshCw" [priority]="1" (click)="rescan()"></button>
      <button snCommand label="Detect automatically" icon="lucideFolderSearch" [priority]="0" (click)="detect()"></button>
      <button snCommand label="Add source" icon="lucidePlus" variant="accent" [priority]="2" (click)="openAdd()"></button>
    </sn-command-bar>

    <div class="grid min-h-0 flex-1 auto-rows-max content-start gap-5 overflow-auto px-7 pt-4 pb-10 scroll-thin">
      <section hlmCard data-size="sm" class="content-start gap-2.5 px-4">
        <h3 class="m-0 text-[13.5px] font-semibold">Sources</h3>
        <p class="m-0 text-xs text-muted-foreground">Folders the app reads. It only reads them and never changes anything inside, and nothing is sent anywhere. Right-click a source for its actions.</p>
        <div hlmTableContainer class="rounded-lg border">
          <table hlmTable snSelectable="grid" [multiple]="false" [(selection)]="picked" (activated)="editBillingId($event)" aria-label="Sources" class="text-[12.5px]">
            <thead hlmTHead><tr hlmTr><th hlmTh>Name</th><th hlmTh>Agent</th><th hlmTh>Folder</th><th hlmTh>Billing</th><th hlmTh class="text-right">Sessions</th><th hlmTh class="text-right">List price</th><th hlmTh>Status</th><th hlmTh><span class="sr-only">Actions</span></th></tr></thead>
            <tbody hlmTBody>
              @for (s of state.sources(); track s.id) {
                <tr hlmTr snSelectableItem [value]="s.id" [snSelectableLabel]="s.name" [snContextMenu]="sourceMenu(s)">
                  <td hlmTd>{{ s.name }}</td>
                  <td hlmTd><cz-agent [agent]="s.agent" /></td>
                  <td hlmTd class="mono">{{ s.dirShort }}</td>
                  <td hlmTd class="whitespace-nowrap">{{ billingLabel(s.billing) }}</td>
                  <td hlmTd class="text-right tabular">{{ s.sessions }}</td>
                  <td hlmTd class="text-right tabular">{{ fmtUSD(s.costUSD) }}</td>
                  <td hlmTd>
                    @if (!s.ok) { <span hlmBadge [class]="TONE['high']">Not found</span> }
                    @else if (s.warnings) { <span hlmBadge [class]="TONE['medium']">Parsed with warnings</span> }
                    @else { <span hlmBadge [class]="TONE['ok']">Parsed</span> }
                    <span class="sub ml-1 text-xs text-muted-foreground">{{ s.version || s.detection }}{{ s.warnings ? ' · ' + s.warnings + ' sessions with warnings' : '' }}</span>
                  </td>
                  <td hlmTd class="whitespace-nowrap text-right">
                    <button hlmBtn variant="outline" size="xs" (click)="$event.stopPropagation(); editBilling(s)">Billing…</button>
                    <button hlmBtn variant="ghost" size="xs" (click)="$event.stopPropagation(); askRemove(s)">Remove…</button>
                  </td>
                </tr>
              }
              @for (d of extra(); track d.dir + d.agent) {
                <tr hlmTr class="text-muted-foreground">
                  <td hlmTd>{{ d.name }}</td><td hlmTd><cz-agent [agent]="d.agent" /></td><td hlmTd class="mono">{{ d.dir }}</td><td hlmTd>—</td><td hlmTd></td><td hlmTd></td>
                  <td hlmTd><span hlmBadge variant="secondary">{{ d.supported ? (d.ok ? 'Detected, not added' : 'Nothing to read') : 'Not supported yet' }}</span> <span class="text-xs">{{ d.message }}</span></td>
                  <td hlmTd class="text-right">@if (d.supported && d.ok) { <button hlmBtn variant="outline" size="xs" (click)="addDetected(d)">Add</button> }</td>
                </tr>
              }
            </tbody>
          </table>
        </div>
      </section>

      <sn-settings-group title="Appearance" description="The app follows your operating system. Pick a different look, scheme or text size here; your choice is kept between launches.">
        <sn-settings-row title="Look" subtitle="Which operating system's controls the app is drawn with">
          <select hlmInput class="h-8 w-48 text-[13px]" aria-label="Look" (change)="setAppearance('skin', $any($event.target).value)">
            <option [value]="SYSTEM" [selected]="skinValue() === SYSTEM">System ({{ skinLabel(theme.nativeSkin()) }})</option>
            @for (o of skins; track o.value) { <option [value]="o.value" [selected]="skinValue() === o.value">{{ o.label }}</option> }
          </select>
        </sn-settings-row>
        <sn-settings-row title="Color scheme">
          <select hlmInput class="h-8 w-48 text-[13px]" aria-label="Color scheme" (change)="setAppearance('scheme', $any($event.target).value)">
            <option [value]="SYSTEM" [selected]="schemeValue() === SYSTEM">System</option>
            <option value="light" [selected]="schemeValue() === 'light'">{{ SCHEME_LABELS.light }}</option>
            <option value="dark" [selected]="schemeValue() === 'dark'">{{ SCHEME_LABELS.dark }}</option>
          </select>
        </sn-settings-row>
        <sn-settings-row title="Text size">
          <select hlmInput class="h-8 w-48 text-[13px]" aria-label="Text size" (change)="setAppearance('textScale', $any($event.target).value)">
            <option [value]="SYSTEM" [selected]="scaleValue() === SYSTEM">System</option>
            @for (o of scales; track o.value) { <option [value]="'' + o.value" [selected]="scaleValue() === '' + o.value">{{ o.label }}</option> }
          </select>
        </sn-settings-row>
        @if (!theme.followsSystem()) {
          <sn-settings-row title="Reset appearance" subtitle="Follow the operating system again">
            <button hlmBtn variant="outline" size="sm" (click)="theme.followSystem()">Follow system</button>
          </sn-settings-row>
        }
      </sn-settings-group>

      <section hlmCard data-size="sm" class="content-start gap-2.5 px-4">
        <div class="flex items-center justify-between"><h3 class="m-0 text-[13.5px] font-semibold">Pricing</h3>
          <span class="flex gap-2" snDialogButtons>
            <button hlmBtn variant="outline" size="sm" (click)="resetPricing()">Reset to defaults</button>
            <button hlmBtn size="sm" (click)="savePricing()">Save pricing</button>
          </span></div>
        <p class="m-0 text-xs text-muted-foreground">USD per million tokens. "Match" is a model-id prefix (longest match wins). Rows with the same match but a later "from" date take over on that day, so old sessions keep the rates valid on their day. Saving re-prices all history.</p>
        <div hlmTableContainer class="max-h-96 overflow-auto rounded-lg border scroll-thin">
          <table hlmTable class="text-xs">
            <thead hlmTHead><tr hlmTr><th hlmTh>Match</th><th hlmTh>From</th><th hlmTh class="text-right">Input</th><th hlmTh class="text-right">Write 5m</th><th hlmTh class="text-right">Write 1h</th><th hlmTh class="text-right">Read</th><th hlmTh class="text-right">Output</th><th hlmTh>Source</th><th hlmTh><span class="sr-only">Remove</span></th></tr></thead>
            <tbody hlmTBody>
              @for (r of rates(); track $index; let i = $index) {
                <tr hlmTr>
                  <td hlmTd><input hlmInput class="mono h-7 w-36 text-xs" aria-label="Model prefix" [value]="r.match" (change)="setRate(i, 'match', $any($event.target).value)" /></td>
                  <td hlmTd><input hlmInput type="date" class="h-7 w-32 text-xs" aria-label="Valid from" [value]="r.from" (change)="setRate(i, 'from', $any($event.target).value)" /></td>
                  @for (k of numKeys; track k) {
                    <td hlmTd class="text-right">
                      <sn-number-box style="inline-size: 5.5rem" [ariaLabel]="k + ' rate'" [spinButtons]="false" [min]="0" [decimals]="2" [step]="0.01" [value]="r[k]" (valueChange)="setRateNum(i, k, $event)" />
                    </td>
                  }
                  <td hlmTd class="text-muted-foreground" [class.text-[var(--warn)]]="r.source === 'placeholder'">{{ r.source }}</td>
                  <td hlmTd><button hlmBtn variant="ghost" size="icon-xs" aria-label="Remove rate" (click)="removeRate(i)">×</button></td>
                </tr>
              }
            </tbody>
          </table>
        </div>
        <div class="flex items-center justify-between gap-3">
          <button hlmBtn variant="outline" size="xs" (click)="addRate()">Add rate</button>
          <label class="flex items-center gap-2 text-[13px]">Price history with
            <select hlmInput class="h-8 w-60 text-[13px]" [value]="mode()" (change)="mode.set($any($event.target).value)">
              <option value="onday">Rates valid on the day (default)</option>
              <option value="today">Today's rates</option>
            </select>
          </label>
        </div>
      </section>

      <sn-settings-group title="Insight thresholds" description="When a session crosses one of these, it gets a finding. Saving re-runs the insights on all history.">
        @if (th(); as t) {
          @for (f of thFields; track f.key) {
            <sn-settings-row [title]="f.label" [subtitle]="f.unit">
              <sn-number-box style="inline-size: 9rem" [ariaLabel]="f.label" [min]="0" [step]="f.step" [largeStep]="f.step * 10" [value]="t[f.key]" (valueChange)="setTh(f.key, $event)" />
            </sn-settings-row>
          }
          <sn-settings-row title="Apply thresholds">
            <span class="flex gap-2" snDialogButtons>
              <button hlmBtn variant="outline" size="sm" (click)="resetThresholds()">Defaults</button>
              <button hlmBtn size="sm" snDefaultAction (click)="saveThresholds()">Save</button>
            </span>
          </sn-settings-row>
        }
      </sn-settings-group>
    </div>

    <sn-modal-sheet title="Add source" [open]="addOpen()" (openChange)="addOpen.set($event)">
      <p class="m-0 mb-3 text-[13px] text-muted-foreground">Choose the agent and the folder where it keeps its history.</p>
      <div class="grid gap-3 text-[13px]">
        <label class="grid grid-cols-[110px_minmax(0,1fr)] items-center gap-2.5">Agent
          <select hlmInput class="h-9" [value]="newAgent()" (change)="newAgent.set($any($event.target).value); check()">
            <option value="claude">Claude Code</option><option value="codex">Codex</option><option value="gemini">Gemini CLI</option>
          </select></label>
        <label class="grid grid-cols-[110px_minmax(0,1fr)] items-center gap-2.5">Name
          <input hlmInput class="h-9" placeholder="e.g. claude-client-x" [value]="newName()" (input)="newName.set($any($event.target).value)" /></label>
        <div class="grid grid-cols-[110px_minmax(0,1fr)] items-center gap-2.5"><span id="cz-folder">Folder</span>
          <div class="flex gap-2"><input hlmInput class="mono h-9 min-w-0 flex-1" aria-labelledby="cz-folder" placeholder="~/.claude-client-x" [value]="newDir()" (change)="newDir.set($any($event.target).value); check()" />
            <button hlmBtn variant="outline" type="button" (click)="browse()">Browse…</button></div></div>
        @if (det(); as d) {
          <div class="grid gap-0.5 rounded-lg border bg-muted px-3 py-2 text-[12.5px]"><b>{{ d.ok ? d.message : 'Nothing found' }}</b>
            <span class="text-muted-foreground">{{ d.ok ? 'Detected as ' + AGENTS[newAgent()] + '.' : d.message }}</span></div>
        }
        @if (addErr()) { <p class="m-0 text-xs text-[var(--crit)]">{{ addErr() }}</p> }
      </div>
      <button snSheetCancel hlmBtn variant="outline">Cancel</button>
      <button snSheetPrimary hlmBtn [disabled]="!newDir()" (click)="add()">Add source</button>
    </sn-modal-sheet>

    <sn-modal-sheet [title]="'Billing for ' + (billing()?.name ?? '')" [open]="!!billing()" (openChange)="!$event && billing.set(null)">
      @if (billing(); as b) {
        <p class="m-0 mb-3 text-[13px] text-muted-foreground">Used to turn the API list price into what you actually pay. Plan prices are entered by you, never guessed.</p>
        <div class="grid gap-3 text-[13px]">
          <label class="grid grid-cols-[130px_minmax(0,1fr)] items-center gap-2.5">Billing type
            <select hlmInput class="h-9" [value]="b.billing.kind" (change)="patchBilling('kind', $any($event.target).value)">
              <option value="unset">Not set</option><option value="api">API key (per token)</option><option value="subscription">Subscription</option><option value="team">Team seat</option><option value="free">Free tier</option>
            </select></label>
          @if (b.billing.kind === 'subscription' || b.billing.kind === 'team') {
            <label class="grid grid-cols-[130px_minmax(0,1fr)] items-center gap-2.5">Plan name
              <input hlmInput class="h-9" placeholder="e.g. Max" [value]="b.billing.plan" (input)="patchBilling('plan', $any($event.target).value)" /></label>
            <div class="grid grid-cols-[130px_minmax(0,1fr)] items-center gap-2.5"><span>Monthly fee (USD)</span>
              <sn-number-box ariaLabel="Monthly fee in USD" [min]="0" [step]="1" [largeStep]="10" [decimals]="2" [value]="b.billing.fee" (valueChange)="patchBilling('fee', $event ?? 0)" /></div>
            <div class="grid grid-cols-[130px_minmax(0,1fr)] items-center gap-2.5"><span>Renews on day</span>
              <sn-number-box ariaLabel="Renews on day of month" [min]="1" [max]="28" [step]="1" [decimals]="0" [value]="b.billing.renewDay || 1" (valueChange)="patchBilling('renewDay', $event ?? 1)" /></div>
          }
          @if (b.billing.kind === 'api') {
            <div class="grid grid-cols-[130px_minmax(0,1fr)] items-center gap-2.5"><span>Discount (%)</span>
              <sn-number-box ariaLabel="Discount in percent" [min]="0" [max]="100" [step]="1" [largeStep]="10" [decimals]="1" [value]="b.billing.discountPct" (valueChange)="patchBilling('discountPct', $event ?? 0)" /></div>
          }
        </div>
      }
      <button snSheetCancel hlmBtn variant="outline">Cancel</button>
      <button snSheetPrimary hlmBtn (click)="saveBilling()">Save</button>
    </sn-modal-sheet>

    <sn-modal-sheet role="alertdialog" [title]="'Stop reading ' + (removing()?.dirShort ?? '') + '?'" [open]="!!removing()" (openChange)="!$event && removing.set(null)">
      <p class="m-0 text-[13px]">The source is removed from the app. Nothing in the folder is deleted.</p>
      <button snSheetCancel hlmBtn variant="outline">Cancel</button>
      <button snSheetPrimary hlmBtn variant="destructive" (click)="remove(removing()!)">Remove</button>
    </sn-modal-sheet>
  `,
})
export class Settings {
  protected readonly state = inject(AppState);
  protected readonly theme = inject(SpartanNativeTheme);
  private readonly host = inject(NATIVE_HOST);
  protected readonly fmtUSD = fmtUSD;
  protected readonly AGENTS = AGENTS;
  protected readonly TONE = TONE;
  protected readonly SYSTEM = SYSTEM;
  protected readonly SCHEME_LABELS = SCHEME_LABELS;
  protected readonly skins = SKIN_OPTIONS;
  protected readonly scales = TEXT_SCALE_OPTIONS;
  readonly picked = signal<readonly string[]>([]);
  readonly detected = signal<core.DetectedSource[]>([]);
  readonly extra = computed(() => this.detected().filter((d) => !d.added));
  readonly rates = signal<pr.Rate[]>([]);
  readonly table = signal<pr.Table | null>(null);
  readonly mode = signal('onday');
  readonly th = signal<ins.Thresholds | null>(null);
  readonly msg = signal('');
  readonly msgErr = signal(false);
  readonly addOpen = signal(false);
  readonly newAgent = signal<string>('claude');
  readonly newName = signal('');
  readonly newDir = signal('');
  readonly det = signal<ad.Detection | null>(null);
  readonly addErr = signal('');
  readonly billing = signal<core.Source | null>(null);
  readonly removing = signal<core.SourceInfo | null>(null);
  protected readonly numKeys: readonly NumKey[] = ['input', 'cacheWrite5m', 'cacheWrite1h', 'cacheRead', 'output'];
  protected readonly thFields: { key: ThresholdKey; label: string; unit: string; step: number }[] = [
    { key: 'contextTokens', label: 'Context too large', unit: 'tokens per call', step: 1000 },
    { key: 'longTurns', label: 'Long session', unit: 'turns without compaction', step: 1 },
    { key: 'slowRunSec', label: 'Slow test or build', unit: 'seconds per turn', step: 1 },
    { key: 'filesStep', label: 'Many files read', unit: 'reads per turn', step: 1 },
    { key: 'filesSession', label: 'Too many files', unit: 'distinct per session', step: 1 },
    { key: 'toolOutput', label: 'Large tool output', unit: 'tokens per result', step: 1000 },
    { key: 'heavyPct', label: 'CPU-heavy', unit: '% of the turn', step: 1 },
    { key: 'cacheChurn', label: 'Cache churn', unit: 'write ÷ read', step: 0.1 },
    { key: 'subagentPct', label: 'Expensive subagents', unit: '% of session', step: 1 },
    { key: 'subagentFanout', label: 'Subagent fan-out', unit: 'per turn', step: 1 },
    { key: 'smallOutput', label: 'Small output', unit: 'tokens (model choice)', step: 10 },
    { key: 'failureStreak', label: 'Failures in a row', unit: 'same tool', step: 1 },
  ];

  // ---------- appearance ----------
  readonly skinValue = computed(() => this.theme.overrides().skin ?? SYSTEM);
  readonly schemeValue = computed(() => this.theme.overrides().scheme ?? SYSTEM);
  readonly scaleValue = computed(() => String(this.theme.overrides().textScale ?? SYSTEM));

  skinLabel(skin: string): string {
    return SKIN_OPTIONS.find((o) => o.value === skin)?.label ?? 'Web';
  }

  /** "System" drops that one override and keeps the others. */
  setAppearance(field: 'skin' | 'scheme' | 'textScale', raw: string): void {
    const rest: Partial<AppearanceState> = { ...this.theme.overrides() };
    delete rest[field];
    if (raw === SYSTEM) {
      this.theme.followSystem();
      if (Object.keys(rest).length) this.theme.override(rest);
      return;
    }
    this.theme.override({ ...rest, [field]: field === 'textScale' ? +raw : raw });
  }

  constructor() {
    void this.load();
    // File → Add source… (from anywhere) lands here and opens the sheet.
    let seen = untracked(this.state.addSourceRequest);
    effect(() => {
      const n = this.state.addSourceRequest();
      if (n !== seen) {
        seen = n;
        untracked(() => this.openAdd());
      }
    });
    if (seen) queueMicrotask(() => this.openAdd());
  }

  async load(): Promise<void> {
    const p = await PricingService.Get();
    this.table.set(p.table);
    this.rates.set([...(p.table.models ?? [])]);
    this.mode.set(p.mode);
    this.th.set(await InsightService.Thresholds());
    this.detected.set((await SourceService.Detect()) ?? []);
  }

  private note(s: string, err = false): void {
    this.msg.set(s);
    this.msgErr.set(err);
    setTimeout(() => this.msg() === s && this.msg.set(''), 5000);
  }

  billingLabel(b: core.Billing): string {
    switch (b.kind) {
      case 'api':
        return 'API key · per token' + (b.discountPct ? ` · −${b.discountPct}%` : '');
      case 'subscription':
      case 'team':
        return `${b.kind === 'team' ? 'Team seat' : 'Subscription'}${b.plan ? ' ' + b.plan : ''} · $${b.fee}/mo · renews day ${b.renewDay || 1}`;
      case 'free':
        return 'Free tier';
      default:
        return 'Not set';
    }
  }

  sourceMenu(s: core.SourceInfo): SnMenuItem[] {
    return [
      { id: 'billing', label: 'Billing…', action: () => this.editBilling(s) },
      { id: 'rescan', label: 'Rescan', action: () => this.rescan() },
      { type: 'separator' },
      { id: 'remove', label: 'Remove…', action: () => void this.askRemove(s) },
    ];
  }

  rescan(): void {
    void SourceService.Rescan();
    this.note('Rescanning…');
  }
  async detect(): Promise<void> {
    this.detected.set((await SourceService.Detect()) ?? []);
    this.note(`${this.extra().length} folder(s) found that are not added yet.`);
  }
  async addDetected(d: core.DetectedSource): Promise<void> {
    try {
      await SourceService.Add(d.name, d.agent, d.dir);
      await this.afterSourceChange();
    } catch (e) {
      this.note(errText(e), true);
    }
  }

  /** The OS's own alert when the host has one (Wails); the skin's alert sheet otherwise. */
  async askRemove(s: core.SourceInfo): Promise<void> {
    const ask = this.host.dialogs?.message;
    if (!ask) {
      this.removing.set(s);
      return;
    }
    const answer = await ask.call(this.host.dialogs, {
      kind: 'warning',
      title: `Stop reading ${s.dirShort}?`,
      message: 'The source is removed from the app. Nothing in the folder is deleted.',
      buttons: [
        { id: 'remove', label: 'Remove' },
        { id: 'cancel', label: 'Cancel' },
      ],
      defaultId: 'remove',
      cancelId: 'cancel',
    });
    if (answer === 'remove') await this.remove(s);
  }
  async remove(s: core.SourceInfo): Promise<void> {
    this.removing.set(null);
    await SourceService.Remove(s.id);
    if (this.state.sourceId() === s.id) this.state.sourceId.set('all');
    await this.afterSourceChange();
  }
  private async afterSourceChange(): Promise<void> {
    this.state.sources.set((await SourceService.List()) ?? []);
    this.detected.set((await SourceService.Detect()) ?? []);
  }

  openAdd(): void {
    this.newName.set('');
    this.newDir.set('');
    this.det.set(null);
    this.addErr.set('');
    this.addOpen.set(true);
  }
  /** The OS folder picker through the host; the Go service's picker where the host gives no path (browser). */
  async browse(): Promise<void> {
    let dir = '';
    if (this.host.dialogs) {
      const picked = await this.host.dialogs.pickFolder();
      if (!picked) return; // cancelled
      dir = picked.path ?? '';
    }
    if (!dir) dir = await SourceService.PickFolder();
    if (dir) {
      this.newDir.set(dir);
      await this.check();
    }
  }
  async check(): Promise<void> {
    if (!this.newDir()) return;
    this.det.set(await SourceService.Check(this.newAgent() as m.Agent, this.newDir()));
  }
  async add(): Promise<void> {
    try {
      await SourceService.Add(this.newName(), this.newAgent() as m.Agent, this.newDir());
      this.addOpen.set(false);
      await this.afterSourceChange();
      this.note('Source added. Reading its history…');
    } catch (e) {
      this.addErr.set(errText(e));
    }
  }

  editBilling(s: core.SourceInfo): void {
    this.billing.set({ id: s.id, name: s.name, agent: s.agent, homeDir: s.homeDir, billing: { ...s.billing } });
  }
  editBillingId(id: string): void {
    const s = this.state.sources().find((x) => x.id === id);
    if (s) this.editBilling(s);
  }
  patchBilling<K extends keyof core.Billing>(k: K, v: core.Billing[K]): void {
    const b = this.billing();
    if (b) this.billing.set({ ...b, billing: { ...b.billing, [k]: v } });
  }
  async saveBilling(): Promise<void> {
    const b = this.billing();
    if (!b) return;
    try {
      await SourceService.Update(b);
      this.billing.set(null);
      this.state.sources.set((await SourceService.List()) ?? []);
      const c = this.state.current();
      if (c) await this.state.openSession(c.sourceId, c.id, true);
      this.note('Billing saved.');
    } catch (e) {
      this.note(errText(e), true);
    }
  }

  setRate(i: number, k: 'match' | 'from', v: string): void {
    const rs = [...this.rates()];
    rs[i] = { ...rs[i], [k]: v };
    this.rates.set(rs);
  }
  setRateNum(i: number, k: NumKey, v: number | null): void {
    const rs = [...this.rates()];
    if (rs[i][k] === (v ?? 0)) return;
    rs[i] = { ...rs[i], [k]: v ?? 0, source: 'edited' };
    this.rates.set(rs);
  }
  addRate(): void {
    this.rates.set([...this.rates(), { match: 'model-prefix', provider: '', from: new Date().toISOString().slice(0, 10), input: 0, cacheWrite5m: 0, cacheWrite1h: 0, cacheRead: 0, output: 0, source: 'edited' }]);
  }
  removeRate(i: number): void {
    this.rates.set(this.rates().filter((_, j) => j !== i));
  }
  async savePricing(): Promise<void> {
    const t = this.table();
    if (!t) return;
    await PricingService.Save({ table: { ...t, models: this.rates() }, mode: this.mode() });
    this.note('Pricing saved. Re-pricing history…');
  }
  async resetPricing(): Promise<void> {
    await PricingService.Reset();
    await this.load();
    this.note('Pricing reset to the built-in table.');
  }
  setTh(k: ThresholdKey, v: number | null): void {
    const t = this.th();
    if (t && v != null) this.th.set({ ...t, [k]: v });
  }
  async resetThresholds(): Promise<void> {
    this.th.set(await InsightService.DefaultThresholds());
  }
  async saveThresholds(): Promise<void> {
    const t = this.th();
    if (!t) return;
    await InsightService.SaveThresholds(t);
    this.note('Thresholds saved. Re-running insights…');
  }
}
