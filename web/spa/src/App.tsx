import { BrowserRouter, Routes, Route } from 'react-router-dom';
import { AuthProvider, LoginGate, RequireRole } from './auth';
import { Shell } from './Shell';

import Dashboard    from './pages/Dashboard';
import Dispatch     from './pages/Dispatch';
import Nodes        from './pages/Nodes';
import NodeDetail   from './pages/NodeDetail';
import Accounts     from './pages/Accounts';
import Keys         from './pages/Keys';
import Policies     from './pages/Policies';
import Desired      from './pages/Desired';
import Logs         from './pages/Logs';
import Billing      from './pages/Billing';
import BanAnalysis  from './pages/BanAnalysis';
import Fallback     from './pages/Fallback';
import Users        from './pages/Users';
import Slots        from './pages/Slots';
import Settings     from './pages/Settings';

// Roles that count as admin-or-above.
const ADMIN_ROLES = ['admin', 'superadmin', 'operator'];
// Mixed views that admins AND tenants can reach (the page renders the tenant
// variant internally when isTenant is true).
const TENANT_ACCESSIBLE = [...ADMIN_ROLES, 'tenant'];
// Only superadmin can reach certain pages.
const SUPERADMIN_ROLES = ['superadmin'];

export default function App() {
  return (
    <BrowserRouter>
      <AuthProvider>
        <LoginGate>
          <Shell>
            <Routes>
              {/* Public (authenticated) routes */}
              <Route path="/"          element={<Dashboard />} />
              <Route path="/nodes"     element={<Nodes />} />
              <Route path="/nodes/:id" element={<NodeDetail />} />
              <Route path="/accounts"  element={<Accounts />} />
              <Route path="/logs"      element={<Logs />} />

              {/* Admin-or-above routes */}
              <Route path="/dispatch"  element={
                <RequireRole allow={TENANT_ACCESSIBLE}><Dispatch /></RequireRole>
              } />
              <Route path="/keys"      element={
                <RequireRole allow={ADMIN_ROLES}><Keys /></RequireRole>
              } />
              <Route path="/slots"     element={
                <RequireRole allow={ADMIN_ROLES}><Slots /></RequireRole>
              } />
              <Route path="/billing"   element={
                <RequireRole allow={TENANT_ACCESSIBLE}><Billing /></RequireRole>
              } />
              <Route path="/ban-analysis" element={
                <RequireRole allow={ADMIN_ROLES}><BanAnalysis /></RequireRole>
              } />
              <Route path="/fallback"     element={
                <RequireRole allow={TENANT_ACCESSIBLE}><Fallback /></RequireRole>
              } />
              <Route path="/settings"     element={
                <RequireRole allow={TENANT_ACCESSIBLE}><Settings /></RequireRole>
              } />
              <Route path="/desired"   element={
                <RequireRole allow={ADMIN_ROLES}><Desired /></RequireRole>
              } />

              {/* Superadmin-only routes */}
              <Route path="/policies"  element={
                <RequireRole allow={SUPERADMIN_ROLES}><Policies /></RequireRole>
              } />
              <Route path="/users"     element={
                <RequireRole allow={SUPERADMIN_ROLES}><Users /></RequireRole>
              } />
            </Routes>
          </Shell>
        </LoginGate>
      </AuthProvider>
    </BrowserRouter>
  );
}
