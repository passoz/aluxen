import React from 'react';
import { hydrateRoot } from 'react-dom/client';
import { DashboardPage, type DashboardProps } from './dashboard';
import { LoginPage, type LoginProps } from './login';
import { SettingsPage, type SettingsProps } from './settings';
import {
  ClientLoginPage,
  ClientPortalPage,
  ClientPasswordPage,
  type ClientLoginProps,
  type ClientPortalProps,
  type ClientPasswordProps,
} from './portal';

declare global {
  interface Window {
    __INITIAL_DATA__?: {
      page: 'dashboard' | 'login' | 'settings' | 'client-login' | 'client-portal' | 'client-password';
      props: Record<string, unknown>;
    };
  }
}

const rootEl = document.getElementById('root');
if (rootEl && window.__INITIAL_DATA__) {
  const { page, props } = window.__INITIAL_DATA__;

  if (page === 'dashboard') {
    hydrateRoot(rootEl, <DashboardPage {...(props as unknown as DashboardProps)} />);
  } else if (page === 'login') {
    hydrateRoot(rootEl, <LoginPage {...(props as unknown as LoginProps)} />);
  } else if (page === 'settings') {
    hydrateRoot(rootEl, <SettingsPage {...(props as unknown as SettingsProps)} />);
  } else if (page === 'client-login') {
    hydrateRoot(rootEl, <ClientLoginPage {...(props as unknown as ClientLoginProps)} />);
  } else if (page === 'client-portal') {
    hydrateRoot(rootEl, <ClientPortalPage {...(props as unknown as ClientPortalProps)} />);
  } else if (page === 'client-password') {
    hydrateRoot(rootEl, <ClientPasswordPage {...(props as unknown as ClientPasswordProps)} />);
  }
}
