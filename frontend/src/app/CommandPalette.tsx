import * as DialogPrimitive from '@radix-ui/react-dialog';
import { Command } from 'cmdk';
import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode
} from 'react';
import { useTranslation } from 'react-i18next';
import { useNavigate } from 'react-router-dom';
import styles from '../ui/overlays.module.css';
import { useProjects, useTaskSearch } from '../lib/queries';
import {
  Chart,
  Chat,
  CheckSquare,
  Dashboard,
  Folder,
  Layers,
  Search,
  Settings,
  Users
} from '../ui/icons';

interface CommandPaletteContextValue {
  open: () => void;
  close: () => void;
}

const CommandPaletteContext = createContext<CommandPaletteContextValue | null>(null);

export function useCommandPalette(): CommandPaletteContextValue {
  const context = useContext(CommandPaletteContext);
  if (!context) throw new Error('useCommandPalette must be used inside CommandPaletteProvider');
  return context;
}

/**
 * Keyboard-first navigation and search.
 *
 * The query goes to the server's full-text index rather than filtering an
 * already-loaded list, so it finds tasks the client has never fetched — which
 * is the difference between a filter and a search.
 */
export function CommandPaletteProvider({ children }: { children: ReactNode }) {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const [isOpen, setIsOpen] = useState(false);
  const [query, setQuery] = useState('');

  const open = useCallback(() => setIsOpen(true), []);
  const close = useCallback(() => setIsOpen(false), []);

  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      // Ctrl/Cmd+K is the near-universal shortcut for this.
      if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === 'k') {
        event.preventDefault();
        setIsOpen((current) => !current);
      }
    };
    window.addEventListener('keydown', onKeyDown);
    return () => window.removeEventListener('keydown', onKeyDown);
  }, []);

  // Reset the query on close, so reopening starts fresh rather than showing
  // the previous search's results.
  useEffect(() => {
    if (!isOpen) setQuery('');
  }, [isOpen]);

  const { data: projects = [] } = useProjects();
  const { data: taskResults } = useTaskSearch(query, isOpen);

  const navigationItems = useMemo(
    () => [
      { to: '/', label: t('nav.dashboard'), Icon: Dashboard },
      { to: '/my-tasks', label: t('nav.myTasks'), Icon: CheckSquare },
      { to: '/spaces', label: t('nav.spaces'), Icon: Layers },
      { to: '/projects', label: t('nav.projects'), Icon: Folder },
      { to: '/teams', label: t('nav.teams'), Icon: Users },
      { to: '/resources', label: t('nav.resources'), Icon: Chart },
      { to: '/chat', label: t('nav.chat'), Icon: Chat },
      { to: '/settings', label: t('nav.settings'), Icon: Settings }
    ],
    [t]
  );

  const go = (to: string) => {
    navigate(to);
    close();
  };

  const value = useMemo(() => ({ open, close }), [open, close]);

  return (
    <CommandPaletteContext.Provider value={value}>
      {children}

      <DialogPrimitive.Root open={isOpen} onOpenChange={setIsOpen}>
        <DialogPrimitive.Portal>
          <DialogPrimitive.Overlay className={styles.overlay} />
          <DialogPrimitive.Content className={styles.palette} aria-label={t('command.open')}>
            <DialogPrimitive.Title className="sr-only">{t('command.open')}</DialogPrimitive.Title>

            <Command shouldFilter={false} loop>
              <div className={styles.paletteInputWrap}>
                <Search size={17} />
                <Command.Input
                  className={styles.paletteInput}
                  placeholder={t('command.placeholder')}
                  value={query}
                  onValueChange={setQuery}
                  autoFocus
                />
              </div>

              <Command.List className={styles.paletteList}>
                <Command.Empty className={styles.paletteEmpty}>{t('command.empty')}</Command.Empty>

                {(taskResults?.items.length ?? 0) > 0 && (
                  <Command.Group heading={t('command.tasks')} className={styles.paletteGroup}>
                    {taskResults?.items.map((task) => {
                      const projectId =
                        typeof task.project === 'string' ? task.project : task.project?.id;
                      const projectName =
                        typeof task.project === 'string'
                          ? projects.find((p) => p.id === task.project)?.name
                          : task.project?.name;
                      return (
                        <Command.Item
                          key={task.id}
                          value={`task-${task.id}`}
                          className={styles.paletteItem}
                          onSelect={() => projectId && go(`/projects/${projectId}`)}
                        >
                          <span className={styles.paletteItemIcon}>
                            <CheckSquare size={16} />
                          </span>
                          {task.title}
                          {projectName && <span className={styles.paletteItemMeta}>{projectName}</span>}
                        </Command.Item>
                      );
                    })}
                  </Command.Group>
                )}

                <Command.Group heading={t('command.navigation')} className={styles.paletteGroup}>
                  {navigationItems
                    .filter((item) => item.label.toLowerCase().includes(query.toLowerCase()))
                    .map(({ to, label, Icon }) => (
                      <Command.Item
                        key={to}
                        value={`nav-${to}`}
                        className={styles.paletteItem}
                        onSelect={() => go(to)}
                      >
                        <span className={styles.paletteItemIcon}>
                          <Icon size={16} />
                        </span>
                        {label}
                      </Command.Item>
                    ))}
                </Command.Group>

                {projects.length > 0 && (
                  <Command.Group heading={t('nav.projects')} className={styles.paletteGroup}>
                    {projects
                      .filter((project) => project.name.toLowerCase().includes(query.toLowerCase()))
                      .slice(0, 6)
                      .map((project) => (
                        <Command.Item
                          key={project.id}
                          value={`project-${project.id}`}
                          className={styles.paletteItem}
                          onSelect={() => go(`/projects/${project.id}`)}
                        >
                          <span className={styles.paletteItemIcon}>
                            <Folder size={16} />
                          </span>
                          {project.name}
                          <span className={styles.paletteItemMeta}>{project.key}</span>
                        </Command.Item>
                      ))}
                  </Command.Group>
                )}
              </Command.List>

              <div className={styles.paletteFooter}>
                <span>
                  <kbd className={styles.kbd}>↑</kbd> <kbd className={styles.kbd}>↓</kbd>{' '}
                  {t('command.hintNavigate')}
                </span>
                <span>
                  <kbd className={styles.kbd}>↵</kbd> {t('command.hintSelect')}
                </span>
                <span>
                  <kbd className={styles.kbd}>esc</kbd> {t('command.hintClose')}
                </span>
              </div>
            </Command>
          </DialogPrimitive.Content>
        </DialogPrimitive.Portal>
      </DialogPrimitive.Root>
    </CommandPaletteContext.Provider>
  );
}
