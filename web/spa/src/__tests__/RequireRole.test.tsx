// Unit test for RequireRole: verifies that non-allowed roles are redirected.
// Uses the REAL RequireRole from auth.tsx — mocks only the network layer (api.ts)
// so that AuthProvider resolves to a controlled user without a backend.
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter, Routes, Route } from 'react-router-dom';
import { AuthProvider, RequireRole } from '../auth';
import type { User } from '../types';

// ---------------------------------------------------------------------------
// Mock the api module so me() resolves to whatever role we need.
// login/logout/changePassword are stubs (not called in these tests).
// ---------------------------------------------------------------------------
vi.mock('../api', () => ({
  me: vi.fn(),
  login: vi.fn(),
  logout: vi.fn(),
  changePassword: vi.fn(),
}));

import { me } from '../api';

function fakeUser(role: string): User {
  return {
    sub: 'testuser',
    role,
    perms: [],
    mustChangePw: false,
  };
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

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------
function renderWithRole(roleOrNull: string | null, path: string) {
  // Make me() resolve immediately with the desired user (or reject for null).
  if (roleOrNull === null) {
    vi.mocked(me).mockRejectedValue(new Error('unauthenticated'));
  } else {
    vi.mocked(me).mockResolvedValue(fakeUser(roleOrNull));
  }

  return render(
    <AuthProvider>
      <MemoryRouter initialEntries={[path]}>
        <Routes>
          <Route path="/" element={<Dashboard />} />
          <Route
            path="/users"
            element={
              <RequireRole allow={['superadmin']}>
                <UsersPage />
              </RequireRole>
            }
          />
        </Routes>
      </MemoryRouter>
    </AuthProvider>,
  );
}

beforeEach(() => {
  vi.clearAllMocks();
});

describe('RequireRole (real component)', () => {
  it('allows superadmin to see the Users page', async () => {
    renderWithRole('superadmin', '/users');
    await waitFor(() => {
      expect(screen.getByText('Users Page')).toBeTruthy();
    });
  });

  it('redirects a tenant away from the Users page to /', async () => {
    renderWithRole('tenant', '/users');
    await waitFor(() => {
      expect(screen.queryByText('Users Page')).toBeNull();
      expect(screen.getByText('Dashboard')).toBeTruthy();
    });
  });

  it('redirects an admin away from superadmin-only pages', async () => {
    renderWithRole('admin', '/users');
    await waitFor(() => {
      expect(screen.queryByText('Users Page')).toBeNull();
      expect(screen.getByText('Dashboard')).toBeTruthy();
    });
  });

  it('redirects a viewer away from superadmin-only pages', async () => {
    renderWithRole('viewer', '/users');
    await waitFor(() => {
      expect(screen.queryByText('Users Page')).toBeNull();
      expect(screen.getByText('Dashboard')).toBeTruthy();
    });
  });

  it('renders nothing while auth is loading (initial state before me() resolves)', async () => {
    // Use a promise that never resolves to simulate infinite loading state.
    vi.mocked(me).mockReturnValue(new Promise(() => {}));
    const { container } = render(
      <AuthProvider>
        <MemoryRouter initialEntries={['/users']}>
          <Routes>
            <Route path="/" element={<Dashboard />} />
            <Route
              path="/users"
              element={
                <RequireRole allow={['superadmin']}>
                  <UsersPage />
                </RequireRole>
              }
            />
          </Routes>
        </MemoryRouter>
      </AuthProvider>,
    );
    // LoginGate is not wrapping here; RequireRole renders null while loading.
    // The container may contain the loading spinner from AuthProvider's parent
    // (if any) but RequireRole itself returns null.
    // We verify no page content appears yet.
    expect(screen.queryByText('Users Page')).toBeNull();
    expect(screen.queryByText('Dashboard')).toBeNull();
    // container may have empty text or the Loading... spinner text from LoginGate
    // (not mounted here), so we only assert page absence.
    void container; // suppress unused warning
  });
});
