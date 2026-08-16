import { useRef, useState, type CSSProperties, type DragEvent } from 'react';
import { useTranslation } from 'react-i18next';
import { Button } from '../ui/Button';
import { Badge } from '../ui/display';
import { useToast } from '../ui/Toast';
import { Download, Paperclip, Trash } from '../ui/icons';
import { useAttachmentConfig, useAttachments, useDeleteAttachment, useUploadAttachment } from '../lib/queries';
import { errorMessage } from '../lib/api';
import { formatDate } from '../lib/format';
import controls from '../ui/controls.module.css';
import type { Attachment } from '../types';

/**
 * Files on a task.
 *
 * Uploads go through the API, which is what makes the size and type rules
 * enforceable. Downloads do not: following an attachment's URL redirects to a
 * short-lived signed link served by the object store, so the bytes never pass
 * through the application and a link that leaks stops working on its own.
 */
/**
 * Kept in the DOM rather than replaced by a button.
 *
 * A file input styled away is still focusable, still announced and still the
 * thing the browser's own file picker is wired to; a bare button with no input
 * behind it is none of those.
 */
const visuallyHidden: CSSProperties = {
  position: 'absolute',
  width: 1,
  height: 1,
  padding: 0,
  margin: -1,
  overflow: 'hidden',
  clip: 'rect(0 0 0 0)',
  whiteSpace: 'nowrap',
  border: 0
};

export default function Attachments({ taskId }: { taskId: string }) {
  const { t } = useTranslation();
  const toast = useToast();
  const inputRef = useRef<HTMLInputElement>(null);

  const { data: config } = useAttachmentConfig();
  const { data: attachments = [], isLoading } = useAttachments(taskId);
  const upload = useUploadAttachment();
  const remove = useDeleteAttachment();

  const [dragging, setDragging] = useState(false);
  const [progress, setProgress] = useState<number | null>(null);
  const [error, setError] = useState('');

  // The deployment may have no object store at all. Saying nothing at all
  // would be worse than saying so: somebody looking for the upload button
  // needs to know it is missing by configuration, not by accident.
  if (config && !config.enabled) {
    return (
      <section>
        <h4 className={controls.label}>{t('attachments.title')}</h4>
        <p className={controls.hint}>{t('attachments.unavailable')}</p>
      </section>
    );
  }

  const maxBytes = config?.maxBytes ?? 0;

  function send(files: FileList | null) {
    if (!files || files.length === 0) return;
    setError('');

    // One at a time and sequentially, so the progress figure means something
    // and a rejected file does not take the rest of the selection with it.
    const queue = Array.from(files);
    const next = (index: number) => {
      if (index >= queue.length) {
        setProgress(null);
        return;
      }
      const file = queue[index];

      // Checked here as well as on the server. Not as a security measure - the
      // server's answer is the one that counts - but because refusing a 300 MB
      // file instantly beats refusing it after two minutes of uploading.
      if (maxBytes > 0 && file.size > maxBytes) {
        setError(t('attachments.tooLarge', { name: file.name, limit: formatBytes(maxBytes) }));
        next(index + 1);
        return;
      }

      upload.mutate(
        { taskId, file, onProgress: setProgress },
        {
          onSuccess: () => next(index + 1),
          onError: (err) => {
            setError(errorMessage(err, t('errors.genericBody')));
            setProgress(null);
          }
        }
      );
    };
    next(0);
  }

  function onDrop(event: DragEvent<HTMLDivElement>) {
    event.preventDefault();
    setDragging(false);
    send(event.dataTransfer.files);
  }

  return (
    <section>
      <h4 className={controls.label}>{t('attachments.title')}</h4>

      <div
        onDragOver={(event) => {
          event.preventDefault();
          setDragging(true);
        }}
        onDragLeave={() => setDragging(false)}
        onDrop={onDrop}
        style={{
          border: `1px dashed ${dragging ? 'var(--accent)' : 'var(--border)'}`,
          background: dragging ? 'var(--surface-hover)' : 'transparent',
          borderRadius: 'var(--radius-md)',
          padding: 'var(--space-4)',
          textAlign: 'center',
          transition: 'background 120ms, border-color 120ms'
        }}
      >
        {/* The input stays in the DOM and is merely visually hidden: replacing
            it with a button that has no file input behind it would make the
            control unreachable from the keyboard and invisible to a screen
            reader. */}
        <input
          ref={inputRef}
          type="file"
          multiple
          onChange={(event) => {
            send(event.target.files);
            // Cleared so choosing the same file twice fires a change event the
            // second time as well.
            event.target.value = '';
          }}
          style={visuallyHidden}
          id={`attach-${taskId}`}
          aria-describedby={`attach-hint-${taskId}`}
        />
        <Button type="button" variant="secondary" size="sm" onClick={() => inputRef.current?.click()}>
          <Paperclip size={15} />
          {t('attachments.choose')}
        </Button>
        <p id={`attach-hint-${taskId}`} className={controls.hint} style={{ marginTop: 'var(--space-2)' }}>
          {t('attachments.dropHint', { limit: formatBytes(maxBytes) })}
        </p>
      </div>

      {progress !== null && (
        <div style={{ marginTop: 'var(--space-2)' }}>
          <progress value={progress} max={100} style={{ width: '100%' }} aria-label={t('attachments.uploading')} />
        </div>
      )}

      {error && (
        <p role="alert" style={{ color: 'var(--danger)', fontSize: 'var(--text-sm)', marginTop: 'var(--space-2)' }}>
          {error}
        </p>
      )}

      {/* Only the files attached to the task itself. The ones belonging to a
          comment are rendered under that comment, where they were said. */}
      {!isLoading && <AttachmentList items={attachments.filter((a) => !a.commentId)} taskId={taskId} />}
    </section>
  );
}

/**
 * Renders a set of attachments and owns the delete action.
 *
 * Shared by the task section and by each comment, so a file looks and behaves
 * the same wherever it is shown.
 */
export function AttachmentList({ items, taskId }: { items: Attachment[]; taskId: string }) {
  const { t } = useTranslation();
  const toast = useToast();
  const remove = useDeleteAttachment();

  if (items.length === 0) return null;

  return (
    <ul style={{ listStyle: 'none', padding: 0, margin: 'var(--space-2) 0 0', display: 'grid', gap: 'var(--space-2)' }}>
      {items.map((attachment) => (
        <AttachmentRow
          key={attachment.id}
          attachment={attachment}
          onDelete={() =>
            remove.mutate(
              { id: attachment.id, taskId },
              {
                onSuccess: () => toast.success(t('attachments.deleted')),
                onError: (err) => toast.error(errorMessage(err, t('errors.genericBody')))
              }
            )
          }
        />
      ))}
    </ul>
  );
}

/**
 * Files chosen alongside a comment that is not written yet.
 *
 * The comment has to exist before anything can hang off it, so this collects
 * the selection and hands it back; TaskModal posts the comment, learns the new
 * id and uploads against it. Attaching to the task and then re-pointing the row
 * would leave a file orphaned on the task whenever the comment failed to save.
 */
export function CommentAttachmentPicker({
  files,
  onChange
}: {
  files: File[];
  onChange: (files: File[]) => void;
}) {
  const { t } = useTranslation();
  const { data: config } = useAttachmentConfig();
  const inputRef = useRef<HTMLInputElement>(null);

  if (config && !config.enabled) return null;

  return (
    <div style={{ display: 'flex', alignItems: 'center', gap: 'var(--space-2)', flexWrap: 'wrap' }}>
      <input
        ref={inputRef}
        type="file"
        multiple
        onChange={(event) => {
          onChange([...files, ...Array.from(event.target.files ?? [])]);
          event.target.value = '';
        }}
        style={visuallyHidden}
      />
      <Button
        type="button"
        variant="ghost"
        size="sm"
        onClick={() => inputRef.current?.click()}
        aria-label={t('attachments.attachToComment')}
        title={t('attachments.attachToComment')}
      >
        <Paperclip size={15} />
      </Button>

      {files.map((file, index) => (
        <Badge key={`${file.name}-${index}`}>
          {file.name} ({formatBytes(file.size)})
          <button
            type="button"
            onClick={() => onChange(files.filter((_, i) => i !== index))}
            aria-label={t('attachments.delete', { name: file.name })}
            style={{
              marginLeft: 'var(--space-1)',
              background: 'none',
              border: 'none',
              color: 'inherit',
              cursor: 'pointer',
              padding: 0
            }}
          >
            ×
          </button>
        </Badge>
      ))}
    </div>
  );
}

function AttachmentRow({ attachment, onDelete }: { attachment: Attachment; onDelete: () => void }) {
  const { t } = useTranslation();
  const isImage = attachment.inline && attachment.contentType.startsWith('image/');
  // A file the scanner flagged, or has not reached yet, has no working
  // download: the server refuses to sign a URL for it. Offering the link
  // anyway would just produce an error on click.
  const downloadable = attachment.scanStatus === 'clean' || attachment.scanStatus === 'skipped';

  return (
    <li
      style={{
        display: 'flex',
        alignItems: 'center',
        gap: 'var(--space-3)',
        padding: 'var(--space-2)',
        border: '1px solid var(--border)',
        borderRadius: 'var(--radius-md)'
      }}
    >
      {isImage && downloadable ? (
        // The session cookie authenticates this request, so the thumbnail
        // works without any JavaScript fetching and re-wrapping the bytes.
        <img
          src={attachment.downloadUrl}
          alt=""
          loading="lazy"
          style={{ width: 40, height: 40, objectFit: 'cover', borderRadius: 'var(--radius-sm)', flexShrink: 0 }}
        />
      ) : (
        <span
          aria-hidden="true"
          style={{
            width: 40,
            height: 40,
            display: 'grid',
            placeItems: 'center',
            background: 'var(--surface-hover)',
            borderRadius: 'var(--radius-sm)',
            flexShrink: 0,
            color: 'var(--text-secondary)'
          }}
        >
          <Paperclip size={18} />
        </span>
      )}

      <div style={{ minWidth: 0, flex: 1 }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 'var(--space-2)' }}>
          <span
            style={{
              fontSize: 'var(--text-sm)',
              fontWeight: 500,
              overflow: 'hidden',
              textOverflow: 'ellipsis',
              whiteSpace: 'nowrap'
            }}
            title={attachment.filename}
          >
            {attachment.filename}
          </span>
          {attachment.scanStatus === 'infected' && <Badge tone="danger">{t('attachments.infected')}</Badge>}
          {attachment.scanStatus === 'pending' && <Badge>{t('attachments.scanning')}</Badge>}
        </div>
        <span style={{ fontSize: 'var(--text-xs)', color: 'var(--text-secondary)' }}>
          {formatBytes(attachment.sizeBytes)}
          {attachment.uploadedBy ? ` · ${attachment.uploadedBy.name}` : ''} · {formatDate(attachment.createdAt)}
        </span>
      </div>

      {downloadable && (
        <a
          href={attachment.downloadUrl}
          // Not "download": the server decides between showing an image in
          // place and saving a document, through the Content-Disposition it
          // signs into the link. Forcing it here would override that.
          target="_blank"
          rel="noreferrer"
          className={controls.iconLink}
          aria-label={t('attachments.download', { name: attachment.filename })}
          title={t('attachments.download', { name: attachment.filename })}
        >
          <Download size={16} />
        </a>
      )}

      <Button
        type="button"
        variant="dangerGhost"
        size="sm"
        onClick={onDelete}
        aria-label={t('attachments.delete', { name: attachment.filename })}
        title={t('attachments.delete', { name: attachment.filename })}
      >
        <Trash size={16} />
      </Button>
    </li>
  );
}

/**
 * Sizes as a person reads them.
 *
 * Binary units, because that is what the server's limits are expressed in —
 * showing a 25 MiB ceiling as "26.2 MB" and then refusing a 26 MB file is the
 * kind of small inconsistency that costs somebody ten minutes.
 */
export function formatBytes(bytes: number): string {
  if (!bytes) return '0 B';
  const units = ['B', 'kB', 'MB', 'GB'];
  const exponent = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1);
  const value = bytes / 1024 ** exponent;
  // One decimal at most, and none when it would be a trailing zero: the
  // configured ceiling is a round 25 MB, and showing it as "25.0 MB" makes a
  // limit look like a measurement.
  const rounded = value >= 100 || exponent === 0 ? Math.round(value) : Number(value.toFixed(1));
  return `${rounded} ${units[exponent]}`;
}
