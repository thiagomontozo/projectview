import { Suspense, lazy, type ReactNode } from 'react';
import { Navigate, Outlet, Route, Routes } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { useAuth } from './lib/auth';
import { AppShell } from './app/AppShell';
import { CommandPaletteProvider } from './app/CommandPalette';
import { SkeletonList } from './ui/Skeleton';
import LoginPage from './pages/LoginPage';

// Routes are split so the initial bundle carries the shell and the dashboard,
// not every screen in the product.
const DashboardPage = lazy(() => import('./pages/DashboardPage'));
const MyTasksPage = lazy(() => import('./pages/MyTasksPage'));
const SpacesPage = lazy(() => import('./pages/SpacesPage'));
const ProjectsPage = lazy(() => import('./pages/ProjectsPage'));
const ProjectBoardPage = lazy(() => import('./pages/ProjectBoardPage'));
const TeamsPage = lazy(() => import('./pages/TeamsPage'));
const ResourcesPage = lazy(() => import('./pages/ResourcesPage'));
const ReportsPage = lazy(() => import('./pages/ReportsPage'));
const ChatPage = lazy(() => import('./pages/ChatPage'));
const DocsPage = lazy(() => import('./pages/DocsPage'));
const GoalsPage = lazy(() => import('./pages/GoalsPage'));
const PortfolioPage = lazy(() => import('./pages/PortfolioPage'));
const SettingsPage = lazy(() => import('./pages/SettingsPage'));
const AdminSettingsPage = lazy(() => import('./pages/AdminSettingsPage'));
const AdminUsersPage = lazy(() => import('./pages/AdminUsersPage'));
const WhiteboardsPage = lazy(() => import('./pages/WhiteboardsPage'));
const ClipsPage = lazy(() => import('./pages/ClipsPage'));
const SheetsPage = lazy(() => import('./pages/SheetsPage'));
const FormsPage = lazy(() => import('./pages/FormsPage'));
const AppsPage = lazy(() => import('./pages/AppsPage'));

function RouteFallback() {
  const { t } = useTranslation();
  return <SkeletonList rows={4} label={t('common.loading')} />;
}

function RequireAuth({ children }: { children: ReactNode }) {
  const { user, loading } = useAuth();
  const { t } = useTranslation();

  if (loading) {
    return (
      <div style={{ padding: 'var(--space-8)' }}>
        <SkeletonList rows={3} label={t('common.loading')} />
      </div>
    );
  }
  if (!user) return <Navigate to="/login" replace />;
  return <>{children}</>;
}

export default function App() {
  return (
    <Routes>
      <Route path="/login" element={<LoginPage />} />
      <Route
        path="/"
        element={
          <RequireAuth>
            <CommandPaletteProvider>
              <AppShell />
            </CommandPaletteProvider>
          </RequireAuth>
        }
      >
        {/* A layout route places one Suspense boundary around every lazily
            loaded page, instead of repeating it per route. */}
        <Route
          element={
            <Suspense fallback={<RouteFallback />}>
              <Outlet />
            </Suspense>
          }
        >
          <Route index element={<DashboardPage />} />
          <Route path="my-tasks" element={<MyTasksPage />} />
          <Route path="spaces" element={<SpacesPage />} />
          <Route path="projects" element={<ProjectsPage />} />
          <Route path="projects/:id" element={<ProjectBoardPage />} />
          <Route path="teams" element={<TeamsPage />} />
          <Route path="resources" element={<ResourcesPage />} />
          <Route path="reports" element={<ReportsPage />} />
          <Route path="chat" element={<ChatPage />} />
          <Route path="docs" element={<DocsPage />} />
          <Route path="goals" element={<GoalsPage />} />
          <Route path="portfolio" element={<PortfolioPage />} />
          <Route path="whiteboards" element={<WhiteboardsPage />} />
          <Route path="clips" element={<ClipsPage />} />
          <Route path="sheets" element={<SheetsPage />} />
          <Route path="forms" element={<FormsPage />} />
          <Route path="apps" element={<AppsPage />} />
          <Route path="settings" element={<SettingsPage />} />
          <Route path="admin/users" element={<AdminUsersPage />} />
          <Route path="admin/settings" element={<AdminSettingsPage />} />
        </Route>
      </Route>
      <Route path="*" element={<Navigate to="/" replace />} />
    </Routes>
  );
}
