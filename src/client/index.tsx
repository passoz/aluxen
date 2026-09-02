import React from 'react';
import { hydrateRoot } from 'react-dom/client';
import { DashboardPage, type DashboardProps } from './dashboard';
import { LoginPage, type LoginProps } from './login';
import { SettingsPage, type SettingsProps } from './settings';

declare global {
  interface Window {
    __INITIAL_DATA__?: {
      page: 'dashboard' | 'login' | 'settings';
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
  }
}
