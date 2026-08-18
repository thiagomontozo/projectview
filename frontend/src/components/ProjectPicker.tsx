import { useEffect } from 'react';
import { useTranslation } from 'react-i18next';
import { useProjects } from '../lib/queries';
import controls from '../ui/controls.module.css';

/**
 * Chooses the project a document belongs to.
 *
 * Whiteboards, sheets and clips all belong to a project — that is what decides
 * who may open them — so all three screens need this same control. Shared
 * rather than copied three times, and it selects the first project on its own:
 * a screen that opens empty and asks you to choose before showing anything is a
 * screen that looks broken on the first visit.
 */
export function ProjectPicker({
  value,
  onChange
}: {
  value: string | undefined;
  onChange: (projectId: string) => void;
}) {
  const { t } = useTranslation();
  const { data: projects = [], isLoading } = useProjects();

  useEffect(() => {
    if (!value && projects.length > 0) onChange(projects[0].id);
  }, [value, projects, onChange]);

  if (isLoading) return null;
  if (projects.length === 0) return <p>{t('canvas.noProjects')}</p>;

  return (
    <label>
      <span className="visually-hidden">{t('nav.projects')}</span>
      <select
        className={controls.input}
        aria-label={t('nav.projects')}
        value={value ?? ''}
        onChange={(event) => onChange(event.target.value)}
      >
        {projects.map((project) => (
          <option key={project.id} value={project.id}>
            {project.name}
          </option>
        ))}
      </select>
    </label>
  );
}
