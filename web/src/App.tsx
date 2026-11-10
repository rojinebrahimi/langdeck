import { useEffect } from "react";
import { Navigate, Route, Routes, useLocation } from "react-router-dom";
import { useMe } from "./api/hooks";
import { setCsrfToken } from "./api/client";
import { Layout } from "./components/Layout";
import { Loading } from "./components/ui";
import { LoginPage } from "./pages/Login";
import { DashboardPage } from "./pages/Dashboard";
import { AgentsPage } from "./pages/Agents";
import { AgentDetailPage } from "./pages/AgentDetail";
import { DependenciesPage } from "./pages/Dependencies";
import { RunsPage } from "./pages/Runs";
import { RunDetailPage } from "./pages/RunDetail";
import { ApprovalsPage } from "./pages/Approvals";
import { APIKeysPage } from "./pages/APIKeys";
import { UsersPage } from "./pages/Users";
import { AuditPage } from "./pages/Audit";
import { ForcedPasswordChangePage } from "./pages/ChangePassword";
import { GuidePage } from "./pages/Guide";

export function App() {
  const location = useLocation();
  const me = useMe(location.pathname !== "/login");
  useEffect(() => {
    if (me.data) setCsrfToken(me.data.csrf_token);
  }, [me.data]);

  if (location.pathname === "/login") return <LoginPage />;
  if (me.isLoading) return <Loading />;
  if (me.isError || !me.data) return <Navigate to="/login" replace />;
  if (me.data.must_change_password) return <ForcedPasswordChangePage />;

  return (
    <Layout>
      <Routes>
        <Route path="/" element={<DashboardPage />} />
        <Route path="/agents" element={<AgentsPage />} />
        <Route path="/agents/:id" element={<AgentDetailPage />} />
        <Route path="/dependencies" element={<DependenciesPage />} />
        <Route path="/runs" element={<RunsPage />} />
        <Route path="/runs/:id" element={<RunDetailPage />} />
        <Route path="/approvals" element={<ApprovalsPage />} />
        <Route path="/guide" element={<GuidePage />} />
        <Route path="/settings/api-keys" element={<APIKeysPage />} />
        <Route path="/settings/users" element={<UsersPage />} />
        <Route path="/settings/audit" element={<AuditPage />} />
        <Route path="*" element={<Navigate to="/" replace />} />
      </Routes>
    </Layout>
  );
}
