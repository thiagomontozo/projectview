import { useEffect, useMemo, useRef, useState, type FormEvent } from 'react';
import { useTranslation } from 'react-i18next';
import clsx from 'clsx';
import { PageHeader } from '../app/AppShell';
import { Avatar, Badge, Card, EmptyState, ErrorState } from '../ui/display';
import { Button } from '../ui/Button';
import { Input } from '../ui/Field';
import { SkeletonList } from '../ui/Skeleton';
import { Chat as ChatIcon } from '../ui/icons';
import {
  useChannels,
  useMessages,
  usePostMessage,
  usePresence,
  useReply,
  useThread,
  useToggleReaction,
  useUsers,
  type ChatMessageDetail
} from '../lib/queries';
import { useAuth } from '../lib/auth';
import { useRealtime, useTyping } from '../hooks/useRealtime';
import { formatRelative } from '../lib/format';
import styles from './pages.module.css';

/** The reactions offered on hover. A short list beats an emoji picker here. */
const QUICK_REACTIONS = ['👍', '🎉', '👀', '❤️'];

export default function ChatPage() {
  const { t, i18n } = useTranslation();
  const { user } = useAuth();
  const { data: channels, isLoading: channelsLoading, isError, refetch } = useChannels();
  const { data: onlineIds = [] } = usePresence();
  const { data: users = [] } = useUsers();

  const [activeId, setActiveId] = useState<string>();
  const [threadOf, setThreadOf] = useState<ChatMessageDetail | null>(null);

  const channelId = activeId ?? channels?.[0]?.id;
  const { data: messages, isLoading: messagesLoading } = useMessages(channelId);
  const postMessage = usePostMessage();

  const [draft, setDraft] = useState('');
  const listRef = useRef<HTMLDivElement>(null);

  useRealtime();
  const typing = useTyping(channelId, user?.id);

  const online = useMemo(() => new Set(onlineIds), [onlineIds]);

  useEffect(() => {
    listRef.current?.scrollTo({ top: listRef.current.scrollHeight });
  }, [messages?.length, channelId]);

  // Switching channels must not leave a stale "still typing" behind.
  useEffect(() => () => typing.stopTyping(), [channelId]);

  function send(event: FormEvent) {
    event.preventDefault();
    const body = draft.trim();
    if (!body || !channelId) return;
    setDraft('');
    typing.stopTyping();
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
        <div className={clsx(styles.chatLayout, threadOf && styles.chatLayoutWithThread)}>
          <Card padded={false}>
            <nav className={styles.channelList} aria-label={t('chat.channels')}>
              {channels.map((channel) => (
                <button
                  key={channel.id}
                  type="button"
                  className={clsx(styles.channelItem, channel.id === channelId && styles.channelItemActive)}
                  aria-current={channel.id === channelId ? 'true' : undefined}
                  onClick={() => {
                    setActiveId(channel.id);
                    setThreadOf(null);
                  }}
                >
                  {channel.name || channel.members.map((m) => m.name).join(', ')}
                </button>
              ))}
            </nav>

            {users.length > 0 && (
              <div className={styles.presenceList}>
                <span className={styles.presenceHeading}>{t('chat.online')}</span>
                {users
                  .filter((member) => online.has(member.id))
                  .map((member) => (
                    <span key={member.id} className={styles.presenceItem}>
                      <Avatar name={member.name} color={member.avatarColor} size={20} />
                      <span className={styles.presenceDot} aria-hidden="true" />
                      {member.name}
                    </span>
                  ))}
                {users.filter((member) => online.has(member.id)).length === 0 && (
                  <span className={styles.subtle}>{t('chat.nobodyOnline')}</span>
                )}
              </div>
            )}
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

                  {(messages as ChatMessageDetail[] | undefined)?.map((message) => (
                    <MessageRow
                      key={message.id}
                      message={message}
                      channelId={channelId}
                      locale={i18n.language}
                      online={online}
                      onOpenThread={() => setThreadOf(message)}
                    />
                  ))}
                </div>

                {typing.names.length > 0 && (
                  <p className={styles.typingLine} aria-live="polite">
                    {t('chat.typing', { names: typing.names.join(', '), count: typing.names.length })}
                  </p>
                )}

                <form className={styles.composer} onSubmit={send}>
                  <Input
                    value={draft}
                    onChange={(event) => {
                      setDraft(event.target.value);
                      typing.notifyTyping();
                    }}
                    onBlur={typing.stopTyping}
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

          {threadOf && channelId && (
            <ThreadPane
              parent={threadOf}
              channelId={channelId}
              locale={i18n.language}
              onClose={() => setThreadOf(null)}
            />
          )}
        </div>
      )}
    </>
  );
}

function MessageRow({
  message,
  channelId,
  locale,
  online,
  onOpenThread
}: {
  message: ChatMessageDetail;
  channelId: string;
  locale: string;
  online: Set<string>;
  onOpenThread: () => void;
}) {
  const { t } = useTranslation();
  const toggleReaction = useToggleReaction();

  return (
    <article className={styles.message}>
      <span className={styles.avatarWithPresence}>
        <Avatar name={message.author?.name ?? '?'} color={message.author?.avatarColor} size={30} />
        {message.author && online.has(message.author.id) && (
          <span className={styles.presenceDotSmall} aria-label={t('chat.online')} />
        )}
      </span>

      <div className={styles.messageBody}>
        <div>
          <span className={styles.messageAuthor}>{message.author?.name}</span>
          <time className={styles.messageTime} dateTime={message.createdAt}>
            {formatRelative(message.createdAt, locale)}
          </time>
        </div>

        <p className={styles.messageText}>{message.body}</p>

        <div className={styles.messageActions}>
          {message.reactions?.map((reaction) => (
            <button
              key={reaction.emoji}
              type="button"
              className={styles.reactionChip}
              onClick={() => toggleReaction.mutate({ messageId: message.id, emoji: reaction.emoji, channelId })}
              aria-label={t('chat.reactionCount', { emoji: reaction.emoji, count: reaction.users.length })}
            >
              {reaction.emoji} {reaction.users.length}
            </button>
          ))}

          <span className={styles.quickReactions}>
            {QUICK_REACTIONS.map((emoji) => (
              <button
                key={emoji}
                type="button"
                className={styles.reactionChip}
                onClick={() => toggleReaction.mutate({ messageId: message.id, emoji, channelId })}
                aria-label={t('chat.react', { emoji })}
              >
                {emoji}
              </button>
            ))}
          </span>

          <button type="button" className={styles.threadLink} onClick={onOpenThread}>
            {message.replyCount > 0 ? t('chat.replies', { count: message.replyCount }) : t('chat.reply')}
          </button>
        </div>
      </div>
    </article>
  );
}

function ThreadPane({
  parent,
  channelId,
  locale,
  onClose
}: {
  parent: ChatMessageDetail;
  channelId: string;
  locale: string;
  onClose: () => void;
}) {
  const { t } = useTranslation();
  const { data: replies, isLoading } = useThread(parent.id);
  const reply = useReply();
  const [draft, setDraft] = useState('');

  function send(event: FormEvent) {
    event.preventDefault();
    const body = draft.trim();
    if (!body) return;
    setDraft('');
    reply.mutate({ messageId: parent.id, body, channelId });
  }

  return (
    <Card padded={false} className={styles.chatPane}>
      <div className={styles.threadHeader}>
        <span className={styles.messageAuthor}>{t('chat.thread')}</span>
        <Button variant="ghost" size="sm" onClick={onClose}>
          {t('common.close')}
        </Button>
      </div>

      <div className={styles.messageList}>
        <article className={styles.message}>
          <Avatar name={parent.author?.name ?? '?'} color={parent.author?.avatarColor} size={30} />
          <div className={styles.messageBody}>
            <span className={styles.messageAuthor}>{parent.author?.name}</span>
            <p className={styles.messageText}>{parent.body}</p>
          </div>
        </article>

        <Badge>{t('chat.replies', { count: replies?.length ?? 0 })}</Badge>

        {isLoading && <SkeletonList rows={2} height={40} label={t('common.loading')} />}

        {replies?.map((message) => (
          <article key={message.id} className={styles.message}>
            <Avatar name={message.author?.name ?? '?'} color={message.author?.avatarColor} size={26} />
            <div className={styles.messageBody}>
              <div>
                <span className={styles.messageAuthor}>{message.author?.name}</span>
                <time className={styles.messageTime} dateTime={message.createdAt}>
                  {formatRelative(message.createdAt, locale)}
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
          placeholder={t('chat.replyPlaceholder')}
          aria-label={t('chat.replyPlaceholder')}
        />
        <Button type="submit" variant="primary" disabled={!draft.trim()} loading={reply.isPending}>
          {t('chat.send')}
        </Button>
      </form>
    </Card>
  );
}
