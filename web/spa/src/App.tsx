import { BrowserRouter, Routes, Route } from 'react-router-dom';
import { AuthProvider } from './auth';
import { LoginGate } from './auth';
import { Shell } from './Shell';

import Dashboard    from './pages/Dashboard';
import Nodes        from './pages/Nodes';
import Accounts     from './pages/Accounts';
import Keys         from './pages/Keys';
import Policies     from './pages/Policies';
import Desired      from './pages/Desired';
import Logs         from './pages/Logs';
import Billing      from './pages/Billing';
import Audit        from './pages/Audit';
import Events       from './pages/Events';

export default function App() {
  return (
    <BrowserRouter>
      <AuthProvider>
        <LoginGate>
          <Shell>
            <Routes>
              <Route path="/"          element={<Dashboard />} />
              <Route path="/nodes"     element={<Nodes />} />
              <Route path="/accounts"  element={<Accounts />} />
              <Route path="/keys"      element={<Keys />} />
              <Route path="/policies"  element={<Policies />} />
              <Route path="/desired"   element={<Desired />} />
              <Route path="/logs"      element={<Logs />} />
              <Route path="/billing"   element={<Billing />} />
              <Route path="/audit"     element={<Audit />} />
              <Route path="/events"    element={<Events />} />
            </Routes>
          </Shell>
        </LoginGate>
      </AuthProvider>
    </BrowserRouter>
  );
}
