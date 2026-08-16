import { useTranslation } from 'react-i18next';
import { Button } from '../ui/Button';
import { Badge } from '../ui/display';
import { Field, Input } from '../ui/Field';
import { useToast } from '../ui/Toast';
import { errorMessage } from '../lib/api';
import { formatDate } from '../lib/format';
import {
  useClearRecurrence,
  useRecurrence,
  useSetRecurrence,
  type RecurrenceFrequency,
  type RecurrenceMode
} from '../lib/queries';
import controls from '../ui/controls.module.css';
import type { Task } from '../types';

const FREQUENCIES: RecurrenceFrequency[] = ['daily', 'weekly', 'monthly'];

/**
 * Whether a task comes back, and what decides when.
 *
 * The mode is the part worth an explanation on screen rather than in a tooltip,
 * because the two behave very differently when nobody does the work:
 * "when completed" quietly stops, and "on schedule" keeps producing instances
 * that go overdue. Both are defensible; picking one by accident is not.
 */
export default function RecurrenceSection({ task }: { task: Task }) {
  const { t } = useTranslation();
  const toast = useToast();

  const { data: rule } = useRecurrence(task.id);
  const setRecurrence = useSetRecurrence();
  const clearRecurrence = useClearRecurrence();

  const frequency = rule?.frequency ?? 'weekly';
  const interval = rule?.intervalCount ?? 1;
  const mode: RecurrenceMode = rule?.mode ?? 'on_complete';

  function save(patch: { frequency?: RecurrenceFrequency; intervalCount?: number; mode?: RecurrenceMode }) {
    setRecurrence.mutate(
      { taskId: task.id, frequency, intervalCount: interval, mode, ...patch },
      { onError: (err) => toast.error(errorMessage(err, t('errors.genericBody'))) }
    );
  }

  return (
    <section>
      <h4 className={controls.label}>{t('recurrence.title')}</h4>

      {!rule ? (
        <div style={{ display: 'flex', gap: 'var(--space-2)', alignItems: 'center', flexWrap: 'wrap' }}>
          <p className={controls.hint} style={{ margin: 0 }}>
            {t('recurrence.notRepeating')}
          </p>
          <Button
            type="button"
            variant="secondary"
            size="sm"
            loading={setRecurrence.isPending}
            onClick={() => save({})}
          >
            {t('recurrence.enable')}
          </Button>
        </div>
      ) : (
        <div style={{ display: 'grid', gap: 'var(--space-3)' }}>
          <div style={{ display: 'flex', gap: 'var(--space-2)', flexWrap: 'wrap', alignItems: 'flex-end' }}>
            <Field label={t('recurrence.every')}>
              {({ id }) => (
                <Input
                  id={id}
                  type="number"
                  min={1}
                  max={52}
                  value={interval}
                  style={{ width: 90 }}
                  onChange={(event) => {
                    const next = Number(event.target.value);
                    if (next >= 1 && next <= 52) save({ intervalCount: next });
                  }}
                />
              )}
            </Field>

            <div style={{ display: 'flex', gap: 'var(--space-1)' }}>
              {FREQUENCIES.map((option) => (
                <Button
                  key={option}
                  type="button"
                  size="sm"
                  variant={frequency === option ? 'primary' : 'secondary'}
                  aria-pressed={frequency === option}
                  onClick={() => save({ frequency: option })}
                >
                  {t(`recurrence.${option}`)}
                </Button>
              ))}
            </div>

            <Button
              type="button"
              variant="dangerGhost"
              size="sm"
              style={{ marginLeft: 'auto' }}
              loading={clearRecurrence.isPending}
              onClick={() =>
                clearRecurrence.mutate(task.id, {
                  onSuccess: () => toast.success(t('recurrence.stopped'))
                })
              }
            >
              {t('recurrence.stop')}
            </Button>
          </div>

          <fieldset style={{ border: 'none', padding: 0, margin: 0 }}>
            <legend className={controls.label} style={{ marginBottom: 'var(--space-2)' }}>
              {t('recurrence.whenLabel')}
            </legend>
            <div style={{ display: 'grid', gap: 'var(--space-2)' }}>
              {(['on_complete', 'on_schedule'] as RecurrenceMode[]).map((option) => (
                <label
                  key={option}
                  style={{ display: 'flex', gap: 'var(--space-2)', alignItems: 'flex-start', cursor: 'pointer' }}
                >
                  <input
                    type="radio"
                    name={`recurrence-mode-${task.id}`}
                    checked={mode === option}
                    onChange={() => save({ mode: option })}
                    style={{ marginTop: 4 }}
                  />
                  <span>
                    <span style={{ fontSize: 'var(--text-sm)', fontWeight: 500 }}>
                      {t(`recurrence.mode_${option}`)}
                    </span>
                    {/* The consequence, not the label. This is the sentence
                        that stops somebody choosing the wrong one. */}
                    <span className={controls.hint} style={{ display: 'block' }}>
                      {t(`recurrence.mode_${option}_hint`)}
                    </span>
                  </span>
                </label>
              ))}
            </div>
          </fieldset>

          <div style={{ display: 'flex', gap: 'var(--space-2)', alignItems: 'center', flexWrap: 'wrap' }}>
            <Badge>{t('recurrence.occurrence', { count: rule.occurrences })}</Badge>
            {rule.nextRunAt && <Badge tone="accent">{t('recurrence.next', { date: formatDate(rule.nextRunAt) })}</Badge>}
          </div>
        </div>
      )}
    </section>
  );
}
