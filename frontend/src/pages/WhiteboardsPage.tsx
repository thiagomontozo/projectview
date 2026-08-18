import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { PageHeader } from '../app/AppShell';
import { Button } from '../ui/Button';
import { Card, EmptyState } from '../ui/display';
import { Input } from '../ui/Field';
import { Spinner } from '../ui/Spinner';
import { useToast } from '../ui/Toast';
import { Layers, Plus, Trash } from '../ui/icons';
import { ProjectPicker } from '../components/ProjectPicker';
import { errorMessage, statusOf } from '../lib/api';
import {
  useCreateWhiteboard,
  useDeleteWhiteboard,
  useSaveWhiteboard,
  useWhiteboard,
  useWhiteboards
} from '../lib/canvas';
import styles from './whiteboard.module.css';
import type { BoardItem, BoardScene } from '../types';

/**
 * Whiteboards.
 *
 * A working surface for the thinking that happens before there are tasks: notes
 * moved around, boxes drawn, arrows between them. Deliberately small — this is
 * not a design tool, and pretending otherwise would mean a year of work to be
 * worse at it than the tools people already have.
 *
 * The scene is one document, saved whole, against the version it was opened at.
 * Two people on one board is the ordinary case, so a save that would overwrite
 * somebody else's is refused and said out loud rather than applied quietly.
 */

type Tool = 'select' | 'note' | 'text' | 'rect' | 'ellipse' | 'arrow';

const COLORS = ['#fde68a', '#bfdbfe', '#bbf7d0', '#fecaca', '#e9d5ff', '#e2e8f0'];

export default function WhiteboardsPage() {
  const { t } = useTranslation();
  const [projectId, setProjectId] = useState<string>();
  const [openId, setOpenId] = useState<string>();

  const boards = useWhiteboards(projectId);
  const create = useCreateWhiteboard(projectId ?? '');
  const remove = useDeleteWhiteboard(projectId ?? '');
  const toast = useToast();

  // Opening a board in another project would show somebody a board they then
  // could not save, so the selection is dropped with the project.
  useEffect(() => setOpenId(undefined), [projectId]);

  if (openId) {
    return <BoardEditor boardId={openId} projectId={projectId ?? ''} onClose={() => setOpenId(undefined)} />;
  }

  return (
    <>
      <PageHeader
        title={t('canvas.whiteboards')}
        description={t('canvas.whiteboardsHint')}
        actions={
          <Button
            variant="primary"
            loading={create.isPending}
            disabled={!projectId}
            onClick={() =>
              create.mutate(t('canvas.untitledBoard'), {
                onSuccess: (board) => setOpenId(board.id),
                onError: (error) => toast.error(errorMessage(error, t('errors.genericBody')))
              })
            }
          >
            <Plus size={16} />
            {t('canvas.newBoard')}
          </Button>
        }
      />

      <div className={styles.toolbarRow}>
        <ProjectPicker value={projectId} onChange={setProjectId} />
      </div>

      {boards.isLoading ? (
        <Spinner label={t('common.loading')} />
      ) : !boards.data?.length ? (
        <Card>
          <EmptyState
            icon={<Layers size={28} />}
            title={t('canvas.noBoards')}
            body={t('canvas.noBoardsBody')}
          />
        </Card>
      ) : (
        <div className={styles.grid}>
          {boards.data.map((board) => (
            <Card key={board.id} interactive>
              <button type="button" className={styles.cardOpen} onClick={() => setOpenId(board.id)}>
                <strong>{board.title}</strong>
                <span className={styles.muted}>
                  {t('canvas.revision', { version: board.version })}
                </span>
              </button>
              <Button
                size="sm"
                variant="ghost"
                aria-label={t('common.delete')}
                onClick={() => {
                  if (!window.confirm(t('canvas.confirmDeleteBoard'))) return;
                  remove.mutate(board.id, {
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

function BoardEditor({
  boardId,
  projectId,
  onClose
}: {
  boardId: string;
  projectId: string;
  onClose: () => void;
}) {
  const { t } = useTranslation();
  const toast = useToast();
  const board = useWhiteboard(boardId);
  const save = useSaveWhiteboard(projectId);

  const [items, setItems] = useState<BoardItem[]>([]);
  const [version, setVersion] = useState(0);
  const [title, setTitle] = useState('');
  const [tool, setTool] = useState<Tool>('select');
  const [color, setColor] = useState(COLORS[0]);
  const [selected, setSelected] = useState<string>();
  const [editing, setEditing] = useState<string>();
  const [dirty, setDirty] = useState(false);
  const [pan, setPan] = useState({ x: 0, y: 0, scale: 1 });

  const surfaceRef = useRef<HTMLDivElement>(null);
  const dragRef = useRef<{ id: string; dx: number; dy: number; resize: boolean } | null>(null);

  // Loaded once. Re-syncing from the query on every render would fight whoever
  // is dragging a note at that moment.
  useEffect(() => {
    if (!board.data) return;
    setItems((board.data.scene?.items as BoardItem[]) ?? []);
    setVersion(board.data.version);
    setTitle(board.data.title);
  }, [board.data?.id]);

  const commit = useCallback(
    (next: BoardItem[]) => {
      setItems(next);
      setDirty(true);
    },
    []
  );

  function pointOf(event: { clientX: number; clientY: number }) {
    const rect = surfaceRef.current?.getBoundingClientRect();
    if (!rect) return { x: 0, y: 0 };
    return {
      x: (event.clientX - rect.left - pan.x) / pan.scale,
      y: (event.clientY - rect.top - pan.y) / pan.scale
    };
  }

  function addItem(kind: Exclude<Tool, 'select'>, at: { x: number; y: number }) {
    const defaults: Record<string, { w: number; h: number }> = {
      note: { w: 160, h: 120 },
      text: { w: 200, h: 40 },
      rect: { w: 180, h: 110 },
      ellipse: { w: 150, h: 150 },
      arrow: { w: 180, h: 0 }
    };
    const item: BoardItem = {
      id: crypto.randomUUID(),
      kind,
      x: Math.round(at.x),
      y: Math.round(at.y),
      ...defaults[kind],
      text: kind === 'note' || kind === 'text' ? '' : undefined,
      color
    };
    commit([...items, item]);
    setSelected(item.id);
    if (item.kind === 'note' || item.kind === 'text') setEditing(item.id);
    setTool('select');
  }

  function onSurfacePointerDown(event: React.PointerEvent) {
    // The scene layer covers the surface, so the click almost never lands on
    // the element the handler is attached to. What actually matters is whether
    // it landed on an existing item - anything else is empty canvas.
    if ((event.target as Element).closest('[data-kind]')) return;
    setSelected(undefined);
    setEditing(undefined);
    if (tool !== 'select') {
      // The browser focuses whatever was pressed *after* React has mounted the
      // new note, which quietly took the focus back off the textarea - so the
      // first thing anybody typed into a new note went nowhere. Suppressing the
      // default focus is what keeps "click, then type" working.
      event.preventDefault();
      addItem(tool, pointOf(event));
    }
  }

  function onItemPointerDown(event: React.PointerEvent, item: BoardItem, resize = false) {
    if (tool !== 'select') return;
    event.stopPropagation();
    (event.target as Element).setPointerCapture(event.pointerId);
    const point = pointOf(event);
    dragRef.current = { id: item.id, dx: point.x - item.x, dy: point.y - item.y, resize };
    setSelected(item.id);
  }

  function onPointerMove(event: React.PointerEvent) {
    const drag = dragRef.current;
    if (!drag) return;
    const point = pointOf(event);
    setItems((current) =>
      current.map((item) => {
        if (item.id !== drag.id) return item;
        if (drag.resize) {
          // A floor rather than free resize: a note dragged to zero is a note
          // that has vanished with no way to get it back.
          return { ...item, w: Math.max(40, Math.round(point.x - item.x)), h: Math.max(24, Math.round(point.y - item.y)) };
        }
        return { ...item, x: Math.round(point.x - drag.dx), y: Math.round(point.y - drag.dy) };
      })
    );
    setDirty(true);
  }

  function onPointerUp() {
    dragRef.current = null;
  }

  function remove(id: string) {
    commit(items.filter((item) => item.id !== id));
    setSelected(undefined);
  }

  // Delete and Backspace remove the selection, unless somebody is typing into a
  // note — where Backspace means Backspace.
  useEffect(() => {
    function onKey(event: KeyboardEvent) {
      if (editing) return;
      const target = event.target as HTMLElement;
      if (target.tagName === 'INPUT' || target.tagName === 'TEXTAREA') return;
      if ((event.key === 'Delete' || event.key === 'Backspace') && selected) {
        event.preventDefault();
        remove(selected);
      }
      if (event.key === 'Escape') {
        setSelected(undefined);
        setTool('select');
      }
    }
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [selected, editing, items]);

  function persist() {
    const scene: BoardScene = { items };
    save.mutate(
      { id: boardId, scene, version, title },
      {
        onSuccess: (saved) => {
          setVersion(saved.version);
          setDirty(false);
          toast.success(t('canvas.saved'));
        },
        onError: (error) => {
          if (statusOf(error) === 409) {
            // Somebody else saved first. Their board is what exists now, so the
            // honest thing is to say so and offer to load it — not to overwrite
            // it and not to pretend the save worked.
            toast.error(t('canvas.conflict'));
            return;
          }
          toast.error(errorMessage(error, t('errors.genericBody')));
        }
      }
    );
  }

  const arrows = useMemo(() => items.filter((item) => item.kind === 'arrow'), [items]);

  if (board.isLoading) return <Spinner label={t('common.loading')} />;

  return (
    <>
      <PageHeader
        title={title || t('canvas.untitledBoard')}
        actions={
          <>
            <Button variant="ghost" onClick={onClose}>
              {t('canvas.backToBoards')}
            </Button>
            <Button variant="primary" loading={save.isPending} disabled={!dirty} onClick={persist}>
              {dirty ? t('common.save') : t('canvas.savedShort')}
            </Button>
          </>
        }
      />

      <div className={styles.toolbarRow}>
        <Input
          aria-label={t('canvas.boardTitle')}
          value={title}
          onChange={(event) => {
            setTitle(event.target.value);
            setDirty(true);
          }}
        />
        {(['select', 'note', 'text', 'rect', 'ellipse', 'arrow'] as const).map((option) => (
          <Button
            key={option}
            size="sm"
            variant={tool === option ? 'primary' : 'ghost'}
            onClick={() => setTool(option)}
          >
            {t(`canvas.tool_${option}`)}
          </Button>
        ))}
        <div className={styles.colors} role="group" aria-label={t('canvas.colour')}>
          {COLORS.map((option) => (
            <button
              key={option}
              type="button"
              className={styles.swatch}
              aria-label={option}
              aria-pressed={color === option}
              style={{ background: option, outline: color === option ? '2px solid var(--text)' : 'none' }}
              onClick={() => {
                setColor(option);
                if (selected) commit(items.map((i) => (i.id === selected ? { ...i, color: option } : i)));
              }}
            />
          ))}
        </div>
        <Button size="sm" variant="ghost" onClick={() => setPan((p) => ({ ...p, scale: Math.min(2, p.scale + 0.1) }))}>
          +
        </Button>
        <Button size="sm" variant="ghost" onClick={() => setPan((p) => ({ ...p, scale: Math.max(0.4, p.scale - 0.1) }))}>
          −
        </Button>
      </div>

      <div
        ref={surfaceRef}
        className={styles.surface}
        data-tool={tool}
        onPointerDown={onSurfacePointerDown}
        onPointerMove={onPointerMove}
        onPointerUp={onPointerUp}
        role="application"
        aria-label={t('canvas.whiteboards')}
      >
        <div
          className={styles.scene}
          style={{ transform: `translate(${pan.x}px, ${pan.y}px) scale(${pan.scale})` }}
        >
          {/* Arrows are the one shape a box cannot be: they need two points and
              a head, so they live in an SVG layer under the rest. */}
          <svg className={styles.arrowLayer} aria-hidden="true">
            <defs>
              <marker id="arrowhead" markerWidth="8" markerHeight="8" refX="7" refY="4" orient="auto">
                <path d="M0,0 L8,4 L0,8 Z" fill="currentColor" />
              </marker>
            </defs>
            {arrows.map((item) => (
              <line
                key={item.id}
                x1={item.x}
                y1={item.y}
                x2={item.x + item.w}
                y2={item.y + (item.h || 0)}
                stroke="currentColor"
                strokeWidth={2}
                markerEnd="url(#arrowhead)"
              />
            ))}
          </svg>

          {items
            .filter((item) => item.kind !== 'arrow')
            .map((item) => (
              <div
                key={item.id}
                className={styles.item}
                data-kind={item.kind}
                data-selected={item.id === selected}
                style={{
                  left: item.x,
                  top: item.y,
                  width: item.w,
                  height: item.h,
                  background: item.kind === 'text' ? 'transparent' : item.color
                }}
                onPointerDown={(event) => onItemPointerDown(event, item)}
                onDoubleClick={() => (item.kind === 'note' || item.kind === 'text') && setEditing(item.id)}
              >
                {editing === item.id ? (
                  <textarea
                    className={styles.itemInput}
                    // Focused from the ref rather than by autoFocus, for the
                    // same reason: autoFocus runs on mount, and the pointer
                    // event that created this note focuses the canvas after it.
                    ref={(node) => node?.focus()}
                    value={item.text ?? ''}
                    onChange={(event) =>
                      commit(items.map((i) => (i.id === item.id ? { ...i, text: event.target.value } : i)))
                    }
                    onBlur={() => setEditing(undefined)}
                  />
                ) : (
                  <span className={styles.itemText}>{item.text}</span>
                )}

                {item.id === selected && (
                  <span
                    className={styles.resizeHandle}
                    onPointerDown={(event) => onItemPointerDown(event, item, true)}
                    aria-hidden="true"
                  />
                )}
              </div>
            ))}

          {arrows.map((item) => (
            <button
              key={`hit-${item.id}`}
              type="button"
              className={styles.arrowHandle}
              aria-label={t('canvas.tool_arrow')}
              data-selected={item.id === selected}
              style={{ left: item.x - 6, top: item.y - 6 }}
              onPointerDown={(event) => onItemPointerDown(event, item)}
            />
          ))}
        </div>

        {items.length === 0 && <p className={styles.hint}>{t('canvas.boardHint')}</p>}
      </div>
    </>
  );
}
