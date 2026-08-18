import { useEffect, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { PageHeader } from '../app/AppShell';
import { Button } from '../ui/Button';
import { Card, EmptyState } from '../ui/display';
import { Spinner } from '../ui/Spinner';
import { useToast } from '../ui/Toast';
import { Trash } from '../ui/icons';
import { ProjectPicker } from '../components/ProjectPicker';
import { errorMessage } from '../lib/api';
import { useClipUrl, useClips, useDeleteClip, useUploadClip } from '../lib/canvas';
import { formatDateTime } from '../lib/format';
import styles from './whiteboard.module.css';
import clips from './clips.module.css';

/**
 * Clips: a screen recording, made here and kept with the work it is about.
 *
 * The recording happens entirely in the browser. `getDisplayMedia` and
 * `MediaRecorder` are what every screen-recording tool on the web is built on,
 * and the alternative — uploading raw frames for the server to encode — would
 * mean shipping a transcoder to do worse what the browser already does well.
 *
 * The bytes then take the attachment path: object storage, and a signed URL
 * issued after this server has checked who is asking. Nothing about a clip is
 * public, and the recording itself never touches this application's disk.
 */
export default function ClipsPage() {
  const { t } = useTranslation();
  const toast = useToast();
  const [projectId, setProjectId] = useState<string>();
  const [playing, setPlaying] = useState<{ id: string; url: string }>();

  const list = useClips(projectId);
  const upload = useUploadClip(projectId ?? '');
  const remove = useDeleteClip(projectId ?? '');
  const signedUrl = useClipUrl();

  const [recording, setRecording] = useState(false);
  const [elapsed, setElapsed] = useState(0);
  const recorderRef = useRef<MediaRecorder>();
  const chunksRef = useRef<Blob[]>([]);
  const startedAt = useRef(0);

  // Whatever happens, the capture is released. A recorder left running is the
  // browser telling somebody all day that this tab is watching their screen.
  useEffect(
    () => () => {
      recorderRef.current?.stream.getTracks().forEach((track) => track.stop());
    },
    []
  );

  useEffect(() => {
    if (!recording) return;
    const timer = window.setInterval(() => setElapsed(Date.now() - startedAt.current), 500);
    return () => window.clearInterval(timer);
  }, [recording]);

  const supported = typeof navigator !== 'undefined' && Boolean(navigator.mediaDevices?.getDisplayMedia);

  async function startRecording() {
    if (!projectId) return;
    try {
      const stream = await navigator.mediaDevices.getDisplayMedia({ video: true, audio: true });
      const recorder = new MediaRecorder(stream, pickMimeType());
      chunksRef.current = [];
      startedAt.current = Date.now();

      recorder.ondataavailable = (event) => {
        if (event.data.size > 0) chunksRef.current.push(event.data);
      };
      recorder.onstop = () => {
        stream.getTracks().forEach((track) => track.stop());
        const duration = Date.now() - startedAt.current;
        const blob = new Blob(chunksRef.current, { type: recorder.mimeType || 'video/webm' });
        setRecording(false);
        setElapsed(0);
        if (blob.size === 0) return;

        upload.mutate(
          { blob, title: t('clips.defaultTitle', { at: formatDateTime(new Date().toISOString()) }), durationMs: duration },
          {
            onSuccess: () => toast.success(t('clips.uploaded')),
            onError: (error) => toast.error(errorMessage(error, t('errors.genericBody')))
          }
        );
      };

      // Stopping the share from the browser's own bar has to stop the
      // recording too, or the tab keeps recording a screen nobody is sharing.
      stream.getVideoTracks()[0]?.addEventListener('ended', () => recorder.state !== 'inactive' && recorder.stop());

      recorder.start(1000);
      recorderRef.current = recorder;
      setRecording(true);
    } catch (error) {
      // Refusing the share is a normal answer, not a failure worth a red toast.
      if (error instanceof DOMException && error.name === 'NotAllowedError') return;
      toast.error(errorMessage(error, t('errors.genericBody')));
    }
  }

  return (
    <>
      <PageHeader
        title={t('clips.title')}
        description={t('clips.hint')}
        actions={
          recording ? (
            <Button variant="danger" onClick={() => recorderRef.current?.stop()}>
              {t('clips.stop', { seconds: Math.round(elapsed / 1000) })}
            </Button>
          ) : (
            <Button
              variant="primary"
              disabled={!projectId || !supported || upload.isPending}
              loading={upload.isPending}
              onClick={() => void startRecording()}
            >
              {t('clips.record')}
            </Button>
          )
        }
      />

      <div className={styles.toolbarRow}>
        <ProjectPicker value={projectId} onChange={setProjectId} />
        {!supported && <span className={styles.muted}>{t('clips.unsupported')}</span>}
      </div>

      {playing && (
        <Card>
          {/* eslint-disable-next-line jsx-a11y/media-has-caption -- a screen
              recording has no transcript, and an empty track element would
              claim one exists. */}
          <video className={clips.player} src={playing.url} controls autoPlay />
        </Card>
      )}

      {list.isLoading ? (
        <Spinner label={t('common.loading')} />
      ) : !list.data?.length ? (
        <Card>
          <EmptyState title={t('clips.none')} body={t('clips.noneBody')} />
        </Card>
      ) : (
        <ul className={clips.list}>
          {list.data.map((clip) => (
            <li key={clip.id} className={clips.row}>
              <div>
                <strong>{clip.title}</strong>
                <div className={styles.muted}>
                  {formatDateTime(clip.createdAt)}
                  {clip.durationMs ? ` · ${Math.round(clip.durationMs / 1000)}s` : ''}
                  {` · ${Math.round(clip.sizeBytes / 1024 / 1024 * 10) / 10} MB`}
                </div>
              </div>
              <div className={styles.toolbarRow}>
                <Button
                  size="sm"
                  loading={signedUrl.isPending}
                  onClick={() =>
                    // Signed on demand rather than for every row: a link that
                    // expires in fifteen minutes is worth nothing if it was
                    // issued when the page loaded.
                    signedUrl.mutate(clip.id, {
                      onSuccess: (result) => setPlaying({ id: clip.id, url: result.url }),
                      onError: (error) => toast.error(errorMessage(error, t('errors.genericBody')))
                    })
                  }
                >
                  {t('clips.play')}
                </Button>
                <Button
                  size="sm"
                  variant="ghost"
                  aria-label={t('common.delete')}
                  onClick={() => {
                    if (!window.confirm(t('clips.confirmDelete'))) return;
                    remove.mutate(clip.id, {
                      onSuccess: () => setPlaying((current) => (current?.id === clip.id ? undefined : current)),
                      onError: (error) => toast.error(errorMessage(error, t('errors.genericBody')))
                    });
                  }}
                >
                  <Trash size={16} />
                </Button>
              </div>
            </li>
          ))}
        </ul>
      )}
    </>
  );
}

/**
 * The best container this browser will actually produce.
 *
 * VP9 where it exists, VP8 where it does not, and the browser's own default
 * where neither is offered — Safari records MP4 here. Asking for a codec the
 * browser cannot encode throws, so this is checked rather than assumed.
 */
function pickMimeType(): MediaRecorderOptions {
  const candidates = ['video/webm;codecs=vp9', 'video/webm;codecs=vp8', 'video/webm', 'video/mp4'];
  const supported = candidates.find((type) => MediaRecorder.isTypeSupported?.(type));
  return supported ? { mimeType: supported } : {};
}
