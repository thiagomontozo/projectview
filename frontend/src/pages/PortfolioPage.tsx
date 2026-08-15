import { useState } from 'react';
import { Link } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import clsx from 'clsx';
import { PageHeader } from '../app/AppShell';
import { Avatar, Badge, Card, CardHeader, EmptyState, ErrorState } from '../ui/display';
import { Button } from '../ui/Button';
import { SkeletonList } from '../ui/Skeleton';
import { useToast } from '../ui/Toast';
import { Chart } from '../ui/icons';
import controls from '../ui/controls.module.css';
import {
  useCapacity,
  useCaptureBaseline,
  useEarnedValue,
  usePortfolio,
  type PortfolioProject
} from '../lib/enterprise';
import { useProjects } from '../lib/queries';
import { formatDate } from '../lib/format';
import { downloadUrl } from '../lib/api';
import styles from './pages.module.css';

const HEALTH_TONES: Record<PortfolioProject['health'], 'success' | 'warning' | 'danger' | 'neutral'> = {
  on_track: 'success',
  at_risk: 'warning',
  off_track: 'danger',
  done: 'neutral'
};

/**
 * Every project at once: health, progress, and whether the people are
 * over-committed.
 *
 * Health is derived rather than typed in. A RAG status somebody updates by
 * hand is a status that is green until the week it is red, which is the week
 * it stopped being useful.
 */
export default function PortfolioPage() {
  const { t, i18n } = useTranslation();
  const portfolio = usePortfolio();

  return (
    <>
      <PageHeader
        title={t('portfolio.title')}
        description={t('portfolio.hint')}
        actions={
          <Button variant="secondary" onClick={() => downloadUrl('/portfolio/export.csv')}>
            {t('portfolio.exportCsv')}
          </Button>
        }
      />

      {portfolio.isLoading && <SkeletonList rows={4} height={52} label={t('common.loading')} />}

      {portfolio.isError && (
        <Card>
          {/* The endpoint is management-only, so the usual cause of an error
              here is that the person looking is not one. */}
          <ErrorState
            title={t('portfolio.restricted')}
            body={t('portfolio.restrictedBody')}
            onRetry={() => void portfolio.refetch()}
            retryLabel={t('common.retry')}
          />
        </Card>
      )}

      {portfolio.data?.length === 0 && (
        <Card>
          <EmptyState icon={<Chart size={20} />} title={t('portfolio.empty')} />
        </Card>
      )}

      {portfolio.data && portfolio.data.length > 0 && (
        <Card padded={false}>
          <div className={styles.tableWrap}>
            <table className={styles.table}>
              <thead>
                <tr>
                  <th scope="col">{t('portfolio.project')}</th>
                  <th scope="col">{t('portfolio.health')}</th>
                  <th scope="col">{t('portfolio.progress')}</th>
                  <th scope="col" className={styles.numeric}>
                    {t('portfolio.tasks')}
                  </th>
                  <th scope="col" className={styles.numeric}>
                    {t('portfolio.overdue')}
                  </th>
                  <th scope="col" className={styles.numeric}>
                    {t('portfolio.hours')}
                  </th>
                  <th scope="col">{t('portfolio.end')}</th>
                </tr>
              </thead>
              <tbody>
                {portfolio.data.map((project) => (
                  <tr key={project.id}>
                    <td>
                      <Link to={`/projects/${project.id}`} className={styles.portfolioLink}>
                        <span className={styles.projectDot} style={{ background: project.color }} aria-hidden="true" />
                        {project.name}
                      </Link>
                    </td>
                    <td>
                      <Badge tone={HEALTH_TONES[project.health]}>{t(`portfolio.health_${project.health}`)}</Badge>
                    </td>
                    <td>
                      <div className={styles.miniBar} aria-hidden="true">
                        <span style={{ width: `${Math.round(project.progress * 100)}%` }} />
                      </div>
                      <span className={styles.subtle}>{Math.round(project.progress * 100)}%</span>
                    </td>
                    <td className={styles.numeric}>
                      {project.doneTasks}/{project.totalTasks}
                    </td>
                    <td className={styles.numeric}>
                      {project.overdueOpen > 0 ? (
                        <strong style={{ color: 'var(--danger)' }}>{project.overdueOpen}</strong>
                      ) : (
                        '—'
                      )}
                    </td>
                    <td className={styles.numeric}>
                      {project.trackedHours.toFixed(0)} / {project.estimatedHours.toFixed(0)}
                    </td>
                    <td>{formatDate(project.endDate, i18n.language)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </Card>
      )}

      <CapacitySection />
      <EarnedValueSection />
    </>
  );
}

function CapacitySection() {
  const { t } = useTranslation();
  const [weeks, setWeeks] = useState(4);
  const capacity = useCapacity(weeks);

  return (
    <div className={styles.settingsSection}>
      <Card padded={false}>
        <div style={{ padding: 'var(--space-5) var(--space-5) 0' }}>
          <CardHeader
            title={t('portfolio.capacity')}
            action={
              <div style={{ display: 'flex', gap: 'var(--space-2)', alignItems: 'center' }}>
                <select
                  className={controls.input}
                  style={{ width: 140 }}
                  aria-label={t('portfolio.window')}
                  value={weeks}
                  onChange={(event) => setWeeks(Number(event.target.value))}
                >
                  {[1, 2, 4, 8, 12].map((option) => (
                    <option key={option} value={option}>
                      {t('portfolio.weeks', { count: option })}
                    </option>
                  ))}
                </select>
                <Button
                  size="sm"
                  variant="secondary"
                  onClick={() => downloadUrl('/portfolio/capacity/export.csv')}
                >
                  {t('portfolio.exportCsv')}
                </Button>
              </div>
            }
          />
          <p className={styles.muted}>{t('portfolio.capacityHint')}</p>
        </div>

        {capacity.isLoading && (
          <div style={{ padding: 'var(--space-5)' }}>
            <SkeletonList rows={3} height={40} label={t('common.loading')} />
          </div>
        )}

        {capacity.data && (
          <div className={styles.tableWrap}>
            <table className={styles.table}>
              <thead>
                <tr>
                  <th scope="col">{t('portfolio.person')}</th>
                  <th scope="col" className={styles.numeric}>
                    {t('portfolio.committed')}
                  </th>
                  <th scope="col" className={styles.numeric}>
                    {t('portfolio.available')}
                  </th>
                  <th scope="col">{t('portfolio.utilisation')}</th>
                  <th scope="col" className={styles.numeric}>
                    {t('portfolio.openTasks')}
                  </th>
                </tr>
              </thead>
              <tbody>
                {capacity.data.rows.map((row) => {
                  const percent = Math.round(row.utilisation * 100);
                  return (
                    <tr key={row.userId}>
                      <td>
                        <div className={styles.personCell}>
                          <Avatar name={row.name} color={row.avatarColor} size={26} />
                          {row.name}
                        </div>
                      </td>
                      <td className={styles.numeric}>{row.committedHours.toFixed(1)}h</td>
                      <td className={styles.numeric}>{row.capacityHours.toFixed(0)}h</td>
                      <td>
                        {/* The bar is capped at 100% of its width; the number
                            beside it is not, because how far past capacity
                            somebody is the whole point of the row. */}
                        <div className={styles.miniBar} aria-hidden="true">
                          <span
                            className={clsx(percent > 100 && styles.miniBarOver)}
                            style={{ width: `${Math.min(100, percent)}%` }}
                          />
                        </div>
                        <span className={clsx(styles.subtle, percent > 100 && styles.overloaded)}>{percent}%</span>
                      </td>
                      <td className={styles.numeric}>{row.openTasks}</td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        )}
      </Card>
    </div>
  );
}

/**
 * Earned value against the approved plan.
 *
 * Measured in hours rather than currency: the system holds estimates and
 * tracked time but no rates, and inventing one to print a money figure would
 * produce a number that looks authoritative and is not.
 */
function EarnedValueSection() {
  const { t, i18n } = useTranslation();
  const toast = useToast();
  const { data: projects } = useProjects();
  const [projectId, setProjectId] = useState<string>();
  const active = projectId ?? projects?.[0]?.id;

  const report = useEarnedValue(active);
  const capture = useCaptureBaseline();

  return (
    <div className={styles.settingsSection}>
      <Card>
        <CardHeader
          title={t('portfolio.earnedValue')}
          action={
            <div style={{ display: 'flex', gap: 'var(--space-2)' }}>
              <select
                className={controls.input}
                style={{ width: 200 }}
                aria-label={t('portfolio.project')}
                value={active ?? ''}
                onChange={(event) => setProjectId(event.target.value)}
              >
                {projects?.map((project) => (
                  <option key={project.id} value={project.id}>
                    {project.name}
                  </option>
                ))}
              </select>
              <Button
                size="sm"
                variant="secondary"
                loading={capture.isPending}
                disabled={!active}
                onClick={() =>
                  active &&
                  capture.mutate(
                    { projectId: active },
                    {
                      onSuccess: () => toast.success(t('portfolio.baselineCaptured')),
                      onError: () => toast.error(t('portfolio.baselineFailed'))
                    }
                  )
                }
              >
                {t('portfolio.captureBaseline')}
              </Button>
            </div>
          }
        />

        <p className={styles.muted}>{t('portfolio.earnedValueHint')}</p>

        {report.isLoading && <SkeletonList rows={2} height={48} label={t('common.loading')} />}

        {report.isError && (
          <EmptyState title={t('portfolio.noBaseline')} body={t('portfolio.noBaselineBody')} />
        )}

        {report.data && (
          <>
            <p className={styles.subtle}>
              {report.data.baseline.name} · {formatDate(report.data.baseline.capturedAt, i18n.language)} ·{' '}
              {t('portfolio.baselinedTasks', { count: report.data.baseline.tasks })}
            </p>

            <div className={styles.evmGrid}>
              <EvmFigure label={t('portfolio.bac')} value={`${report.data.earnedValue.bac.toFixed(0)}h`} />
              <EvmFigure label={t('portfolio.pv')} value={`${report.data.earnedValue.pv.toFixed(1)}h`} />
              <EvmFigure label={t('portfolio.ev')} value={`${report.data.earnedValue.ev.toFixed(1)}h`} />
              <EvmFigure label={t('portfolio.ac')} value={`${report.data.earnedValue.ac.toFixed(1)}h`} />
              <EvmFigure
                label={t('portfolio.spi')}
                value={report.data.earnedValue.spi?.toFixed(2) ?? '—'}
                tone={indexTone(report.data.earnedValue.spi)}
                hint={t('portfolio.spiHint')}
              />
              <EvmFigure
                label={t('portfolio.cpi')}
                value={report.data.earnedValue.cpi?.toFixed(2) ?? '—'}
                tone={indexTone(report.data.earnedValue.cpi)}
                hint={t('portfolio.cpiHint')}
              />
              <EvmFigure
                label={t('portfolio.eac')}
                value={report.data.earnedValue.eac ? `${report.data.earnedValue.eac.toFixed(0)}h` : '—'}
                hint={t('portfolio.eacHint')}
              />
              <EvmFigure
                label={t('portfolio.vac')}
                value={report.data.earnedValue.vac ? `${report.data.earnedValue.vac.toFixed(0)}h` : '—'}
                tone={report.data.earnedValue.vac == null ? undefined : report.data.earnedValue.vac >= 0 ? 'good' : 'bad'}
              />
            </div>
          </>
        )}
      </Card>
    </div>
  );
}

/** An index below 1 is behind or over; exactly 1 is neither. */
function indexTone(value: number | null | undefined): 'good' | 'bad' | undefined {
  if (value == null) return undefined;
  return value >= 1 ? 'good' : 'bad';
}

function EvmFigure({
  label,
  value,
  hint,
  tone
}: {
  label: string;
  value: string;
  hint?: string;
  tone?: 'good' | 'bad';
}) {
  return (
    <div className={styles.evmFigure}>
      <span className={styles.kpiLabel}>{label}</span>
      <strong
        className={clsx(styles.evmValue, tone === 'good' && styles.evmGood, tone === 'bad' && styles.evmBad)}
      >
        {value}
      </strong>
      {hint && <span className={styles.subtle}>{hint}</span>}
    </div>
  );
}
