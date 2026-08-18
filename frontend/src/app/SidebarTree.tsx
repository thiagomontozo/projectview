import { useState } from 'react';
import { NavLink } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { ChevronDown, ChevronRight } from '../ui/icons';
import { useProjects, useSpaces } from '../lib/queries';
import styles from './AppShell.module.css';

/**
 * Spaces and the projects inside them, in the sidebar.
 *
 * The hierarchy has existed in the data since the first migration and had no
 * expression in the navigation: getting to a project meant Projects, then a
 * list, then a click, with nothing on screen to say which space it belonged to.
 * A tree costs one line per project and answers "where does this live?" without
 * asking anybody to navigate to find out.
 *
 * Spaces start expanded. A tree that opens fully closed makes somebody click
 * before they can see anything, which is the one thing this exists to avoid.
 */
export function SidebarTree() {
  const { t } = useTranslation();
  const { data: spaces = [] } = useSpaces();
  const { data: projects = [] } = useProjects();
  const [collapsed, setCollapsed] = useState<Set<string>>(new Set());

  if (spaces.length === 0) return null;

  function toggle(id: string) {
    setCollapsed((current) => {
      const next = new Set(current);
      if (!next.delete(id)) next.add(id);
      return next;
    });
  }

  // Projects with no space would otherwise be invisible here, which would make
  // the tree quietly incomplete - worse than not having one.
  const orphans = projects.filter((project) => !project.spaceId);

  return (
    <div className={styles.tree}>
      <p className={styles.treeHeading}>{t('nav.spaces')}</p>

      {spaces.map((space) => {
        const children = projects.filter((project) => project.spaceId === space.id);
        const open = !collapsed.has(space.id);
        return (
          <div key={space.id}>
            <div className={styles.treeSpace}>
              <button
                type="button"
                className={styles.treeToggle}
                aria-expanded={open}
                aria-label={open ? t('views.collapseGroup') : t('views.expandGroup')}
                onClick={() => toggle(space.id)}
              >
                {open ? <ChevronDown size={13} /> : <ChevronRight size={13} />}
              </button>
              <NavLink to="/spaces" className={styles.treeSpaceName}>
                {space.name}
              </NavLink>
              {/* The count is of projects, and says so by sitting beside the
                  space name rather than a task count that would be a different
                  number people would read as this one. */}
              <span className={styles.treeCount}>{children.length}</span>
            </div>

            {open &&
              children.map((project) => (
                <NavLink
                  key={project.id}
                  to={`/projects/${project.id}`}
                  className={({ isActive }) =>
                    isActive ? `${styles.treeProject} ${styles.treeProjectActive}` : styles.treeProject
                  }
                >
                  <span className={styles.treeKey}>{project.key}</span>
                  {project.name}
                </NavLink>
              ))}
          </div>
        );
      })}

      {orphans.length > 0 && (
        <div>
          <div className={styles.treeSpace}>
            <span className={styles.treeSpaceName}>{t('nav.unfiled')}</span>
            <span className={styles.treeCount}>{orphans.length}</span>
          </div>
          {orphans.map((project) => (
            <NavLink
              key={project.id}
              to={`/projects/${project.id}`}
              className={({ isActive }) =>
                isActive ? `${styles.treeProject} ${styles.treeProjectActive}` : styles.treeProject
              }
            >
              <span className={styles.treeKey}>{project.key}</span>
              {project.name}
            </NavLink>
          ))}
        </div>
      )}
    </div>
  );
}
