// Unit test for Policies.tsx: verifies the form is hidden/read-only for
// non-superadmin roles, and that Save/dry-run buttons are absent.
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { AuthProvider } from '../auth';
import type { User } from '../types';
import Policies from '../pages/Policies';

// ---------------------------------------------------------------------------
// Mock the api module — listPolicies returns an empty list so the
// load-on-mount effect runs without error. dryRunPolicy/putGlobalPolicy
// are stubs (not called in these tests).
// ---------------------------------------------------------------------------
vi.mock('../api', () => ({
  me: vi.fn(),
  login: vi.fn(),
  logout: vi.fn(),
  changePassword: vi.fn(),
  listPolicies: vi.fn().mockResolvedValue([]),
  dryRunPolicy: vi.fn(),
  putGlobalPolicy: vi.fn(),
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

function renderPolicies(role: string) {
  vi.mocked(me).mockResolvedValue(fakeUser(role));
  return render(
    <AuthProvider>
      <MemoryRouter>
        <Policies />
      </MemoryRouter>
    </AuthProvider>,
  );
}

beforeEach(() => {
  vi.clearAllMocks();
});

describe('Policies form gate', () => {
  it('shows Save and dry-run buttons for superadmin', async () => {
    renderPolicies('superadmin');
    await waitFor(() => {
      expect(screen.getByText('保存全局')).toBeTruthy();
      expect(screen.getByText(/预览.*dry-run/)).toBeTruthy();
    });
  });

  it('hides Save and dry-run buttons for admin', async () => {
    renderPolicies('admin');
    await waitFor(() => {
      expect(screen.queryByText('保存全局')).toBeNull();
      expect(screen.queryByText(/预览.*dry-run/)).toBeNull();
    });
  });

  it('shows read-only notice for non-superadmin', async () => {
    renderPolicies('admin');
    await waitFor(() => {
      expect(screen.getByText(/只读/)).toBeTruthy();
    });
  });

  it('hides Save and dry-run buttons for tenant', async () => {
    renderPolicies('tenant');
    await waitFor(() => {
      expect(screen.queryByText('保存全局')).toBeNull();
      expect(screen.queryByText(/预览.*dry-run/)).toBeNull();
    });
  });
});
