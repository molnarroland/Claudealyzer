import { ChangeDetectionStrategy, Component, computed, inject, input, signal } from '@angular/core';
import { NgIcon } from '@ng-icons/core';
import { SnSidebarNavImports, SnTooltip, SnWindowControls } from '@spartan-native/desktop-ui';
import { HlmButtonImports } from '@spartan-ng/helm/button';
import { HlmInputImports } from '@spartan-ng/helm/input';
import { HlmProgressImports } from '@spartan-ng/helm/progress';
import { AGENTS, fmtUSD } from '../lib/format';
import { AppState, VIEWS } from '../lib/state';
import { Segmented } from './shared';

/** Source picker and agent filter: they scope every view, so they live in the window's title bar. */
@Component({
  selector: 'cz-scope-filters',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [HlmInputImports, Segmented],
  host: { class: 'flex items-center gap-2' },
  template: `
    <select hlmInput class="h-7 w-52 text-[13px]" aria-label="Source" (change)="state.selectSource($any($event.target).value)">
      <option value="all" [selected]="state.sourceId() === 'all'">All sources · {{ state.sources().length }} folders</option>
      @for (s of state.sources(); track s.id) {
        <option [value]="s.id" [selected]="state.sourceId() === s.id">{{ s.name }} · {{ AGENTS[s.agent] ?? s.agent }} · {{ s.dirShort }}</option>
      }
    </select>
    @if (agents().length > 2) {
      <cz-segmented label="Agent filter" [options]="agents()" [value]="state.agent()" (valueChange)="state.selectAgent($event)" />
    }
  `,
})
export class ScopeFilters {
  protected readonly state = inject(AppState);
  protected readonly AGENTS = AGENTS;
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
    @if (state.view() === 'history') {
      <span role="toolbar" aria-label="Session commands" class="flex items-center gap-0.5">
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
}

/**
 * App navigation as one sidebar (the macOS 26 skin draws it as a single inset glass panel):
 * the views, then the projects with their search, then settings at the foot.
 */
@Component({
  selector: 'cz-sidebar',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [SnSidebarNavImports, SnTooltip, SnWindowControls, HlmInputImports, HlmProgressImports],
  host: { class: 'flex min-h-0' },
  template: `
    <!-- Only the project list scrolls: views, the search field and settings stay put. -->
    <sn-sidebar-nav aria-label="Navigation" class="sn-sidebar min-h-0" style="overflow: hidden">
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
        <input hlmInput type="search" class="mx-1 mb-1 h-7 w-[calc(100%-0.5rem)] shrink-0 text-[13px]" placeholder="Search projects" aria-label="Search projects"
          [value]="q()" (input)="q.set($any($event.target).value)" />
        <div class="grid min-h-0 flex-1 grid-cols-[minmax(0,1fr)] content-start gap-0.5 overflow-x-hidden overflow-y-auto scroll-thin" role="presentation">
        @for (p of filtered(); track p.key) {
          <button snSidebarItem [active]="p.key === state.projectKey()" [badge]="p.unpriced && !p.costUSD ? '—' : fmtUSD(p.costUSD)"
            [attr.aria-current]="p.key === state.projectKey() ? 'true' : null" [snTooltip]="projectTip(p)" (click)="state.selectProject(p.key)">
            <span class="truncate">{{ p.name }}</span>
          </button>
        } @empty {
          <p class="m-0 px-2 py-1 text-[13px] text-muted-foreground">
            @if (state.scan()?.running) { Reading history… } @else if (q()) { No project matches. } @else { No sessions yet. Add a source in Settings. }
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
      </sn-sidebar-section>
    </sn-sidebar-nav>
  `,
})
export class Sidebar {
  protected readonly state = inject(AppState);
  /** Mac look only: the sidebar's top strip holds the traffic lights (the OS's own, or drawn). */
  readonly windowChrome = input<'none' | 'native' | 'drawn'>('none');
  protected readonly fmtUSD = fmtUSD;
  protected readonly views = VIEWS.filter((v) => v.id !== 'settings');
  protected readonly settings = VIEWS.find((v) => v.id === 'settings')!;
  readonly q = signal('');

  readonly filtered = computed(() => {
    const q = this.q().toLowerCase();
    return this.state.projects().filter((p) => !q || p.name.toLowerCase().includes(q) || p.path.toLowerCase().includes(q));
  });

  projectTip(p: { path: string; sessionCount: number; agents: string[] | null }): string {
    const agents = (p.agents ?? []).map((a) => AGENTS[a] ?? a).join(', ');
    return `${p.path}\n${p.sessionCount} session${p.sessionCount === 1 ? '' : 's'}${agents ? ' · ' + agents : ''}`;
  }
}
