import { useEffect, useMemo, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { PageHeader } from '../app/AppShell';
import { Button } from '../ui/Button';
import { Card, EmptyState } from '../ui/display';
import { Input } from '../ui/Field';
import { Spinner } from '../ui/Spinner';
import { useToast } from '../ui/Toast';
import { Table as TableIcon, Trash } from '../ui/icons';
import { ProjectPicker } from '../components/ProjectPicker';
import { errorMessage, statusOf } from '../lib/api';
import { useCreateSheet, useDeleteSheet, useSaveSheet, useSheet, useSheets } from '../lib/canvas';
import { cellRef, columnName, evaluateSheet, formatValue } from '../lib/formula';
import styles from './whiteboard.module.css';
import grid from './sheets.module.css';
import type { SheetCell } from '../types';

/**
 * Spreadsheets.
 *
 * The thing people leave a project tool to do and then never bring back: a
 * column of numbers with a total under it, kept beside the work it describes
 * instead of in an attachment somebody has to download to read.
 *
 * Small on purpose. The formula engine is written here rather than pulled in —
 * see lib/formula.ts for why — and what it cannot do it says, rather than
 * silently reading an unsupported function as zero.
 */
export default function SheetsPage() {
  const { t } = useTranslation();
  const toast = useToast();
  const [projectId, setProjectId] = useState<string>();
  const [openId, setOpenId] = useState<string>();

  const sheets = useSheets(projectId);
  const create = useCreateSheet(projectId ?? '');
  const remove = useDeleteSheet(projectId ?? '');

  useEffect(() => setOpenId(undefined), [projectId]);

  if (openId) {
    return <SheetEditor sheetId={openId} projectId={projectId ?? ''} onClose={() => setOpenId(undefined)} />;
  }

  return (
    <>
      <PageHeader
        title={t('sheets.title')}
        description={t('sheets.hint')}
        actions={
          <Button
            variant="primary"
            disabled={!projectId}
            loading={create.isPending}
            onClick={() =>
              create.mutate(t('sheets.untitled'), {
                onSuccess: (sheet) => setOpenId(sheet.id),
                onError: (error) => toast.error(errorMessage(error, t('errors.genericBody')))
              })
            }
          >
            {t('sheets.new')}
          </Button>
        }
      />

      <div className={styles.toolbarRow}>
        <ProjectPicker value={projectId} onChange={setProjectId} />
      </div>

      {sheets.isLoading ? (
        <Spinner label={t('common.loading')} />
      ) : !sheets.data?.length ? (
        <Card>
          <EmptyState icon={<TableIcon size={28} />} title={t('sheets.none')} body={t('sheets.noneBody')} />
        </Card>
      ) : (
        <div className={styles.grid}>
          {sheets.data.map((sheet) => (
            <Card key={sheet.id} interactive>
              <button type="button" className={styles.cardOpen} onClick={() => setOpenId(sheet.id)}>
                <strong>{sheet.title}</strong>
                <span className={styles.muted}>{t('canvas.revision', { version: sheet.version })}</span>
              </button>
              <Button
                size="sm"
                variant="ghost"
                aria-label={t('common.delete')}
                onClick={() => {
                  if (!window.confirm(t('sheets.confirmDelete'))) return;
                  remove.mutate(sheet.id, {
                    onError: (error) => toast.error(errorMessage(error, t('errors.genericBody')))
                  });
                }}
              >
                <Trash size={16} />
              </Button>
            </Card>
          ))}
        </div>
      )}
    </>
  );
}

function SheetEditor({
  sheetId,
  projectId,
  onClose
}: {
  sheetId: string;
  projectId: string;
  onClose: () => void;
}) {
  const { t } = useTranslation();
  const toast = useToast();
  const sheet = useSheet(sheetId);
  const save = useSaveSheet(projectId);

  const [cells, setCells] = useState<Record<string, SheetCell>>({});
  const [title, setTitle] = useState('');
  const [version, setVersion] = useState(0);
  const [rows, setRows] = useState(30);
  const [cols, setCols] = useState(10);
  const [active, setActive] = useState('A1');
  const [draft, setDraft] = useState('');
  const [dirty, setDirty] = useState(false);
  const editorRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    if (!sheet.data) return;
    setCells(sheet.data.cells ?? {});
    setTitle(sheet.data.title);
    setVersion(sheet.data.version);
    setRows(Math.max(30, sheet.data.rows));
    setCols(Math.max(10, sheet.data.cols));
  }, [sheet.data?.id]);

  // Every cell at once, with one cache. Per-cell evaluation on render would
  // re-walk the same dependency chain for every row on screen.
  const values = useMemo(() => evaluateSheet(cells), [cells]);

  function select(ref: string) {
    setActive(ref);
    const cell = cells[ref];
    // The formula bar shows the formula, the grid shows the result. Showing the
    // result in both would mean editing a cell replaced its formula with its
    // own answer — the classic way a spreadsheet eats somebody's work.
    setDraft(cell?.f ?? cell?.v ?? '');
  }

  function commitDraft(ref: string, raw: string) {
    const next = { ...cells };
    const trimmed = raw.trim();
    if (trimmed === '') {
      delete next[ref];
    } else if (trimmed.startsWith('=')) {
      next[ref] = { f: trimmed };
    } else {
      next[ref] = { v: raw };
    }
    setCells(next);
    setDirty(true);
  }

  function persist() {
    save.mutate(
      { id: sheetId, cells, version, title, rows, cols },
      {
        onSuccess: (saved) => {
          setVersion(saved.version);
          setDirty(false);
          toast.success(t('canvas.saved'));
        },
        onError: (error) => {
          if (statusOf(error) === 409) {
            toast.error(t('sheets.conflict'));
            return;
          }
          toast.error(errorMessage(error, t('errors.genericBody')));
        }
      }
    );
  }

  function move(ref: string, dRow: number, dCol: number) {
    const match = /^([A-Z]+)([0-9]+)$/.exec(ref);
    if (!match) return;
    const row = Math.min(rows - 1, Math.max(0, Number(match[2]) - 1 + dRow));
    const col = Math.min(cols - 1, Math.max(0, letterIndex(match[1]) + dCol));
    select(cellRef(row, col));
  }

  if (sheet.isLoading) return <Spinner label={t('common.loading')} />;

  return (
    <>
      <PageHeader
        title={title || t('sheets.untitled')}
        actions={
          <>
            <Button variant="ghost" onClick={onClose}>
              {t('sheets.backToSheets')}
            </Button>
            <Button variant="primary" loading={save.isPending} disabled={!dirty} onClick={persist}>
              {dirty ? t('common.save') : t('canvas.savedShort')}
            </Button>
          </>
        }
      />

      <div className={styles.toolbarRow}>
        <Input
          aria-label={t('sheets.sheetTitle')}
          value={title}
          onChange={(event) => {
            setTitle(event.target.value);
            setDirty(true);
          }}
        />
        <span className={grid.activeRef}>{active}</span>
        <input
          ref={editorRef}
          className={grid.formulaBar}
          aria-label={t('sheets.formulaBar')}
          value={draft}
          placeholder={t('sheets.formulaHint')}
          onChange={(event) => setDraft(event.target.value)}
          onBlur={() => commitDraft(active, draft)}
          onKeyDown={(event) => {
            if (event.key === 'Enter') {
              commitDraft(active, draft);
              move(active, 1, 0);
            }
            if (event.key === 'Escape') select(active);
          }}
        />
        <Button size="sm" variant="ghost" onClick={() => setRows((r) => r + 10)}>
          {t('sheets.addRows')}
        </Button>
        <Button size="sm" variant="ghost" onClick={() => setCols((c) => c + 3)}>
          {t('sheets.addCols')}
        </Button>
      </div>

      <div className={grid.scroller}>
        <table className={grid.sheet}>
          <thead>
            <tr>
              <th scope="col" className={grid.corner} />
              {Array.from({ length: cols }, (_, col) => (
                <th key={col} scope="col">
                  {columnName(col)}
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {Array.from({ length: rows }, (_, row) => (
              <tr key={row}>
                <th scope="row" className={grid.rowHead}>
                  {row + 1}
                </th>
                {Array.from({ length: cols }, (_, col) => {
                  const ref = cellRef(row, col);
                  const value = formatValue(values[ref]);
                  const isError = typeof value === 'string' && value.startsWith('#');
                  return (
                    <td key={col}>
                      <button
                        type="button"
                        className={grid.cell}
                        data-active={ref === active}
                        data-error={isError}
                        data-numeric={typeof values[ref] === 'number'}
                        onClick={() => select(ref)}
                        onDoubleClick={() => {
                          select(ref);
                          editorRef.current?.focus();
                        }}
                      >
                        {value}
                      </button>
                    </td>
                  );
                })}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </>
  );
}

function letterIndex(letters: string): number {
  let index = 0;
  for (const char of letters) index = index * 26 + (char.charCodeAt(0) - 64);
  return index - 1;
}
