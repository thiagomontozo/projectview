import { useEffect, useState } from 'react';
import { NavLink, Outlet, useLocation } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import clsx from 'clsx';
import styles from './AppShell.module.css';
import overlays from '../ui/overlays.module.css';
import { useAuth } from '../lib/auth';
import { useNotifications, useMarkAllNotificationsRead } from '../lib/queries';
import { useCommandPalette } from './CommandPalette';
import { Avatar } from '../ui/display';
import { Button } from '../ui/Button';
import { Menu, MenuContent, MenuItem, MenuLabel, MenuSeparator, MenuTrigger, Tooltip } from '../ui/Menu';
import {
  Bell,
  Chart,
  Chat,
  CheckSquare,
  Dashboard,
  Folder,
  Layers,
  LogOut,
  Moon,
  Monitor,
  Puzzle,
  Search,
  Settings,
  Sun,
  Users
} from '../ui/icons';
import { useTheme, type ThemePreference } from '../lib/theme';
import { SUPPORTED_LANGUAGES } from '../i18n';

interface NavItem {
  to: string;
  labelKey: string;
  Icon: typeof Dashboard;
  /** Only the index route matches exactly; the rest match by prefix. */
  end?: boolean;
}

const NAV_ITEMS: NavItem[] = [
  { to: '/', labelKey: 'nav.dashboard', Icon: Dashboard, end: true },
  { to: '/my-tasks', labelKey: 'nav.myTasks', Icon: CheckSquare },
  { to: '/spaces', labelKey: 'nav.spaces', Icon: Layers },
  { to: '/projects', labelKey: 'nav.projects', Icon: Folder },
  { to: '/teams', labelKey: 'nav.teams', Icon: Users },
  { to: '/resources', labelKey: 'nav.resources', Icon: Puzzle },
  { to: '/reports', labelKey: 'nav.reports', Icon: Chart },
  { to: '/chat', labelKey: 'nav.chat', Icon: Chat }
];

export function AppShell() {
  const { t, i18n } = useTranslation();
  const { user, signOut } = useAuth();
  const { open: openPalette } = useCommandPalette();
  const location = useLocation();
  const [navOpen, setNavOpen] = useState(false);

  // Close the slide-over on navigation, or it stays over the page the user
  // just asked for.
  useEffect(() => setNavOpen(false), [location.pathname]);

  return (
    <div className={styles.shell}>
      <a className="skip-link" href="#main-content">
        {t('nav.skipToContent')}
      </a>

      {navOpen && <div className={styles.sidebarScrim} onClick={() => setNavOpen(false)} aria-hidden="true" />}

      <nav
        className={clsx(styles.sidebar, navOpen && styles.sidebarOpen)}
        aria-label={t('nav.primary')}
      >
        <div className={styles.brand}>
          <span className={styles.brandMark} aria-hidden="true">
            PV
          </span>
          <span className={styles.brandName}>{t('app.name')}</span>
        </div>

        <div className={styles.nav}>
          {NAV_ITEMS.map(({ to, labelKey, Icon, end }) => (
            <NavLink
              key={to}
              to={to}
              end={end}
              className={({ isActive }) => clsx(styles.navLink, isActive && styles.navLinkActive)}
              // Announces the current page rather than relying on colour alone.
              aria-current={undefined}
            >
              {({ isActive }) => (
                <>
                  <span className={styles.navIcon}>
                    <Icon size={17} />
                  </span>
                  {t(labelKey)}
                  {isActive && <span className="sr-only"> ({t('common.of')})</span>}
                </>
              )}
            </NavLink>
          ))}
        </div>

        <div className={styles.sidebarFooter}>
          <NavLink
            to="/settings"
            className={({ isActive }) => clsx(styles.navLink, isActive && styles.navLinkActive)}
          >
            <span className={styles.navIcon}>
              <Settings size={17} />
            </span>
            {t('nav.settings')}
          </NavLink>
        </div>
      </nav>

      <div className={styles.main}>
        <header className={styles.topbar}>
          <Button
            variant="ghost"
            size="sm"
            iconOnly
            className={styles.menuButton}
            aria-label={t('nav.primary')}
            aria-expanded={navOpen}
            onClick={() => setNavOpen((open) => !open)}
          >
            <Layers size={17} />
          </Button>

          <button type="button" className={styles.searchTrigger} onClick={openPalette}>
            <Search size={15} />
            {t('common.search')}
            <span className={styles.searchTriggerKbd} aria-hidden="true">
              <kbd className={overlays.kbd}>Ctrl</kbd>
              <kbd className={overlays.kbd}>K</kbd>
            </span>
          </button>

          <div className={styles.topbarSpacer} />

          <div className={styles.topbarActions}>
            <NotificationsMenu />
            <ThemeMenu />
            <LanguageMenu currentLanguage={i18n.language} />

            <Menu>
              <MenuTrigger asChild>
                <button type="button" className={styles.userButton}>
                  <Avatar name={user?.name ?? '?'} color={user?.avatarColor} size={26} />
                  <span className={styles.userName}>{user?.name}</span>
                </button>
              </MenuTrigger>
              <MenuContent>
                <MenuLabel>{user?.email}</MenuLabel>
                <MenuSeparator />
                <MenuItem onSelect={() => signOut()} danger>
                  <LogOut size={15} />
                  {t('auth.signOut')}
                </MenuItem>
              </MenuContent>
            </Menu>
          </div>
        </header>

        <main id="main-content" className={styles.content} tabIndex={-1}>
          <div className={styles.contentInner}>
            <Outlet />
          </div>
        </main>
      </div>
    </div>
  );
}

function NotificationsMenu() {
  const { t } = useTranslation();
  const { data: notifications = [] } = useNotifications();
  const markAllRead = useMarkAllNotificationsRead();
  const unread = notifications.filter((n) => !n.read).length;

  return (
    <Menu>
      <Tooltip content={t('notifications.title')}>
        <MenuTrigger asChild>
          <Button variant="ghost" size="sm" iconOnly aria-label={t('notifications.title')}>
            <span className={styles.notificationWrapper}>
              <Bell size={17} />
              {unread > 0 && <span className={styles.notificationDot} aria-hidden="true" />}
            </span>
          </Button>
        </MenuTrigger>
      </Tooltip>

      <MenuContent>
        <MenuLabel>
          {unread > 0 ? t('notifications.unread', { count: unread }) : t('notifications.title')}
        </MenuLabel>
        <MenuSeparator />

        {notifications.length === 0 ? (
          <div style={{ padding: 'var(--space-4)', color: 'var(--text-muted)', fontSize: 'var(--text-sm)' }}>
            {t('notifications.empty')}
          </div>
        ) : (
          <div className={styles.notificationList}>
            {notifications.slice(0, 8).map((notification) => (
              <div
                key={notification.id}
                className={clsx(styles.notificationItem, !notification.read && styles.notificationItemUnread)}
              >
                <span className={styles.notificationTitle}>{notification.title}</span>
                {notification.body && <span className={styles.notificationBody}>{notification.body}</span>}
              </div>
            ))}
          </div>
        )}

        {unread > 0 && (
          <>
            <MenuSeparator />
            <MenuItem onSelect={() => markAllRead.mutate()}>{t('notifications.markAllRead')}</MenuItem>
          </>
        )}
      </MenuContent>
    </Menu>
  );
}

function ThemeMenu() {
  const { t } = useTranslation();
  const { preference, setPreference } = useTheme();

  const options: Array<{ value: ThemePreference; labelKey: string; Icon: typeof Sun }> = [
    { value: 'light', labelKey: 'theme.light', Icon: Sun },
    { value: 'dark', labelKey: 'theme.dark', Icon: Moon },
    { value: 'system', labelKey: 'theme.system', Icon: Monitor }
  ];
  const Current = options.find((o) => o.value === preference)?.Icon ?? Monitor;

  return (
    <Menu>
      <Tooltip content={t('theme.label')}>
        <MenuTrigger asChild>
          <Button variant="ghost" size="sm" iconOnly aria-label={t('theme.label')}>
            <Current size={17} />
          </Button>
        </MenuTrigger>
      </Tooltip>
      <MenuContent>
        <MenuLabel>{t('theme.label')}</MenuLabel>
        {options.map(({ value, labelKey, Icon }) => (
          <MenuItem key={value} onSelect={() => setPreference(value)}>
            <Icon size={15} />
            {t(labelKey)}
            {preference === value && <span className={overlays.menuShortcut}>✓</span>}
          </MenuItem>
        ))}
      </MenuContent>
    </Menu>
  );
}

function LanguageMenu({ currentLanguage }: { currentLanguage: string }) {
  const { t, i18n } = useTranslation();

  return (
    <Menu>
      <Tooltip content={t('language.label')}>
        <MenuTrigger asChild>
          <Button variant="ghost" size="sm" aria-label={t('language.label')}>
            <span style={{ fontSize: 'var(--text-xs)', fontWeight: 'var(--weight-semibold)' }}>
              {currentLanguage.startsWith('pt') ? 'PT' : 'EN'}
            </span>
          </Button>
        </MenuTrigger>
      </Tooltip>
      <MenuContent>
        <MenuLabel>{t('language.label')}</MenuLabel>
        {SUPPORTED_LANGUAGES.map(({ code, label }) => (
          <MenuItem key={code} onSelect={() => void i18n.changeLanguage(code)}>
            {label}
            {currentLanguage === code && <span className={overlays.menuShortcut}>✓</span>}
          </MenuItem>
        ))}
      </MenuContent>
    </Menu>
  );
}

/** Consistent page heading used by every route. */
export function PageHeader({
  title,
  description,
  actions
}: {
  title: string;
  description?: string;
  actions?: React.ReactNode;
}) {
  return (
    <div className={styles.pageHeader}>
      <div>
        <h1 className={styles.pageTitle}>{title}</h1>
        {description && <p className={styles.pageDescription}>{description}</p>}
      </div>
      {actions && <div className={styles.pageActions}>{actions}</div>}
    </div>
  );
}
