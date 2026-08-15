import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { PageHeader } from '../app/AppShell';
import { Badge, Card, EmptyState, ErrorState } from '../ui/display';
import { Button } from '../ui/Button';
import { Dialog } from '../ui/Dialog';
import { Field, Input, Textarea } from '../ui/Field';
import { SkeletonList } from '../ui/Skeleton';
import { useToast } from '../ui/Toast';
import { Plus, Chart } from '../ui/icons';
import controls from '../ui/controls.module.css';
import {
  useAddKeyResult,
  useCreateGoal,
  useDeleteGoal,
  useGoals,
  useSetKeyResultValue,
  useUpdateGoal,
  type Goal,
  type KeyResult
} from '../lib/enterprise';
import { useProjects, useSpaces } from '../lib/queries';
import { formatDate } from '../lib/format';
import styles from './pages.module.css';

const STATUS_TONES: Record<Goal['status'], 'success' | 'warning' | 'danger' | 'accent'> = {
  active: 'accent',
  at_risk: 'warning',
  achieved: 'success',
  missed: 'danger'
};

const STATUSES = ['active', 'at_risk', 'achieved', 'missed'] as const;

/**
 * Goals and their key results.
 *
 * A key result is either typed in by hand or read from the tasks of a project.
 * The derived kind is the one that matters: a goal whose number nobody updates
 * is worse than no goal at all, and pointing a measure at the work means it
 * cannot drift from what the team is actually doing.
 */
export default function GoalsPage() {
  const { t, i18n } = useTranslation();
  const { data: spaces } = useSpaces();
  const [spaceId, setSpaceId] = useState<string>();
  const { data: goals, isLoading, isError, refetch } = useGoals(spaceId);
  const [creating, setCreating] = useState(false);

  const newGoal = (
    <Button variant="primary" onClick={() => setCreating(true)}>
      <Plus size={16} />
      {t('goals.new')}
    </Button>
  );

  return (
    <>
      <PageHeader title={t('goals.title')} description={t('goals.hint')} actions={newGoal} />

      {spaces && spaces.length > 0 && (
        <div className={styles.filterRow}>
          <Button size="sm" variant={spaceId ? 'secondary' : 'primary'} onClick={() => setSpaceId(undefined)}>
            {t('goals.allSpaces')}
          </Button>
          {spaces.map((space) => (
            <Button
              key={space.id}
              size="sm"
              variant={space.id === spaceId ? 'primary' : 'secondary'}
              onClick={() => setSpaceId(space.id)}
            >
              {space.name}
            </Button>
          ))}
        </div>
      )}

      {isLoading && <SkeletonList rows={3} height={120} label={t('common.loading')} />}

      {isError && (
        <Card>
          <ErrorState title={t('errors.loadFailed')} onRetry={() => void refetch()} retryLabel={t('common.retry')} />
        </Card>
      )}

      {goals?.length === 0 && (
        <Card>
          <EmptyState
            icon={<Chart size={20} />}
            title={t('goals.empty')}
            body={t('goals.emptyBody')}
            action={newGoal}
          />
        </Card>
      )}

      <div className={styles.grid}>
        {goals?.map((goal) => (
          <GoalCard key={goal.id} goal={goal} locale={i18n.language} />
        ))}
      </div>

      <GoalDialog open={creating} spaceId={spaceId} onClose={() => setCreating(false)} />
    </>
  );
}

function GoalCard({ goal, locale }: { goal: Goal; locale: string }) {
  const { t } = useTranslation();
  const toast = useToast();
  const updateGoal = useUpdateGoal();
  const deleteGoal = useDeleteGoal();
  const [addingMeasure, setAddingMeasure] = useState(false);

  const percent = Math.round(goal.progress * 100);

  return (
    <Card>
      <div className={styles.goalHeader}>
        <div>
          <h2 className={styles.goalName}>{goal.name}</h2>
          {goal.description && <p className={styles.muted}>{goal.description}</p>}
        </div>
        <Badge tone={STATUS_TONES[goal.status]}>{t(`goals.status_${goal.status}`)}</Badge>
      </div>

      <div className={styles.goalProgress}>
        <div
          className={styles.goalProgressBar}
          role="progressbar"
          aria-valuenow={percent}
          aria-valuemin={0}
          aria-valuemax={100}
          aria-label={goal.name}
        >
          <span style={{ width: `${percent}%` }} />
        </div>
        <strong className={styles.goalPercent}>{percent}%</strong>
      </div>

      {goal.dueDate && (
        <p className={styles.subtle}>
          {t('goals.due')}: {formatDate(goal.dueDate, locale)}
        </p>
      )}

      {goal.keyResults.length === 0 ? (
        <p className={styles.subtle} style={{ marginTop: 'var(--space-3)' }}>
          {t('goals.noMeasures')}
        </p>
      ) : (
        <ul className={styles.keyResultList}>
          {goal.keyResults.map((kr) => (
            <KeyResultRow key={kr.id} goalId={goal.id} keyResult={kr} />
          ))}
        </ul>
      )}

      <div className={styles.goalActions}>
        <Button size="sm" variant="secondary" onClick={() => setAddingMeasure(true)}>
          {t('goals.addMeasure')}
        </Button>
        <select
          className={controls.input}
          aria-label={t('goals.statusLabel')}
          value={goal.status}
          onChange={(event) =>
            updateGoal.mutate(
              { id: goal.id, status: event.target.value as Goal['status'] },
              { onSuccess: () => toast.success(t('goals.saved')) }
            )
          }
        >
          {STATUSES.map((status) => (
            <option key={status} value={status}>
              {t(`goals.status_${status}`)}
            </option>
          ))}
        </select>
        <Button
          size="sm"
          variant="dangerGhost"
          loading={deleteGoal.isPending}
          onClick={() => deleteGoal.mutate(goal.id, { onSuccess: () => toast.success(t('goals.deleted')) })}
        >
          {t('common.delete')}
        </Button>
      </div>

      <KeyResultDialog open={addingMeasure} goalId={goal.id} onClose={() => setAddingMeasure(false)} />
    </Card>
  );
}

function KeyResultRow({ goalId, keyResult }: { goalId: string; keyResult: KeyResult }) {
  const { t } = useTranslation();
  const setValue = useSetKeyResultValue();
  const [draft, setDraft] = useState(String(keyResult.currentValue));
  const derived = keyResult.source !== 'manual';

  return (
    <li className={styles.keyResultRow}>
      <span className={styles.keyResultName}>
        {keyResult.name}
        {derived && <span className={styles.subtle}> · {t(`goals.source_${keyResult.source}`)}</span>}
      </span>

      <span className={styles.keyResultNumbers}>
        {derived ? (
          // A derived measure is read-only on purpose: accepting a typed value
          // that the next read would overwrite is a lie about what was stored.
          <span className={styles.numeric}>
            {keyResult.currentValue.toFixed(1)} / {keyResult.targetValue} {keyResult.unit}
          </span>
        ) : (
          <>
            <Input
              type="number"
              className={styles.keyResultInput}
              aria-label={keyResult.name}
              value={draft}
              onChange={(event) => setDraft(event.target.value)}
              onBlur={() => {
                const value = Number(draft);
                if (Number.isFinite(value) && value !== keyResult.currentValue) {
                  setValue.mutate({ goalId, keyResultId: keyResult.id, value });
                }
              }}
            />
            <span className={styles.subtle}>
              / {keyResult.targetValue} {keyResult.unit}
            </span>
          </>
        )}
        <strong className={styles.numeric}>{Math.round(keyResult.progress * 100)}%</strong>
      </span>
    </li>
  );
}

function GoalDialog({ open, spaceId, onClose }: { open: boolean; spaceId?: string; onClose: () => void }) {
  const { t } = useTranslation();
  const toast = useToast();
  const createGoal = useCreateGoal();
  const { data: spaces } = useSpaces();

  const [name, setName] = useState('');
  const [description, setDescription] = useState('');
  const [space, setSpace] = useState(spaceId ?? '');
  const [dueDate, setDueDate] = useState('');

  function submit(event: React.FormEvent) {
    event.preventDefault();
    createGoal.mutate(
      {
        name,
        description,
        spaceId: space || undefined,
        dueDate: dueDate ? new Date(dueDate).toISOString() : undefined
      },
      {
        onSuccess: () => {
          toast.success(t('goals.created'));
          setName('');
          setDescription('');
          onClose();
        },
        onError: () => toast.error(t('errors.genericBody'))
      }
    );
  }

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => !next && onClose()}
      title={t('goals.new')}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            {t('common.cancel')}
          </Button>
          <Button variant="primary" type="submit" form="new-goal" loading={createGoal.isPending}>
            {t('common.create')}
          </Button>
        </>
      }
    >
      <form id="new-goal" onSubmit={submit} style={{ display: 'grid', gap: 'var(--space-4)' }}>
        <Field label={t('goals.name')} required>
          {({ id }) => <Input id={id} value={name} onChange={(e) => setName(e.target.value)} required />}
        </Field>

        <Field label={t('goals.description')}>
          {({ id }) => (
            <Textarea id={id} value={description} onChange={(e) => setDescription(e.target.value)} rows={3} />
          )}
        </Field>

        <Field label={t('goals.space')} hint={t('goals.spaceHint')}>
          {({ id, describedBy }) => (
            <select
              id={id}
              aria-describedby={describedBy}
              className={controls.input}
              value={space}
              onChange={(e) => setSpace(e.target.value)}
            >
              <option value="">{t('goals.organisationWide')}</option>
              {spaces?.map((s) => (
                <option key={s.id} value={s.id}>
                  {s.name}
                </option>
              ))}
            </select>
          )}
        </Field>

        <Field label={t('goals.due')}>
          {({ id }) => (
            <Input id={id} type="date" value={dueDate} onChange={(e) => setDueDate(e.target.value)} />
          )}
        </Field>
      </form>
    </Dialog>
  );
}

function KeyResultDialog({
  open,
  goalId,
  onClose
}: {
  open: boolean;
  goalId: string;
  onClose: () => void;
}) {
  const { t } = useTranslation();
  const toast = useToast();
  const addKeyResult = useAddKeyResult();
  const { data: projects } = useProjects();

  const [name, setName] = useState('');
  const [source, setSource] = useState<KeyResult['source']>('manual');
  const [projectId, setProjectId] = useState('');
  const [startValue, setStartValue] = useState('0');
  const [targetValue, setTargetValue] = useState('100');
  const [unit, setUnit] = useState('');

  const derived = source !== 'manual';

  function submit(event: React.FormEvent) {
    event.preventDefault();
    addKeyResult.mutate(
      {
        goalId,
        name,
        source,
        unit,
        projectId: derived ? projectId : undefined,
        startValue: Number(startValue),
        targetValue: Number(targetValue)
      },
      {
        onSuccess: () => {
          toast.success(t('goals.measureAdded'));
          setName('');
          onClose();
        },
        onError: () => toast.error(t('errors.genericBody'))
      }
    );
  }

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => !next && onClose()}
      title={t('goals.addMeasure')}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            {t('common.cancel')}
          </Button>
          <Button variant="primary" type="submit" form="new-key-result" loading={addKeyResult.isPending}>
            {t('common.create')}
          </Button>
        </>
      }
    >
      <form id="new-key-result" onSubmit={submit} style={{ display: 'grid', gap: 'var(--space-4)' }}>
        <Field label={t('goals.measureName')} required>
          {({ id }) => <Input id={id} value={name} onChange={(e) => setName(e.target.value)} required />}
        </Field>

        <Field label={t('goals.measureSource')} hint={t('goals.measureSourceHint')}>
          {({ id, describedBy }) => (
            <select
              id={id}
              aria-describedby={describedBy}
              className={controls.input}
              value={source}
              onChange={(e) => setSource(e.target.value as KeyResult['source'])}
            >
              <option value="manual">{t('goals.source_manual')}</option>
              <option value="tasks_completed">{t('goals.source_tasks_completed')}</option>
              <option value="tasks_count">{t('goals.source_tasks_count')}</option>
            </select>
          )}
        </Field>

        {derived && (
          <Field label={t('goals.measureProject')} required>
            {({ id }) => (
              <select
                id={id}
                className={controls.input}
                value={projectId}
                onChange={(e) => setProjectId(e.target.value)}
                required
              >
                <option value="">—</option>
                {projects?.map((project) => (
                  <option key={project.id} value={project.id}>
                    {project.name}
                  </option>
                ))}
              </select>
            )}
          </Field>
        )}

        <div className={styles.fieldRow}>
          <Field label={t('goals.startValue')}>
            {({ id }) => (
              <Input id={id} type="number" value={startValue} onChange={(e) => setStartValue(e.target.value)} />
            )}
          </Field>
          <Field label={t('goals.targetValue')}>
            {({ id }) => (
              <Input id={id} type="number" value={targetValue} onChange={(e) => setTargetValue(e.target.value)} />
            )}
          </Field>
          <Field label={t('goals.unit')}>
            {({ id }) => <Input id={id} value={unit} onChange={(e) => setUnit(e.target.value)} placeholder="%" />}
          </Field>
        </div>
      </form>
    </Dialog>
  );
}
