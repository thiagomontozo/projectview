import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api } from './api';
import type { BoardScene, Clip, SavedView, SheetCell, Spreadsheet, Whiteboard } from '../types';

/**
 * Saved views, whiteboards, spreadsheets and clips.
 *
 * Their own module rather than more of `queries.ts`, for the reason
 * `enterprise.ts` is separate: these belong to four screens most sessions never
 * open, and a file that every page imports should not carry them.
 */

const keys = {
  savedViews: (projectId: string) => ['projects', projectId, 'views'] as const,
  whiteboards: (projectId: string) => ['projects', projectId, 'whiteboards'] as const,
  whiteboard: (id: string) => ['whiteboards', id] as const,
  sheets: (projectId: string) => ['projects', projectId, 'sheets'] as const,
  sheet: (id: string) => ['sheets', id] as const,
  clips: (projectId: string) => ['projects', projectId, 'clips'] as const
};

/* --- Saved views ------------------------------------------------------------ */

export function useSavedViews(projectId?: string) {
  return useQuery({
    queryKey: keys.savedViews(projectId ?? ''),
    queryFn: () => api.get<SavedView[]>(`/projects/${projectId}/views`).then((r) => r.data),
    enabled: Boolean(projectId)
  });
}

export function useSaveView(projectId: string) {
  const client = useQueryClient();
  return useMutation({
    mutationFn: (body: Omit<SavedView, 'id' | 'projectId' | 'createdAt'>) =>
      api.post<SavedView>(`/projects/${projectId}/views`, body).then((r) => r.data),
    onSuccess: () => client.invalidateQueries({ queryKey: keys.savedViews(projectId) })
  });
}

export function useDeleteView(projectId: string) {
  const client = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => api.delete(`/views/${id}`).then(() => id),
    onSuccess: () => client.invalidateQueries({ queryKey: keys.savedViews(projectId) })
  });
}

/* --- Whiteboards ------------------------------------------------------------ */

export function useWhiteboards(projectId?: string) {
  return useQuery({
    queryKey: keys.whiteboards(projectId ?? ''),
    queryFn: () => api.get<Whiteboard[]>(`/projects/${projectId}/whiteboards`).then((r) => r.data),
    enabled: Boolean(projectId)
  });
}

/**
 * One board, with its scene.
 *
 * The list deliberately arrives without scenes — a busy board is not small, and
 * ten of them to draw ten titles is ten documents nobody looked at — so opening
 * one is its own request.
 */
export function useWhiteboard(id?: string) {
  return useQuery({
    queryKey: keys.whiteboard(id ?? ''),
    queryFn: () => api.get<Whiteboard>(`/whiteboards/${id}`).then((r) => r.data),
    enabled: Boolean(id)
  });
}

export function useCreateWhiteboard(projectId: string) {
  const client = useQueryClient();
  return useMutation({
    mutationFn: (title: string) =>
      api.post<Whiteboard>(`/projects/${projectId}/whiteboards`, { title }).then((r) => r.data),
    onSuccess: () => client.invalidateQueries({ queryKey: keys.whiteboards(projectId) })
  });
}

/**
 * Saves a scene against the version it was read at.
 *
 * A 409 means somebody else saved first, and carries their board with it, so
 * the caller can show what happened rather than only refuse.
 */
export function useSaveWhiteboard(projectId: string) {
  const client = useQueryClient();
  return useMutation({
    mutationFn: ({
      id,
      scene,
      version,
      title
    }: {
      id: string;
      scene: BoardScene;
      version: number;
      title?: string;
    }) => api.put<Whiteboard>(`/whiteboards/${id}`, { scene, version, title }).then((r) => r.data),
    onSuccess: (board) => {
      client.setQueryData(keys.whiteboard(board.id), board);
      client.invalidateQueries({ queryKey: keys.whiteboards(projectId) });
    }
  });
}

export function useDeleteWhiteboard(projectId: string) {
  const client = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => api.delete(`/whiteboards/${id}`).then(() => id),
    onSuccess: () => client.invalidateQueries({ queryKey: keys.whiteboards(projectId) })
  });
}

/* --- Spreadsheets ----------------------------------------------------------- */

export function useSheets(projectId?: string) {
  return useQuery({
    queryKey: keys.sheets(projectId ?? ''),
    queryFn: () => api.get<Spreadsheet[]>(`/projects/${projectId}/sheets`).then((r) => r.data),
    enabled: Boolean(projectId)
  });
}

export function useSheet(id?: string) {
  return useQuery({
    queryKey: keys.sheet(id ?? ''),
    queryFn: () => api.get<Spreadsheet>(`/sheets/${id}`).then((r) => r.data),
    enabled: Boolean(id)
  });
}

export function useCreateSheet(projectId: string) {
  const client = useQueryClient();
  return useMutation({
    mutationFn: (title: string) =>
      api.post<Spreadsheet>(`/projects/${projectId}/sheets`, { title }).then((r) => r.data),
    onSuccess: () => client.invalidateQueries({ queryKey: keys.sheets(projectId) })
  });
}

export function useSaveSheet(projectId: string) {
  const client = useQueryClient();
  return useMutation({
    mutationFn: ({
      id,
      cells,
      version,
      title,
      rows,
      cols
    }: {
      id: string;
      cells: Record<string, SheetCell>;
      version: number;
      title?: string;
      rows?: number;
      cols?: number;
    }) =>
      api.put<Spreadsheet>(`/sheets/${id}`, { cells, version, title, rows, cols }).then((r) => r.data),
    onSuccess: (sheet) => {
      client.setQueryData(keys.sheet(sheet.id), sheet);
      client.invalidateQueries({ queryKey: keys.sheets(projectId) });
    }
  });
}

export function useDeleteSheet(projectId: string) {
  const client = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => api.delete(`/sheets/${id}`).then(() => id),
    onSuccess: () => client.invalidateQueries({ queryKey: keys.sheets(projectId) })
  });
}

/* --- Clips ------------------------------------------------------------------ */

export function useClips(projectId?: string) {
  return useQuery({
    queryKey: keys.clips(projectId ?? ''),
    queryFn: () => api.get<Clip[]>(`/projects/${projectId}/clips`).then((r) => r.data),
    enabled: Boolean(projectId)
  });
}

/**
 * Uploads a recording the browser just made.
 *
 * multipart rather than a base64 field, for the reason attachments give: base64
 * inflates by a third, and a recording is already tens of megabytes.
 */
export function useUploadClip(projectId: string) {
  const client = useQueryClient();
  return useMutation({
    mutationFn: ({
      blob,
      title,
      durationMs,
      taskId
    }: {
      blob: Blob;
      title: string;
      durationMs?: number;
      taskId?: string;
    }) => {
      const form = new FormData();
      form.append('file', blob, 'clip.webm');
      form.append('title', title);
      if (durationMs) form.append('durationMs', String(Math.round(durationMs)));
      if (taskId) form.append('taskId', taskId);
      return api
        .post<Clip>(`/projects/${projectId}/clips`, form, {
          headers: { 'Content-Type': 'multipart/form-data' }
        })
        .then((r) => r.data);
    },
    onSuccess: () => client.invalidateQueries({ queryKey: keys.clips(projectId) })
  });
}

/** A signed URL, fetched when somebody presses play rather than for every row. */
export function useClipUrl() {
  return useMutation({
    mutationFn: (id: string) =>
      api.get<{ url: string; expiresAt: string }>(`/clips/${id}/url`).then((r) => r.data)
  });
}

export function useDeleteClip(projectId: string) {
  const client = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => api.delete(`/clips/${id}`).then(() => id),
    onSuccess: () => client.invalidateQueries({ queryKey: keys.clips(projectId) })
  });
}
