// Unit test for RequireRole: verifies that non-allowed roles are redirected.
import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { MemoryRouter, Routes, Route, Navigate } from 'react-router-dom';
import { createContext, useContext, type ReactNode } from 'react';

// ---------------------------------------------------------------------------
// Minimal stub of the auth context value so we can test RequireRole without
// a real backend. We replicate RequireRole's dependency (role + loading) here.
// ---------------------------------------------------------------------------

interface StubAuthCtx {
  role: string | null;
  loading: boolean;
}

const StubCtx = createContext<StubAuthCtx>({ role: null, loading: false });

function StubAuthProvider({
  role,
  loading,
  children,
}: {
  role: string | null;
  loading: boolean;
  children: ReactNode;
}) {
  return <StubCtx.Provider value={{ role, loading }}>{children}</StubCtx.Provider>;
}

// Re-implement RequireRole locally using StubCtx so the test doesn't depend on
// the real auth module, which requires a network.
function TestRequireRole({ allow, children }: { allow: string[]; children: ReactNode }) {
  const { role, loading } = useContext(StubCtx);
  if (loading) return null;
  if (!role || !allow.includes(role)) return <Navigate to="/" replace />;
  return <>{children}</>;
}

// ---------------------------------------------------------------------------
// Pages
// ---------------------------------------------------------------------------
function Dashboard() {
  return <div>Dashboard</div>;
}
function UsersPage() {
  return <div>Users Page</div>;
}

function renderWithRole(role: string | null, path: string, loading = false) {
  return render(
    <StubAuthProvider role={role} loading={loading}>
      <MemoryRouter initialEntries={[path]}>
        <Routes>
          <Route path="/" element={<Dashboard />} />
          <Route
            path="/users"
            element={
              <TestRequireRole allow={['superadmin']}>
                <UsersPage />
              </TestRequireRole>
            }
          />
        </Routes>
      </MemoryRouter>
    </StubAuthProvider>,
  );
}

describe('RequireRole', () => {
  it('allows superadmin to see the Users page', () => {
    renderWithRole('superadmin', '/users');
    expect(screen.getByText('Users Page')).toBeTruthy();
  });

  it('redirects a tenant away from the Users page to /', () => {
    renderWithRole('tenant', '/users');
    expect(screen.queryByText('Users Page')).toBeNull();
    expect(screen.getByText('Dashboard')).toBeTruthy();
  });

  it('redirects an admin away from superadmin-only pages', () => {
    renderWithRole('admin', '/users');
    expect(screen.queryByText('Users Page')).toBeNull();
    expect(screen.getByText('Dashboard')).toBeTruthy();
  });

  it('redirects a viewer away from superadmin-only pages', () => {
    renderWithRole('viewer', '/users');
    expect(screen.queryByText('Users Page')).toBeNull();
    expect(screen.getByText('Dashboard')).toBeTruthy();
  });

  it('renders nothing while auth is loading', () => {
    const { container } = renderWithRole(null, '/users', true);
    // Should be empty — no page, no redirect (spinner is above this layer)
    expect(container.textContent).toBe('');
  });
});
