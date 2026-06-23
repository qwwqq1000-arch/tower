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
                <RequireRole allow={ADMIN_ROLES}><Dispatch /></RequireRole>
              } />
              <Route path="/keys"      element={
                <RequireRole allow={ADMIN_ROLES}><Keys /></RequireRole>
              } />
              <Route path="/slots"     element={
                <RequireRole allow={ADMIN_ROLES}><Slots /></RequireRole>
              } />
              <Route path="/billing"   element={
                <RequireRole allow={ADMIN_ROLES}><Billing /></RequireRole>
              } />
              <Route path="/ban-analysis" element={
                <RequireRole allow={ADMIN_ROLES}><BanAnalysis /></RequireRole>
              } />
              <Route path="/fallback"     element={
                <RequireRole allow={ADMIN_ROLES}><Fallback /></RequireRole>
              } />
              <Route path="/settings"     element={
                <RequireRole allow={ADMIN_ROLES}><Settings /></RequireRole>
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
