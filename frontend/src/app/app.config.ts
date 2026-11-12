import { ApplicationConfig, provideBrowserGlobalErrorListeners, provideZonelessChangeDetection } from '@angular/core';
import { provideRouter } from '@angular/router';
import { provideSpartanNative } from '@spartan-native/theme';
import { isWails, wailsHost } from '@spartan-native/host-wails';
import { webHost } from '@spartan-native/host-web';
import { provideAppIcons } from './lib/icons';

export const appConfig: ApplicationConfig = {
  providers: [
    provideBrowserGlobalErrorListeners(),
    provideZonelessChangeDetection(),
    // The app has no routes; the router is provided because Spartan's breadcrumb links use RouterLink.
    provideRouter([]),
    // The real OS look in the desktop app, browser APIs in `ng serve`. `persist` keeps a skin or
    // scheme the user picks in Settings → Appearance between launches.
    provideSpartanNative({ host: isWails() ? wailsHost() : webHost(), persist: true }),
    provideAppIcons(),
  ],
};
