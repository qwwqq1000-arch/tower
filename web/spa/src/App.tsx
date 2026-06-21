import { BrowserRouter, Routes, Route } from 'react-router-dom';
import { AuthProvider } from './auth';
import { LoginGate } from './auth';
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
import Audit        from './pages/Audit';
import Events       from './pages/Events';
import BanAnalysis  from './pages/BanAnalysis';
import Fallback     from './pages/Fallback';

export default function App() {
  return (
    <BrowserRouter>
      <AuthProvider>
        <LoginGate>
          <Shell>
            <Routes>
              <Route path="/"          element={<Dashboard />} />
              <Route path="/dispatch"  element={<Dispatch />} />
              <Route path="/nodes"     element={<Nodes />} />
              <Route path="/nodes/:id" element={<NodeDetail />} />
              <Route path="/accounts"  element={<Accounts />} />
              <Route path="/keys"      element={<Keys />} />
              <Route path="/policies"  element={<Policies />} />
              <Route path="/desired"   element={<Desired />} />
              <Route path="/logs"      element={<Logs />} />
              <Route path="/billing"   element={<Billing />} />
              <Route path="/audit"     element={<Audit />} />
              <Route path="/events"        element={<Events />} />
              <Route path="/ban-analysis" element={<BanAnalysis />} />
              <Route path="/fallback"     element={<Fallback />} />
            </Routes>
          </Shell>
        </LoginGate>
      </AuthProvider>
    </BrowserRouter>
  );
}
