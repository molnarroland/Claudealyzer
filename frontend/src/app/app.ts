import { NgTemplateOutlet } from '@angular/common';
import { ChangeDetectionStrategy, Component, computed, inject, OnInit } from '@angular/core';
import { SnMenuImports, SnMenuItem, SnStatusBarImports, SnWindowImports } from '@spartan-native/desktop-ui';
import { isWails } from '@spartan-native/host-wails';
import { injectPlatformFamily, injectSkin, skinFamily } from '@spartan-native/theme';
import { SourceService } from './lib/api';
import { fmtUSD } from './lib/format';
import { AppState, VIEWS } from './lib/state';
import { Calendar } from './views/calendar';
import { Analysis, Overview } from './views/analysis';
import { Costs } from './views/costs';
import { History } from './views/history';
import { Settings } from './views/settings';
import { ScopeFilters, Sidebar, ViewCommands } from './views/sidebar';

/**
 * The window. Mac look: a full-height sidebar with the traffic lights in its top strip, and a
 * unified toolbar (the title bar) over the content only. Other looks: a title bar across the
 * window, sidebar and content below. Both end in the status bar.
 * `snSheetHost` lets macOS sheets hang from the title bar.
 */
@Component({
  selector: 'app-root',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [NgTemplateOutlet, SnWindowImports, SnStatusBarImports, SnMenuImports, Sidebar, ScopeFilters, ViewCommands, History, Analysis, Overview, Calendar, Costs, Settings],
  host: { class: 'sn-window flex h-screen min-h-0 flex-col overflow-hidden', snSheetHost: '' },
  template: `
    <ng-template #titleBar>
      @if (mac()) {
        <sn-title-bar class="cz-unified" [attr.aria-label]="toolbarTitle()">
          <span snTitleBarStart class="flex min-w-0 items-center gap-3">
            <sn-menubar [menu]="menu()" ariaLabel="Application menu" />
            <span class="flex min-w-0 items-baseline gap-2">
              <b class="truncate text-[0.95rem] font-semibold">{{ toolbarTitle() }}</b>
              @if (toolbarSubtitle()) { <span class="shrink-0 text-xs text-muted-foreground">{{ toolbarSubtitle() }}</span> }
            </span>
          </span>
          <span snTitleBarEnd class="flex items-center gap-2"><cz-scope-filters /><cz-view-commands /></span>
        </sn-title-bar>
      } @else {
        <sn-title-bar title="Claudealyzer" [subtitle]="subtitle()" [windowControls]="windowControls">
          <img snTitleBarIcon src="favicon.ico" alt="" width="16" height="16" />
          <sn-menubar snTitleBarStart [menu]="menu()" ariaLabel="Application menu" />
          <span snTitleBarEnd class="flex items-center gap-2"><cz-scope-filters /><cz-view-commands /></span>
        </sn-title-bar>
      }
    </ng-template>
    <ng-template #view>
      <main class="sn-content grid min-h-0 min-w-0 flex-1 grid-rows-[minmax(0,1fr)]">
        @switch (state.view()) {
          @case ('history') { <cz-history /> }
          @case ('analysis') { <cz-analysis /> }
          @case ('overview') { <cz-overview /> }
          @case ('calendar') { <cz-calendar /> }
          @case ('costs') { <cz-costs /> }
          @case ('settings') { <cz-settings /> }
        }
      </main>
    </ng-template>

    @if (mac()) {
      <div class="flex min-h-0 flex-1">
        <cz-sidebar class="shrink-0" [windowChrome]="windowControls === 'native' ? 'native' : 'drawn'" />
        <div class="flex min-h-0 min-w-0 flex-1 flex-col">
          <ng-container [ngTemplateOutlet]="titleBar" />
          <ng-container [ngTemplateOutlet]="view" />
        </div>
      </div>
    } @else {
      <ng-container [ngTemplateOutlet]="titleBar" />
      <div class="flex min-h-0 flex-1">
        <cz-sidebar class="shrink-0" />
        <ng-container [ngTemplateOutlet]="view" />
      </div>
    }
    <sn-status-bar ariaLabel="Status">
      <span snStatusItem live>{{ scanText() }}</span>
      <span snStatusItem>{{ state.projects().length }} projects · {{ state.sources().length }} sources</span>
      <span snStatusItem snStatusBarEnd class="tabular">{{ fmtUSD(totalCost()) }} list price</span>
    </sn-status-bar>
  `,
})
export class App implements OnInit {
  protected readonly state = inject(AppState);
  protected readonly fmtUSD = fmtUSD;
  /** macOS draws its own traffic lights in the hidden-inset title bar; elsewhere the window is frameless. */
  protected readonly windowControls = isWails() && injectPlatformFamily()() === 'macos' ? 'native' : 'custom';
  private readonly skin = injectSkin();
  /** The Mac look (macOS 26 or 13–15), whatever OS it runs on: switches to the unified-toolbar layout. */
  readonly mac = computed(() => skinFamily(this.skin()) === 'macos');

  readonly toolbarTitle = computed(() => {
    // History has no page heading of its own, so the toolbar names the project; the other pages
    // title themselves in their command bar, so the toolbar shows the app's name instead.
    if (this.state.view() !== 'history') return 'Claudealyzer';
    return this.state.project()?.name ?? 'Claudealyzer';
  });
  readonly toolbarSubtitle = computed(() => {
    if (this.state.view() !== 'history') return '';
    const n = this.state.sessions().length;
    return `${n} session${n === 1 ? '' : 's'}`;
  });

  readonly subtitle = computed(() => {
    const v = VIEWS.find((x) => x.id === this.state.view())?.label ?? '';
    const p = this.state.project()?.name;
    return this.state.view() === 'settings' || !p ? v : `${p} · ${v}`;
  });

  readonly totalCost = computed(() => this.state.projects().reduce((a, p) => a + p.costUSD, 0));

  readonly scanText = computed(() => {
    const s = this.state.scan();
    if (s?.running) return `Reading ${s.source} · ${s.done} / ${s.total}`;
    return s?.last ? `Up to date · ${new Date(s.last).toLocaleTimeString('en-GB', { hour: '2-digit', minute: '2-digit' })}` : 'Ready';
  });

  /** One definition: the macOS global menu bar with the Wails host, an in-window menu bar elsewhere. */
  readonly menu = computed<SnMenuItem[]>(() => {
    const view = this.state.view();
    const hasSession = !!this.state.current();
    return [
      { role: 'appMenu' },
      {
        label: 'File',
        submenu: [
          { id: 'add-source', label: 'Add source…', accelerator: 'mod+shift+n', action: () => this.state.requestAddSource() },
          { id: 'rescan', label: 'Rescan sources', accelerator: 'mod+shift+r', action: () => void SourceService.Rescan() },
          { type: 'separator' },
          { role: 'close' },
        ],
      },
      { role: 'editMenu' },
      {
        label: 'View',
        submenu: [
          ...VIEWS.filter((v) => v.id !== 'settings').map<SnMenuItem>((v) => ({
            id: 'view-' + v.id,
            label: v.label,
            type: 'radio',
            checked: view === v.id,
            enabled: hasSession || (v.id !== 'analysis' && v.id !== 'costs'),
            accelerator: 'mod+' + v.key,
            action: () => this.state.go(v.id),
          })),
          { type: 'separator' },
          {
            id: 'panel',
            label: 'Usage & insights panel',
            type: 'checkbox',
            checked: this.state.panelOpen(),
            accelerator: 'mod+alt+i',
            action: () => this.state.panelOpen.update((o) => !o),
          },
          { type: 'separator' },
          { id: 'settings', label: 'Sources & settings', accelerator: 'mod+,', action: () => this.state.go('settings') },
          { role: 'toggleFullscreen' },
        ],
      },
      { role: 'windowMenu' },
    ];
  });

  ngOnInit(): void {
    void this.state.init();
  }
}
