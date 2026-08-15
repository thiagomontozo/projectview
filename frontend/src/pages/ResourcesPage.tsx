import { useTranslation } from 'react-i18next';
import { PageHeader } from '../app/AppShell';
import { Avatar, Badge, Card, EmptyState, ErrorState } from '../ui/display';
import { SkeletonList } from '../ui/Skeleton';
import { Puzzle } from '../ui/icons';
import { useWorkload } from '../lib/queries';
import styles from './pages.module.css';

export default function ResourcesPage() {
  const { t } = useTranslation();
  const { data: rows, isLoading, isError, refetch } = useWorkload();

  const hasAllocations = rows?.some((row) => row.openTasks > 0);

  return (
    <>
      <PageHeader title={t('resources.title')} />

      {isLoading && <SkeletonList rows={5} height={48} label={t('common.loading')} />}

      {isError && (
        <Card>
          <ErrorState title={t('errors.loadFailed')} onRetry={() => void refetch()} retryLabel={t('common.retry')} />
        </Card>
      )}

      {rows && !hasAllocations && (
        <Card>
          <EmptyState icon={<Puzzle size={22} />} title={t('resources.empty')} body={t('resources.emptyBody')} />
        </Card>
      )}

      {rows && hasAllocations && (
        <Card padded={false}>
          <div className={styles.tableWrap}>
            <table className={styles.table}>
              <caption className="sr-only">{t('resources.title')}</caption>
              <thead>
                <tr>
                  <th scope="col">{t('resources.person')}</th>
                  <th scope="col" className={styles.numeric}>
                    {t('resources.openTasks')}
                  </th>
                  <th scope="col" className={styles.numeric}>
                    {t('resources.hours')}
                  </th>
                  <th scope="col" className={styles.numeric}>
                    {t('resources.projects')}
                  </th>
                  <th scope="col" className={styles.numeric}>
                    {t('resources.overdue')}
                  </th>
                </tr>
              </thead>
              <tbody>
                {rows.map((row) => (
                  <tr key={row.user.id}>
                    <td>
                      <div className={styles.personCell}>
                        <Avatar name={row.user.name} color={row.user.avatarColor} size={28} />
                        <div>
                          <div>{row.user.name}</div>
                          {row.user.title && <div className={styles.subtle}>{row.user.title}</div>}
                        </div>
                      </div>
                    </td>
                    <td className={styles.numeric}>{row.openTasks}</td>
                    <td className={styles.numeric}>{row.estimateHours}</td>
                    <td className={styles.numeric}>{row.projectCount}</td>
                    <td className={styles.numeric}>
                      {row.overdue > 0 ? <Badge tone="danger">{row.overdue}</Badge> : row.overdue}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </Card>
      )}
    </>
  );
}
