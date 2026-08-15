import { useEffect, useRef, useState, type FormEvent } from 'react';
import { useTranslation } from 'react-i18next';
import clsx from 'clsx';
import { PageHeader } from '../app/AppShell';
import { Avatar, Card, EmptyState, ErrorState } from '../ui/display';
import { Button } from '../ui/Button';
import { Input } from '../ui/Field';
import { SkeletonList } from '../ui/Skeleton';
import { Chat as ChatIcon } from '../ui/icons';
import { useChannels, useMessages, usePostMessage } from '../lib/queries';
import { useRealtime } from '../hooks/useRealtime';
import { formatRelative } from '../lib/format';
import styles from './pages.module.css';

export default function ChatPage() {
  const { t, i18n } = useTranslation();
  const { data: channels, isLoading: channelsLoading, isError, refetch } = useChannels();
  const [activeId, setActiveId] = useState<string>();

  const channelId = activeId ?? channels?.[0]?.id;
  const { data: messages, isLoading: messagesLoading } = useMessages(channelId);
  const postMessage = usePostMessage();

  const [draft, setDraft] = useState('');
  const listRef = useRef<HTMLDivElement>(null);

  // Incoming messages arrive over the WebSocket; the hook invalidates the
  // cached thread so the list refreshes without polling.
  useRealtime();

  useEffect(() => {
    // Keep the newest message in view as the thread grows.
    listRef.current?.scrollTo({ top: listRef.current.scrollHeight });
  }, [messages?.length, channelId]);

  function send(event: FormEvent) {
    event.preventDefault();
    const body = draft.trim();
    if (!body || !channelId) return;
    setDraft('');
    postMessage.mutate({ channelId, body });
  }

  const activeChannel = channels?.find((channel) => channel.id === channelId);

  return (
    <>
      <PageHeader title={t('chat.title')} />

      {isError && (
        <Card>
          <ErrorState title={t('errors.loadFailed')} onRetry={() => void refetch()} retryLabel={t('common.retry')} />
        </Card>
      )}

      {channelsLoading && <SkeletonList rows={4} height={48} label={t('common.loading')} />}

      {channels && (
        <div className={styles.chatLayout}>
          <Card padded={false}>
            <nav className={styles.channelList} aria-label={t('chat.channels')}>
              {channels.map((channel) => (
                <button
                  key={channel.id}
                  type="button"
                  className={clsx(styles.channelItem, channel.id === channelId && styles.channelItemActive)}
                  aria-current={channel.id === channelId ? 'true' : undefined}
                  onClick={() => setActiveId(channel.id)}
                >
                  {channel.name || channel.members.map((m) => m.name).join(', ')}
                </button>
              ))}
            </nav>
          </Card>

          <Card padded={false} className={styles.chatPane}>
            {!channelId && (
              <EmptyState icon={<ChatIcon size={22} />} title={t('chat.noChannel')} body={t('chat.noChannelBody')} />
            )}

            {channelId && (
              <>
                <div className={styles.messageList} ref={listRef}>
                  {messagesLoading && <SkeletonList rows={4} height={44} label={t('common.loading')} />}

                  {messages?.length === 0 && (
                    <EmptyState icon={<ChatIcon size={22} />} title={t('chat.empty')} body={t('chat.emptyBody')} />
                  )}

                  {messages?.map((message) => (
                    <article key={message.id} className={styles.message}>
                      <Avatar
                        name={message.author?.name ?? '?'}
                        color={message.author?.avatarColor}
                        size={30}
                      />
                      <div className={styles.messageBody}>
                        <div>
                          <span className={styles.messageAuthor}>{message.author?.name}</span>
                          <time className={styles.messageTime} dateTime={message.createdAt}>
                            {formatRelative(message.createdAt, i18n.language)}
                          </time>
                        </div>
                        <p className={styles.messageText}>{message.body}</p>
                      </div>
                    </article>
                  ))}
                </div>

                <form className={styles.composer} onSubmit={send}>
                  <Input
                    value={draft}
                    onChange={(event) => setDraft(event.target.value)}
                    placeholder={t('chat.messagePlaceholder')}
                    aria-label={t('chat.messagePlaceholder')}
                  />
                  <Button type="submit" variant="primary" disabled={!draft.trim()}>
                    {t('chat.send')}
                  </Button>
                </form>
              </>
            )}
          </Card>
        </div>
      )}
    </>
  );
}
