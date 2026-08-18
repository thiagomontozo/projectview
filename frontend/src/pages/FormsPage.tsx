import { lazy, Suspense, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { PageHeader } from '../app/AppShell';
import { Button } from '../ui/Button';
import { Badge, Card, EmptyState } from '../ui/display';
import { Spinner } from '../ui/Spinner';
import { Inbox } from '../ui/icons';
import { ProjectPicker } from '../components/ProjectPicker';
import { useIntakeForms } from '../lib/queries';
import styles from './whiteboard.module.css';

const IntakeDialog = lazy(() => import('../components/IntakeDialog'));

/**
 * Forms, as a place of its own.
 *
 * The intake feature shipped complete and reachable only from inside a project
 * board, which meant the people most likely to want it — whoever fields the
 * requests — had no route to it at all. This is that route: pick a project, see
 * its forms and what has come in, open the builder.
 *
 * The builder itself is not duplicated here. It is the same dialog the board
 * opens, because two form builders would be two places for the two to disagree.
 */
export default function FormsPage() {
  const { t } = useTranslation();
  const [projectId, setProjectId] = useState<string>();
  const [open, setOpen] = useState(false);
  const forms = useIntakeForms(projectId);

  return (
    <>
      <PageHeader
        title={t('intake.title')}
        description={t('intake.hint')}
        actions={
          <Button variant="primary" disabled={!projectId} onClick={() => setOpen(true)}>
            {t('forms.manage')}
          </Button>
        }
      />

      <div className={styles.toolbarRow}>
        <ProjectPicker value={projectId} onChange={setProjectId} />
      </div>

      {forms.isLoading ? (
        <Spinner label={t('common.loading')} />
      ) : !forms.data?.length ? (
        <Card>
          <EmptyState
            icon={<Inbox size={28} />}
            title={t('intake.noForms')}
            body={t('intake.noFormsBody')}
            action={
              <Button variant="primary" disabled={!projectId} onClick={() => setOpen(true)}>
                {t('intake.newForm')}
              </Button>
            }
          />
        </Card>
      ) : (
        <div className={styles.grid}>
          {forms.data.map((form) => (
            <Card key={form.id} interactive>
              <button type="button" className={styles.cardOpen} onClick={() => setOpen(true)}>
                <strong>{form.title}</strong>
                <span className={styles.muted}>
                  {t('forms.questionCount', { count: form.fields.length })}
                </span>
                <span className={styles.toolbarRow}>
                  <Badge tone={form.enabled ? 'success' : undefined}>
                    {form.enabled ? t('intake.open') : t('intake.closed')}
                  </Badge>
                  {form.public && <Badge tone="accent">{t('intake.publicForm')}</Badge>}
                </span>
              </button>
            </Card>
          ))}
        </div>
      )}

      {open && projectId && (
        <Suspense fallback={null}>
          <IntakeDialog projectId={projectId} onClose={() => setOpen(false)} />
        </Suspense>
      )}
    </>
  );
}
