import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import clsx from 'clsx';
import { PageHeader } from '../app/AppShell';
import { Card, EmptyState, ErrorState } from '../ui/display';
import { Button } from '../ui/Button';
import { Textarea } from '../ui/Field';
import { SkeletonList } from '../ui/Skeleton';
import { useToast } from '../ui/Toast';
import { Plus } from '../ui/icons';
import {
  useCreateDoc,
  useDoc,
  useDocRevision,
  useDocRevisions,
  useDocs,
  useSaveDoc,
  useSpaces
} from '../lib/queries';
import { formatDateTime } from '../lib/format';
import styles from './pages.module.css';

/**
 * Documents, stored as Markdown.
 *
 * Markdown rather than a rich-text document model: the content stays
 * greppable, diffable and portable, and the editor is a detail of this screen
 * rather than a format the database has to understand. Every save keeps the
 * previous version, so a careless paste is recoverable.
 */
export default function DocsPage() {
  const { t, i18n } = useTranslation();
  const toast = useToast();

  const { data: spaces } = useSpaces();
  const [spaceId, setSpaceId] = useState<string>();
  const activeSpace = spaceId ?? spaces?.[0]?.id;

  const { data: docs, isLoading, isError, refetch } = useDocs({ spaceId: activeSpace });
  const [selectedId, setSelectedId] = useState<string>();
  const activeDocId = selectedId ?? docs?.[0]?.id;

  const { data: doc } = useDoc(activeDocId);
  const { data: revisions } = useDocRevisions(activeDocId);
  const createDoc = useCreateDoc();
  const saveDoc = useSaveDoc();

  const [title, setTitle] = useState('');
  const [content, setContent] = useState('');
  const [dirty, setDirty] = useState(false);
  const [historyOpen, setHistoryOpen] = useState(false);
  const [restoringId, setRestoringId] = useState<number>();
  const { data: restored } = useDocRevision(activeDocId, restoringId);

  // Load the document into the editor, but never over unsaved edits: a
  // background refetch must not discard what someone is typing.
  useEffect(() => {
    if (!doc || dirty) return;
    setTitle(doc.title);
    setContent(doc.content);
  }, [doc, dirty]);

  useEffect(() => {
    setDirty(false);
    setRestoringId(undefined);
    setHistoryOpen(false);
  }, [activeDocId]);

  // A restored version lands in the editor as an unsaved edit rather than
  // overwriting the document outright: the person restoring gets to read it
  // first, and changes their mind by navigating away.
  useEffect(() => {
    if (!restored?.content && restored?.content !== '') return;
    setTitle(restored.title);
    setContent(restored.content);
    setDirty(true);
  }, [restored]);

  function save() {
    if (!activeDocId) return;
    saveDoc.mutate(
      { id: activeDocId, title, content },
      {
        onSuccess: () => {
          setDirty(false);
          toast.success(t('docs.saved'));
        },
        onError: () => toast.error(t('errors.genericBody'))
      }
    );
  }

  const newDoc = (
    <Button
      variant="primary"
      disabled={!activeSpace}
      loading={createDoc.isPending}
      onClick={() =>
        activeSpace &&
        createDoc.mutate(
          { spaceId: activeSpace, title: t('docs.untitled'), content: '' },
          { onSuccess: (created) => setSelectedId(created.id) }
        )
      }
    >
      <Plus size={16} />
      {t('docs.new')}
    </Button>
  );

  return (
    <>
      <PageHeader title={t('docs.title')} description={t('docs.hint')} actions={newDoc} />

      {spaces && spaces.length > 1 && (
        <div style={{ marginBottom: 'var(--space-4)', display: 'flex', gap: 'var(--space-2)', flexWrap: 'wrap' }}>
          {spaces.map((space) => (
            <Button
              key={space.id}
              size="sm"
              variant={space.id === activeSpace ? 'primary' : 'secondary'}
              onClick={() => {
                setSpaceId(space.id);
                setSelectedId(undefined);
              }}
            >
              {space.name}
            </Button>
          ))}
        </div>
      )}

      {isLoading && <SkeletonList rows={3} height={44} label={t('common.loading')} />}

      {isError && (
        <Card>
          <ErrorState title={t('errors.loadFailed')} onRetry={() => void refetch()} retryLabel={t('common.retry')} />
        </Card>
      )}

      {docs?.length === 0 && (
        <Card>
          <EmptyState title={t('docs.empty')} body={t('docs.emptyBody')} action={newDoc} />
        </Card>
      )}

      {docs && docs.length > 0 && (
        <div className={styles.docsLayout}>
          <Card padded={false}>
            <nav className={styles.docList} aria-label={t('docs.title')}>
              {docs.map((entry) => (
                <button
                  key={entry.id}
                  type="button"
                  className={clsx(styles.docItem, entry.id === activeDocId && styles.docItemActive)}
                  aria-current={entry.id === activeDocId ? 'true' : undefined}
                  onClick={() => setSelectedId(entry.id)}
                >
                  {entry.title}
                </button>
              ))}
            </nav>
          </Card>

          <Card padded={false}>
            {doc ? (
              <div className={styles.docEditor}>
                <input
                  className={styles.docTitleInput}
                  value={title}
                  aria-label={t('docs.docTitle')}
                  onChange={(event) => {
                    setTitle(event.target.value);
                    setDirty(true);
                  }}
                />

                <Textarea
                  className={styles.docBody}
                  value={content}
                  aria-label={t('docs.body')}
                  placeholder={t('docs.placeholder')}
                  onChange={(event) => {
                    setContent(event.target.value);
                    setDirty(true);
                  }}
                  onKeyDown={(event) => {
                    // Ctrl/Cmd+S saves, as anyone editing a document expects.
                    if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === 's') {
                      event.preventDefault();
                      save();
                    }
                  }}
                />

                <div className={styles.docMeta}>
                  <Button variant="primary" size="sm" onClick={save} loading={saveDoc.isPending} disabled={!dirty}>
                    {dirty ? t('common.save') : t('docs.saved')}
                  </Button>
                  <span>
                    {t('docs.updated')}: {formatDateTime(doc.updatedAt, i18n.language)}
                  </span>
                  {revisions && revisions.length > 0 && (
                    <Button variant="ghost" size="sm" onClick={() => setHistoryOpen((open) => !open)}>
                      {t('docs.revisions', { count: revisions.length })}
                    </Button>
                  )}
                </div>

                {historyOpen && revisions && (
                  <ul className={styles.revisionList} aria-label={t('docs.history')}>
                    {revisions.map((revision) => (
                      <li key={revision.id} className={styles.revisionRow}>
                        <span>{formatDateTime(revision.createdAt, i18n.language)}</span>
                        <Button variant="ghost" size="sm" onClick={() => setRestoringId(revision.id)}>
                          {t('docs.restore')}
                        </Button>
                      </li>
                    ))}
                  </ul>
                )}
              </div>
            ) : (
              <EmptyState title={t('docs.selectOne')} />
            )}
          </Card>
        </div>
      )}
    </>
  );
}
